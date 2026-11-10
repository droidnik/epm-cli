// specgen maintains the OpenAPI spec and the generated command table.
//
//	go run ./tools/specgen fetch [-out spec/openapi-v3.json]
//	    Rebuild the spec from the public API reference at docs.beyondtrust.com.
//	    (ReadMe.io embeds one operation per page; all v3 pages are merged.)
//
//	go run ./tools/specgen gen [-spec spec/openapi-v3.json] [-out internal/spec/commands.json] [-v]
//	    Generate the compact command table that is embedded into the binary.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/droidnik/epm-cli/internal/ojson"
	"github.com/droidnik/epm-cli/internal/spec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: specgen fetch|gen [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "fetch":
		fs := flag.NewFlagSet("fetch", flag.ExitOnError)
		out := fs.String("out", "spec/openapi-v3.json", "output file")
		_ = fs.Parse(os.Args[2:])
		err = fetch(*out)
	case "gen":
		fs := flag.NewFlagSet("gen", flag.ExitOnError)
		in := fs.String("spec", "spec/openapi-v3.json", "OpenAPI spec (JSON)")
		out := fs.String("out", "internal/spec/commands.json", "output file")
		verbose := fs.Bool("v", false, "print the command list")
		_ = fs.Parse(os.Args[2:])
		err = generate(*in, *out, *verbose)
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// ============================================================== fetch

const (
	docsBase  = "https://docs.beyondtrust.com"
	startPage = docsBase + "/epm-wm/reference/get_v3-accepteddomains"
)

var slugRe = regexp.MustCompile(`/epm-wm/reference/(get|post|put|patch|delete)_(v3-|policyeditor-v3|scim-v3)[a-z0-9_-]*`)

func httpGet(url string) (string, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "epm-cli-specgen")
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// extractSpecs finds every embedded JSON object that looks like an OpenAPI document.
func extractSpecs(html string) []*ojson.Object {
	var out []*ojson.Object
	i := 0
	for {
		idx := strings.Index(html[i:], `"openapi":"3`)
		if idx < 0 {
			return out
		}
		idx += i
		pos := strings.LastIndex(html[:idx], "{")
		for tries := 0; pos >= 0 && tries < 60; tries++ {
			dec := json.NewDecoder(strings.NewReader(html[pos:]))
			dec.UseNumber()
			if v, err := ojson.DecodeFrom(dec); err == nil {
				if o := ojson.Obj(v); o != nil {
					if _, ok := o.Get("paths"); ok {
						out = append(out, o)
						break
					}
				}
			}
			pos = strings.LastIndex(html[:pos], "{")
		}
		i = idx + 10
	}
}

