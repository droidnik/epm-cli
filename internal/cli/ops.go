package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/droidnik/epm-cli/internal/apperr"
	"github.com/droidnik/epm-cli/internal/client"
	"github.com/droidnik/epm-cli/internal/ojson"
	"github.com/droidnik/epm-cli/internal/spec"
)

func resourceCmd(a *App, res *spec.Resource) *cobra.Command {
	rc := &cobra.Command{
		Use:   res.Name,
		Short: short(strings.Join(res.Verbs(), ", "), 72),
		Long:  res.Tag + " — BeyondTrust EPM Management API",
		Args:  cobra.ArbitraryArgs,
		RunE:  parentRunE,
	}
	for _, op := range res.Commands {
		rc.AddCommand(operationCmd(a, res, op))
	}
	return rc
}

func short(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Flag names for the advanced/body flags the CLI adds itself.
const (
	flagAll          = "all"
	flagMaxPages     = "max-pages"
	flagQuery        = "query"
	flagBody         = "body"
	flagSet          = "set"
	flagBodyTemplate = "body-template"
)

func operationCmd(a *App, res *spec.Resource, op *spec.Command) *cobra.Command {
	var argNames []string
	use := op.Verb
	for _, p := range op.PathParams {
		argNames = append(argNames, p.Name)
		use += " " + strings.ToUpper(p.Name)
	}
	marker := ""
	if op.Destructive {
		marker = " [destructive]"
	}
	long := op.Summary + marker
	if op.Description != "" {
		long += "\n\n" + op.Description
	}
	long += "\n\nAPI: " + op.Method + " " + op.Path
	if op.Body != nil {
		long += "\nBody: " + op.Body.Kind
		if op.Body.Schema != "" {
			long += " (" + op.Body.Schema + ")"
		}
		if op.Body.Required {
			long += " — required"
		}
		if op.Body.Kind == "json" {
			long += fmt.Sprintf("\nExample body:  epm-cli %s %s --body-template", res.Name, op.Verb)
		}
	}
	c := &cobra.Command{
		Use:         use,
		Short:       short(op.Summary, 90) + marker,
		Long:        long,
		Args:        usageArgs(argNames),
		Annotations: map[string]string{"method": op.Method, "path": op.Path},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runOperation(cmd, res, op, args)
		},
	}
	if op.Destructive {
		c.Annotations["destructive"] = "true"
	}
	f := c.Flags()
	for _, q := range op.QueryParams {
		help := short(q.Help, 140)
		if help == "" {
			help = q.Name
		}
		if q.Name == "Sorts" {
			f.StringArray("sort", nil, "sort by FIELD[:asc|desc], repeatable (e.g. --sort created:desc)")
			continue
		}
		addTypedFlag(c, q.Flag, q.Type, q.Format, q.Array, q.Enum, help)
		if q.Required {
			_ = c.MarkFlagRequired(q.Flag)
		}
	}
	if op.Paged {
		f.BoolP(flagAll, "A", false, "fetch every page and return all records")
		f.Int(flagMaxPages, 0, "with --all: stop after N pages")
	}
	f.StringArray(flagQuery, nil, "extra raw query parameter KEY=VALUE (repeatable)")
	if op.Body != nil {
		for _, fl := range op.Body.Fields {
			help := short(fl.Help, 140)
			if help == "" {
				help = fl.Name
			}
			if fl.Binary {
				f.String(fl.Flag, "", help+" (file path)")
				continue
			}
			addTypedFlag(c, fl.Flag, fl.Type, fl.Format, fl.Array, fl.Enum, help)
		}
		if op.Body.Kind == "json" {
			f.StringP(flagBody, "d", "", "full JSON body: inline, @file or - (stdin); field flags are merged on top")
			f.StringArray(flagSet, nil, "set a (nested) body field PATH=VALUE, value parsed as JSON if possible "+
				"(e.g. --set filter.os=Windows)")
			f.Bool(flagBodyTemplate, false, "print an example request body and exit (no API call)")
		}
	}
	return c
}