func fetch(outPath string) error {
	fmt.Fprintln(os.Stderr, "index:", startPage)
	index, err := httpGet(startPage)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var slugs []string
	for _, s := range slugRe.FindAllString(index, -1) {
		if !seen[s] {
			seen[s] = true
			slugs = append(slugs, s)
		}
	}
	sort.Strings(slugs)
	fmt.Fprintf(os.Stderr, "%d reference pages\n", len(slugs))

	pages := make([]string, len(slugs))
	errs := make([]error, len(slugs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, s := range slugs {
		wg.Add(1)
		go func(i int, s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pages[i], errs[i] = httpGet(docsBase + s)
		}(i, s)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	merged := ojson.NewObject()
	merged.Set("openapi", "3.0.1")
	paths, components := ojson.NewObject(), ojson.NewObject()
	for _, html := range pages {
		for _, s := range extractSpecs(html) {
			for _, k := range []string{"info", "servers"} {
				if _, ok := merged.Get(k); !ok {
					if v, ok := s.Get(k); ok {
						merged.Set(k, v)
					}
				}
			}
			if p := obj(s, "paths"); p != nil {
				for _, path := range p.Keys {
					dst := obj(paths, path)
					if dst == nil {
						dst = ojson.NewObject()
						paths.Set(path, dst)
					}
					src := obj(p, path)
					for _, m := range src.Keys {
						dst.Set(m, src.Values[m])
					}
				}
			}
			if c := obj(s, "components"); c != nil {
				for _, kind := range c.Keys {
					dst := obj(components, kind)
					if dst == nil {
						dst = ojson.NewObject()
						components.Set(kind, dst)
					}
					src := obj(c, kind)
					for _, name := range src.Keys {
						dst.Set(name, src.Values[name])
					}
				}
			}
		}
	}
	// Sort paths for stable diffs.
	sort.Strings(paths.Keys)
	merged.Set("paths", paths)
	merged.Set("components", components)
	b, err := ojson.MarshalIndent(merged)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, append(b, '\n'), 0o644); err != nil {
		return err
	}
	n := 0
	for _, p := range paths.Keys {
		n += len(obj(paths, p).Keys)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d paths, ~%d operations)\n", outPath, len(paths.Keys), n)
	return nil
}

// ============================================================ generate

// Tag -> CLI resource name (anything not listed is kebab-cased from the tag).
var resourceNames = map[string]string{
	"AdminAccessRequest":         "admin-access-requests",
	"AuthorizationRequest":       "authorization-requests",
	"AuthorizationRequestAudits": "authorization-request-audits",
	"AuthenticationProvider":     "authentication-provider",
	"ApiAccounts":                "api-accounts",
	"AppSwitcher":                "app-switcher",
	"File":                       "api-definition",
	"ScimResourceTypes":          "scim-resource-types",
	"ScimRoles":                  "scim-roles",
	"ScimSchemas":                "scim-schemas",
	"ScimServiceProviderConfig":  "scim-service-provider-config",
	"ScimUsers":                  "scim-users",
}

// "METHOD path" -> verb, where the derived name is ambiguous or ugly.
var verbOverrides = map[string]string{
	"GET /v3/About":                                                                  "show",
	"POST /v3/Computers/archive":                                                     "bulk-archive",
	"POST /v3/Computers/Unarchive":                                                   "bulk-unarchive",
	"POST /v3/Computers/{computerId}/archive":                                        "archive",
	"POST /v3/Computers/{computerId}/Unarchive":                                      "unarchive",
	"GET /v3/Computers/Logs/{id}/Content":                                            "log-content",
	"GET /v3/Policies/PolicyRevision/{policyRevisionId}/Content":                     "revision-content",
	"GET /v3/File/download/GetYamlApiDefinitionFile":                                 "download",
	"POST /v3/AdminAccessRequest/approval":                                           "decide",
	"POST /v3/Groups/AssignPolicyToMultipleGroups":                                   "assign-policy-to-groups",
	"POST /v3/Groups/bulkclearpolicy":                                                "bulk-clear-policy",
	"GET /PolicyEditor/v3/Policy/{policyId}/Windows/ApplicationGroups":               "windows-applications",
	"POST /PolicyEditor/v3/Policy/{policyId}/Windows/ApplicationGroups/Applications": "windows-applications-modify",
	"GET /PolicyEditor/v3/Policy/{policyId}/MacOS/ApplicationGroups":                 "macos-applications",
	"POST /PolicyEditor/v3/Policy/{policyId}/MacOS/ApplicationGroups/Applications":   "macos-applications-modify",
	"GET /scim/v3/ResourceTypes/Role":                                                "role",
	"GET /scim/v3/ResourceTypes/User":                                                "user",
	"PATCH /scim/v3/users/{userID}":                                                  "patch",
}

// Verbs that change or remove state in a way that is hard to undo -> confirmation prompt.
var destructiveVerbs = map[string]bool{
	"delete": true, "reject": true, "deactivate": true, "archive": true, "bulk-archive": true,
	"clear-policy": true, "bulk-clear-policy": true, "disable": true, "discard-draft": true,
	"unassign-computers": true,
}

// Flags the CLI defines itself; generated flags must not collide with them.
var reservedFlags = []string{
	"all", "body", "set", "yes", "help", "output", "profile", "field", "columns", "dry-run", "wide",
	"out-file", "query", "verbose", "quiet", "no-input", "no-color", "body-template", "max-pages",
}

var stripPrefixes = []string{"Filter.", "Pagination.", "TimePeriod."}

var httpMethods = []string{"get", "post", "put", "patch", "delete"}

// ---- generic helpers over ojson values

func obj(o *ojson.Object, key string) *ojson.Object {
	if o == nil {
		return nil
	}
	v, _ := o.Get(key)
	return ojson.Obj(v)
}

func str(o *ojson.Object, key string) string {
	if o == nil {
		return ""
	}
	v, _ := o.Get(key)
	return ojson.Str(v)
}

func arr(o *ojson.Object, key string) []any {
	if o == nil {
		return nil
	}
	v, _ := o.Get(key)
	a, _ := v.([]any)
	return a
}

func boolean(o *ojson.Object, key string) bool {
	if o == nil {
		return false
	}
	v, _ := o.Get(key)
	b, _ := v.(bool)
	return b
}

func has(o *ojson.Object, key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.Get(key)
	return ok
}

func kebab(name string) string {
	name = strings.NewReplacer(".", "-", "_", "-").Replace(name)
	rs := []rune(name)
	var b strings.Builder
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteRune('-')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	s := regexp.MustCompile(`-+`).ReplaceAllString(b.String(), "-")
	return strings.Trim(s, "-")
}

var wsRe = regexp.MustCompile(`\s+`)

func helpText(s string) string { return strings.TrimSpace(wsRe.ReplaceAllString(s, " ")) }

type gen struct {
	schemas *ojson.Object
}

func refName(s *ojson.Object) string {
	r := str(s, "$ref")
	if r == "" {
		return ""
	}
	return r[strings.LastIndex(r, "/")+1:]
}

func (g *gen) resolve(s *ojson.Object) *ojson.Object {
	for i := 0; s != nil && has(s, "$ref") && i < 20; i++ {
		s = obj(g.schemas, refName(s))
	}
	if all := arr(s, "allOf"); len(all) == 1 {
		return g.resolve(ojson.Obj(all[0]))
	}
	if s == nil {
		return ojson.NewObject()
	}
	return s
}

func enumStrings(a []any) []string {
	var out []string
	for _, v := range a {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

type typeInfo struct {
	Type, Format string
	Array        bool
	Enum         []string
}

func (g *gen) typeInfo(schema *ojson.Object) typeInfo {
	s := g.resolve(schema)
	t := typeInfo{Type: str(s, "type"), Format: str(s, "format"), Enum: enumStrings(arr(s, "enum"))}
	if t.Type == "" {
		t.Type = "string"
	}
	if t.Type == "array" {
		items := g.resolve(obj(s, "items"))
		t = typeInfo{Type: str(items, "type"), Format: str(items, "format"), Array: true, Enum: enumStrings(arr(items, "enum"))}
		if t.Type == "" {
			t.Type = "string"
		}
		if t.Type == "object" || has(items, "properties") {
			t.Type = "object"
		}
	} else if has(s, "properties") {
		t.Type = "object"
	}
	return t
}

// example builds a skeleton value for a request body template.
func (g *gen) example(schema *ojson.Object, depth int, stack []string) any {
	name := refName(schema)
	for _, n := range stack {
		if name != "" && n == name {
			return ojson.NewObject()
		}
	}
	s := g.resolve(schema)
	if name != "" {
		stack = append(append([]string{}, stack...), name)
	}
	if depth > 6 {
		return nil
	}
	if v, ok := s.Get("example"); ok {
		return v
	}
	if e := arr(s, "enum"); len(e) > 0 {
		return e[0]
	}
	def, hasDef := s.Get("default")
	switch t := str(s, "type"); {
	case t == "object" || has(s, "properties"):
		o := ojson.NewObject()
		if props := obj(s, "properties"); props != nil {
			for _, k := range props.Keys {
				o.Set(k, g.example(obj(props, k), depth+1, stack))
			}
		}
		return o
	case t == "array":
		return []any{g.example(obj(s, "items"), depth+1, stack)}
	case t == "boolean":
		if hasDef {
			return def
		}
		return false
	case t == "integer" || t == "number":
		if hasDef {
			return def
		}
		return 0
	}
	switch str(s, "format") {
	case "uuid":
		return "00000000-0000-0000-0000-000000000000"
	case "date-time":
		return "2026-01-01T00:00:00Z"
	case "binary":
		return "<file>"
	}
	if hasDef {
		return def
	}
	return ""
}

var baseRe = regexp.MustCompile(`^(/scim/v\d+/[^/]+|/PolicyEditor/v\d+/Policy|/v\d+/[^/]+)`)

func resourceBase(path string) string {
	if m := baseRe.FindString(path); m != "" {
		return m
	}
	return path
}

func deriveVerb(method, path, base string, isList bool) string {
	var params, words []string
	for _, s := range strings.Split(strings.Trim(path[len(base):], "/"), "/") {
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "{") {
			params = append(params, s)
		} else {
			words = append(words, s)
		}
	}
	if len(words) == 0 {
		if len(params) > 0 {
			return map[string]string{"GET": "get", "PUT": "update", "DELETE": "delete", "PATCH": "patch", "POST": "create"}[method]
		}
		if method == "GET" {
			if isList {
				return "list"
			}
			return "show"
		}
		return map[string]string{"POST": "create", "PUT": "update", "DELETE": "delete", "PATCH": "patch"}[method]
	}
	return kebab(strings.Join(words, "-"))
}

func flagName(param string) string {
	switch param {
	case "Sorts":
		return "sort"
	case "Pagination.PageNumber":
		return "page"
	}
	for _, p := range stripPrefixes {
		if strings.HasPrefix(param, p) {
			return kebab(strings.TrimPrefix(param, p))
		}
	}
	// Grouped search params (Events.EventAction, Computers.HostName, ...) -> last segment.
	return kebab(param[strings.LastIndex(param, ".")+1:])
}

func (g *gen) operation(method, path string, op, pathItem *ojson.Object) *spec.Command {
	cmd := &spec.Command{
		Method: strings.ToUpper(method), Path: path,
		Summary: helpText(str(op, "summary")), Description: helpText(str(op, "description")),
		PathParams: []spec.PathParam{}, QueryParams: []spec.Param{},
	}
	if cmd.Summary == "" {
		cmd.Summary = cmd.Method + " " + path
	}
	if cmd.Description == cmd.Summary {
		cmd.Description = ""
	}
	if tags := arr(op, "tags"); len(tags) > 0 {
		cmd.Tag = fmt.Sprint(tags[0])
	} else {
		cmd.Tag = "Other"
	}
	used := map[string]bool{}
	for _, f := range reservedFlags {
		used[f] = true
	}
	params := append(append([]any{}, arr(pathItem, "parameters")...), arr(op, "parameters")...)
	for _, pv := range params {
		p := ojson.Obj(pv)
		name := str(p, "name")
		switch str(p, "in") {
		case "path":
			cmd.PathParams = append(cmd.PathParams, spec.PathParam{Name: name, Help: helpText(str(p, "description"))})
		case "query":
			ti := g.typeInfo(obj(p, "schema"))
			flag := flagName(name)
			if used[flag] {
				flag = kebab(name)
			}
			used[flag] = true
			cmd.QueryParams = append(cmd.QueryParams, spec.Param{
				Name: name, Flag: flag, Type: ti.Type, Format: ti.Format, Array: ti.Array, Enum: ti.Enum,
				Required: boolean(p, "required"), Help: helpText(str(p, "description")),
			})
		}
	}

	if rb := obj(op, "requestBody"); rb != nil {
		content := obj(rb, "content")
		body := &spec.Body{Required: boolean(rb, "required"), Fields: []spec.Field{}}
		var schema *ojson.Object
		if mp := obj(content, "multipart/form-data"); mp != nil {
			body.Kind, schema = "multipart", obj(mp, "schema")
		} else {
			body.Kind = "json"
			for _, ct := range append([]string{"application/json", "application/scim+json"}, content.Keys...) {
				if c := obj(content, ct); c != nil {
					schema = obj(c, "schema")
					break
				}
			}
		}
		resolved := g.resolve(schema)
		body.IsArray = str(resolved, "type") == "array"
		if body.IsArray {
			body.Schema = refName(obj(resolved, "items"))
		} else {
			body.Schema = refName(schema)
		}
		if props := obj(resolved, "properties"); props != nil {
			for _, pname := range props.Keys {
				ps := obj(props, pname)
				ti := g.typeInfo(ps)
				if ti.Type == "object" && body.Kind == "json" {
					continue // nested objects are set via --set / --body
				}
				flag := kebab(pname)
				if used[flag] {
					flag = "body-" + flag
				}
				if used[flag] {
					continue
				}
				used[flag] = true
				rps := g.resolve(ps)
				help := str(rps, "description")
				if help == "" {
					help = str(ps, "description")
				}
				body.Fields = append(body.Fields, spec.Field{
					Name: pname, Flag: flag, Type: ti.Type, Format: ti.Format, Array: ti.Array, Enum: ti.Enum,
					Binary: str(rps, "format") == "binary", Help: helpText(help),
				})
			}
		}
		tmpl, _ := ojson.Marshal(g.example(schema, 0, nil))
		body.Template = tmpl
		cmd.Body = body
	}

	isArray := false
	if resps := obj(op, "responses"); resps != nil {
		for _, code := range resps.Keys {
			if !strings.HasPrefix(code, "2") {
				continue
			}
			content := obj(obj(resps, code), "content")
			if content == nil {
				continue
			}
			for _, ct := range content.Keys {
				s := obj(obj(content, ct), "schema")
				if n := refName(s); n != "" {
					cmd.ResponseSchema = n
				} else if n := refName(obj(s, "items")); n != "" {
					cmd.ResponseSchema = n
				}
				if str(g.resolve(s), "type") == "array" {
					isArray = true
				}
			}
		}
	}
	for _, q := range cmd.QueryParams {
		if q.Name == "Pagination.PageNumber" {
			cmd.Paged = true
		}
	}
	cmd.IsList = cmd.Paged || isArray || strings.HasSuffix(cmd.ResponseSchema, "PagedResponse") ||
		strings.HasSuffix(cmd.ResponseSchema, "ListResponse")
	return cmd
}

func generate(in, out string, verbose bool) error {
	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	v, err := ojson.Decode(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}
	doc := ojson.Obj(v)
	g := &gen{schemas: obj(obj(doc, "components"), "schemas")}
	if g.schemas == nil {
		g.schemas = ojson.NewObject()
	}
	paths := obj(doc, "paths")
	keys := append([]string{}, paths.Keys...)
	sort.Strings(keys)

	byName := map[string]*spec.Resource{}
	for _, path := range keys {
		item := obj(paths, path)
		for _, m := range httpMethods {
			op := obj(item, m)
			if op == nil {
				continue
			}
			cmd := g.operation(m, path, op, item)
			name := resourceNames[cmd.Tag]
			if name == "" {
				name = kebab(cmd.Tag)
			}
			cmd.Verb = verbOverrides[cmd.Method+" "+path]
			if cmd.Verb == "" {
				cmd.Verb = deriveVerb(cmd.Method, path, resourceBase(path), cmd.IsList)
			}
			cmd.Destructive = cmd.Method == "DELETE" || destructiveVerbs[cmd.Verb]
			// The API docs say to sort computers by created/asc to avoid duplicates while paging.
			if path == "/v3/Computers" && cmd.Method == "GET" {
				cmd.DefaultSort = "created:asc"
			}
			r := byName[name]
			if r == nil {
				r = &spec.Resource{Name: name, Tag: cmd.Tag}
				byName[name] = r
			}
			if r.Command(cmd.Verb) != nil {
				return fmt.Errorf("verb collision: %s %s (%s %s)", name, cmd.Verb, cmd.Method, path)
			}
			r.Commands = append(r.Commands, cmd)
		}
	}
	info := obj(doc, "info")
	t := spec.Table{Title: str(info, "title"), Version: str(info, "version")}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	total := 0
	for _, n := range names {
		t.Resources = append(t.Resources, byName[n])
		total += len(byName[n].Commands)
	}
	b, err := json.MarshalIndent(t, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d resources, %d commands\n", out, len(t.Resources), total)
	if verbose {
		for _, r := range t.Resources {
			for _, c := range r.Commands {
				mark := ""
				if c.Destructive {
					mark = "  [!]"
				}
				fmt.Printf("  %-30s %-28s %-6s %s%s\n", r.Name, c.Verb, c.Method, c.Path, mark)
			}
		}
	}
	return nil
}