func addTypedFlag(c *cobra.Command, name, typ, format string, array bool, enum []string, help string) {
	f := c.Flags()
	if len(enum) > 0 {
		help += " (" + strings.Join(enum, "|") + ")"
	}
	switch {
	case array:
		f.StringSlice(name, nil, help+" (comma-separated or repeated)")
	case typ == "boolean":
		f.Bool(name, false, help)
	case typ == "integer":
		f.Int64(name, 0, help)
	case typ == "number":
		f.Float64(name, 0, help)
	default:
		f.String(name, "", help)
	}
	if len(enum) > 0 {
		// Must run after the flag is defined, or cobra rejects the registration.
		vals := enum
		_ = c.RegisterFlagCompletionFunc(name, func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return vals, cobra.ShellCompDirectiveNoFileComp
		})
	}
	if format == "uuid" || format == "date-time" {
		if fl := f.Lookup(name); fl != nil {
			fl.Usage += " [" + format + "]"
		}
	}
}

// flagValues returns typed values for a changed flag (nil if not set).
func flagValues(f *pflag.FlagSet, name, typ string, array bool, enum []string) ([]any, error) {
	fl := f.Lookup(name)
	if fl == nil || !fl.Changed {
		return nil, nil
	}
	var raw []string
	switch {
	case array:
		raw, _ = f.GetStringSlice(name)
	case typ == "boolean":
		b, _ := f.GetBool(name)
		return []any{b}, nil
	case typ == "integer":
		i, _ := f.GetInt64(name)
		return []any{json.Number(strconv.FormatInt(i, 10))}, nil
	case typ == "number":
		n, _ := f.GetFloat64(name)
		return []any{json.Number(strconv.FormatFloat(n, 'f', -1, 64))}, nil
	default:
		s, _ := f.GetString(name)
		raw = []string{s}
	}
	out := make([]any, 0, len(raw))
	for _, s := range raw {
		if len(enum) > 0 {
			canon := ""
			for _, e := range enum {
				if strings.EqualFold(e, s) {
					canon = e
				}
			}
			if canon == "" {
				return nil, apperr.Usagef("invalid value %q for --%s (choose from %s)", s, name, strings.Join(enum, ", "))
			}
			s = canon
		}
		switch typ {
		case "integer", "number":
			if _, err := strconv.ParseFloat(s, 64); err != nil {
				return nil, apperr.Usagef("--%s expects numbers, got %q", name, s)
			}
			out = append(out, json.Number(s))
		case "boolean":
			b, err := strconv.ParseBool(s)
			if err != nil {
				return nil, apperr.Usagef("--%s expects true/false, got %q", name, s)
			}
			out = append(out, b)
		default:
			out = append(out, s)
		}
	}
	return out, nil
}

func queryString(v any) string {
	switch t := v.(type) {
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

// ReadBodyArg parses --body: inline JSON, @file or - (stdin).
func ReadBodyArg(value string, stdin io.Reader) (any, error) {
	var text []byte
	var err error
	switch {
	case value == "-":
		text, err = io.ReadAll(stdin)
	case strings.HasPrefix(value, "@"):
		text, err = os.ReadFile(expandHome(value[1:]))
	default:
		text = []byte(value)
	}
	if err != nil {
		return nil, apperr.Usagef("cannot read body: %v", err)
	}
	v, err := ojson.Decode(text)
	if err != nil {
		return nil, apperr.Usagef("--body is not valid JSON: %v", err)
	}
	return v, nil
}

func parseKV(item, flag string) (string, string, error) {
	k, v, ok := strings.Cut(item, "=")
	if !ok {
		return "", "", apperr.Usagef("%s expects KEY=VALUE, got %q", flag, item)
	}
	return strings.TrimSpace(k), v, nil
}

func jsonOrString(s string) any {
	if v, err := ojson.Decode([]byte(s)); err == nil {
		return v
	}
	return s
}

func setPath(o *ojson.Object, path string, v any) {
	parts := strings.Split(path, ".")
	cur := o
	for _, p := range parts[:len(parts)-1] {
		next := ojson.Obj(cur.Values[p])
		if next == nil {
			next = ojson.NewObject()
			cur.Set(p, next)
		}
		cur = next
	}
	cur.Set(parts[len(parts)-1], v)
}

func sortParams(sorts []string) ([]client.KV, error) {
	var out []client.KV
	for i, s := range sorts {
		field, order, _ := strings.Cut(s, ":")
		order = strings.ToLower(strings.TrimSpace(order))
		if order == "" {
			order = "asc"
		}
		if order != "asc" && order != "desc" {
			return nil, apperr.Usagef("--sort order must be asc or desc, got %q", order)
		}
		out = append(out,
			client.KV{Key: fmt.Sprintf("Sorts[%d].by", i), Value: strings.TrimSpace(field)},
			client.KV{Key: fmt.Sprintf("Sorts[%d].order", i), Value: strings.ToUpper(order[:1]) + order[1:]})
	}
	return out, nil
}

// buildRequest turns flags and args into an API request.
func buildRequest(a *App, cmd *cobra.Command, op *spec.Command, args []string) (*client.Request, error) {
	f := cmd.Flags()
	path := op.Path
	for i, p := range op.PathParams {
		path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(args[i]))
	}
	req := &client.Request{Method: op.Method, Path: path}

	for _, q := range op.QueryParams {
		if q.Name == "Sorts" {
			sorts, _ := f.GetStringArray("sort")
			all, _ := f.GetBool(flagAll)
			if len(sorts) == 0 && op.DefaultSort != "" && all {
				sorts = []string{op.DefaultSort}
			}
			kv, err := sortParams(sorts)
			if err != nil {
				return nil, err
			}
			req.Query = append(req.Query, kv...)
			continue
		}
		vals, err := flagValues(f, q.Flag, q.Type, q.Array, q.Enum)
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			req.Query = append(req.Query, client.KV{Key: q.Name, Value: queryString(v)})
		}
	}
	extra, _ := f.GetStringArray(flagQuery)
	for _, item := range extra {
		k, v, err := parseKV(item, "--query")
		if err != nil {
			return nil, err
		}
		req.Query = append(req.Query, client.KV{Key: k, Value: v})
	}
	if op.Body == nil {
		return req, nil
	}

	if op.Body.Kind == "multipart" {
		mp := &client.Multipart{}
		needsFile := false
		for _, fl := range op.Body.Fields {
			if fl.Binary {
				needsFile = true
				p, _ := f.GetString(fl.Flag)
				if p == "" {
					continue
				}
				p = expandHome(p)
				if st, err := os.Stat(p); err != nil || st.IsDir() {
					return nil, apperr.Usagef("--%s: file not found: %s", fl.Flag, p)
				}
				mp.Files = append(mp.Files, client.KV{Key: fl.Name, Value: p})
				continue
			}
			vals, err := flagValues(f, fl.Flag, fl.Type, fl.Array, fl.Enum)
			if err != nil {
				return nil, err
			}
			for _, v := range vals {
				mp.Fields = append(mp.Fields, client.KV{Key: fl.Name, Value: queryString(v)})
			}
		}
		if needsFile && len(mp.Files) == 0 {
			var names []string
			for _, fl := range op.Body.Fields {
				if fl.Binary {
					names = append(names, "--"+fl.Flag)
				}
			}
			return nil, apperr.Usagef("a file is required (%s)", strings.Join(names, ", "))
		}
		req.Multipart = mp
		return req, nil
	}

	var data any
	if s, _ := f.GetString(flagBody); s != "" {
		v, err := ReadBodyArg(s, a.In)
		if err != nil {
			return nil, err
		}
		data = v
	}
	sets, _ := f.GetStringArray(flagSet)
	if data == nil {
		if op.Body.IsArray {
			data = []any{}
		} else {
			data = ojson.NewObject()
		}
	}
	if obj := ojson.Obj(data); obj != nil {
		for _, fl := range op.Body.Fields {
			vals, err := flagValues(f, fl.Flag, fl.Type, fl.Array, fl.Enum)
			if err != nil {
				return nil, err
			}
			if vals == nil {
				continue
			}
			if fl.Array {
				obj.Set(fl.Name, vals)
			} else {
				obj.Set(fl.Name, vals[0])
			}
		}
		for _, item := range sets {
			k, v, err := parseKV(item, "--set")
			if err != nil {
				return nil, err
			}
			setPath(obj, k, jsonOrString(v))
		}
	} else if len(sets) > 0 {
		return nil, apperr.Usagef("--set cannot be combined with a JSON array --body")
	}
	empty := false
	switch t := data.(type) {
	case *ojson.Object:
		empty = t.Len() == 0
	case []any:
		empty = len(t) == 0
	}
	if op.Body.Required && empty {
		return nil, apperr.Usagef("a request body is required: use the field flags, --set or --body (see --body-template)")
	}
	if !empty || op.Body.Required {
		req.JSON = data
	}
	return req, nil
}

func (a *App) runOperation(cmd *cobra.Command, res *spec.Resource, op *spec.Command, args []string) error {
	f := cmd.Flags()
	if tmpl, _ := f.GetBool(flagBodyTemplate); tmpl {
		v, err := ojson.Decode(op.Body.Template)
		if err != nil {
			return err
		}
		if a.format() == "table" {
			b, _ := ojson.MarshalIndent(v)
			fmt.Fprintln(a.Out, string(b))
			return nil
		}
		return a.Render(v, nil, "")
	}
	req, err := buildRequest(a, cmd, op, args)
	if err != nil {
		return err
	}
	if a.DryRun {
		return a.printPlan(req, op.Destructive)
	}
	c, err := a.Client()
	if err != nil {
		return err
	}
	if op.Destructive {
		target := strings.Join(args, " ")
		if target == "" {
			target = "the selected items"
		}
		if err := a.Confirm(fmt.Sprintf("About to %s %s %s", strings.ReplaceAll(op.Verb, "-", " "), res.Name, target)); err != nil {
			return err
		}
	}

	if all, _ := f.GetBool(flagAll); all {
		pageSize := 100
		if fl := f.Lookup("page-size"); fl != nil && fl.Changed {
			n, _ := f.GetInt64("page-size")
			pageSize = int(n)
		}
		maxPages, _ := f.GetInt(flagMaxPages)
		progress := func(page, pages, total int) {
			if a.ErrTTY && !a.Quiet {
				fmt.Fprintf(a.Err, "\r  fetched page %d/%d (%d records)", page, pages, total)
				if page >= pages || (maxPages > 0 && page >= maxPages) {
					fmt.Fprintln(a.Err)
				}
			}
		}
		items, err := c.Paginate(req.Path, req.Query, pageSize, maxPages, progress)
		if err != nil {
			return err
		}
		return a.Render(items, nil, res.Name)
	}

	resp, err := c.Do(*req)
	if err != nil {
		return err
	}
	data := resp.Data()
	if o := ojson.Obj(data); o != nil && op.Paged {
		if pc, ok := o.Values["pageCount"].(json.Number); ok {
			pages, _ := pc.Int64()
			pn, _ := o.Values["pageNumber"].(json.Number)
			total, _ := o.Values["totalRecordCount"].(json.Number)
			if pages > 1 {
				a.Hint(fmt.Sprintf("Page %s/%d (%s records total). Use --all to fetch everything, or --page N.",
					pn, pages, total))
			}
		}
	}
	if data == nil {
		if a.machineOutput() {
			o := ojson.NewObject()
			o.Set("ok", true)
			o.Set("status", resp.Status)
			return a.Render(o, nil, "")
		}
		a.Success(fmt.Sprintf("%s %s → HTTP %d", op.Method, req.Path, resp.Status))
		return nil
	}
	if err := a.Render(data, resp.Body, res.Name); err != nil {
		return err
	}
	if s, ok := data.(string); ok && op.Method != "GET" && a.format() == "table" && len(s) == 36 {
		a.Hint("Returned id " + s + ". Asynchronous jobs can be followed with: epm-cli tasks get " + s)
	}
	return nil
}

// printPlan prints what a request would do (--dry-run).
func (a *App) printPlan(req *client.Request, destructive bool) error {
	plan := ojson.NewObject()
	plan.Set("method", req.Method)
	if c, err := a.Client(); err == nil {
		plan.Set("url", c.BuildURL(req.Path, req.Query))
	} else {
		plan.Set("path", req.Path)
		if len(req.Query) > 0 {
			q := make([]any, len(req.Query))
			for i, kv := range req.Query {
				q[i] = []any{kv.Key, kv.Value}
			}
			plan.Set("query", q)
		}
	}
	if req.JSON != nil {
		plan.Set("body", req.JSON)
	}
	if req.Multipart != nil {
		mp := ojson.NewObject()
		fields, files := ojson.NewObject(), ojson.NewObject()
		for _, kv := range req.Multipart.Fields {
			fields.Set(kv.Key, kv.Value)
		}
		for _, kv := range req.Multipart.Files {
			files.Set(kv.Key, kv.Value)
		}
		mp.Set("fields", fields)
		mp.Set("files", files)
		plan.Set("multipart", mp)
	}
	if destructive {
		plan.Set("destructive", true)
	}
	b, _ := ojson.MarshalIndent(plan)
	fmt.Fprintln(a.Out, string(b))
	return nil
}
