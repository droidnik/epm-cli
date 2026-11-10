// Package output renders API data as table, json, ndjson, csv, yaml, plain or raw.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/droidnik/epm-cli/internal/ojson"
)

var Formats = []string{"table", "json", "ndjson", "csv", "yaml", "plain", "raw"}

// DefaultColumns are curated table columns for list views (--wide/--columns override).
var DefaultColumns = map[string][]string{
	"computers": {"id", "host", "os", "domain", "agentVersion", "authorisationState", "connectionStatus",
		"groupName", "policyName", "lastConnected"},
	"groups": {"id", "name", "computerCount", "activeComputers", "policyName", "revision",
		"policyRevisionStatus", "default"},
	"policies": {"id", "name", "revision", "totalAssignedRevisions", "isAssignedToGroup", "hasOpenDraft",
		"lastModified", "lastModifiedUser"},
	"users":            {"id", "accountName", "emailAddress", "userType", "disabled", "lastSignedIn"},
	"accepted-domains": {"id", "domain", "created"},
	"activity-audits":  {"id", "created", "auditType", "entity", "entityName", "changedBy", "user"},
	"api-accounts": {"id", "name", "clientId", "createdDate", "managementAccess", "scimAccess",
		"reportingAccess", "auditAccess"},
	"global-roles": {"id", "name", "isBuiltIn", "description"},
	"roles":        {"id", "name"},
	"authorization-request-audits": {"id", "ticketId", "user", "computerName", "productName", "decision",
		"timeOfRequest", "duration"},
	"scim-users": {"id", "userName", "displayName", "active"},
}

type Renderer struct {
	Format   string
	Out      io.Writer
	Color    bool
	TTY      bool
	Width    int // terminal width (0 = unknown)
	Columns  []string
	Wide     bool
	Resource string
	Hint     func(string)
}

// Unwrap strips paging envelopes: PagedResponse.data and SCIM ListResponse.Resources.
func Unwrap(v any) any {
	o := ojson.Obj(v)
	if o == nil {
		return v
	}
	if d, ok := o.Values["data"].([]any); ok {
		if _, a := o.Get("pageNumber"); a {
			return d
		}
		if _, b := o.Get("pageSize"); b {
			return d
		}
	}
	if r, ok := o.Values["Resources"].([]any); ok {
		if _, t := o.Get("totalResults"); t {
			return r
		}
	}
	return v
}

// Render writes data. raw is the original body (used by -o raw); field extracts one path.
func (r *Renderer) Render(data any, raw []byte, field string) error {
	if r.Hint == nil {
		r.Hint = func(string) {}
	}
	if r.Format == "raw" {
		if raw != nil {
			_, err := r.Out.Write(raw)
			return err
		}
		return r.json(data)
	}
	if b, ok := data.([]byte); ok {
		_, err := r.Out.Write(b)
		return err
	}
	data = Unwrap(data)
	if field != "" {
		list, isList := data.([]any)
		if !isList {
			list = []any{data}
		}
		vals := make([]any, len(list))
		for i, item := range list {
			vals[i], _ = ojson.Path(item, field)
		}
		switch r.Format {
		case "json", "yaml", "ndjson":
			if isList {
				data = vals
			} else {
				data = vals[0]
			}
		default:
			for _, v := range vals {
				switch v.(type) {
				case *ojson.Object, []any:
					b, _ := ojson.Marshal(v)
					fmt.Fprintln(r.Out, string(b))
				default:
					fmt.Fprintln(r.Out, Cell(v))
				}
			}
			return nil
		}
	}
	switch r.Format {
	case "json":
		return r.json(data)
	case "ndjson":
		list, ok := data.([]any)
		if !ok {
			list = []any{data}
		}
		for _, item := range list {
			b, err := ojson.Marshal(item)
			if err != nil {
				return err
			}
			fmt.Fprintln(r.Out, string(b))
		}
		return nil
	case "yaml":
		fmt.Fprintln(r.Out, ToYAML(data, 0))
		return nil
	case "csv":
		return r.csv(data)
	case "plain":
		return r.plain(data)
	default:
		return r.table(data)
	}
}

func (r *Renderer) json(v any) error {
	b, err := ojson.MarshalIndent(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.Out, string(b))
	return err
}

// Flatten turns nested objects into dotted keys (two levels deep).
func Flatten(o *ojson.Object) *ojson.Object {
	out := ojson.NewObject()
	flattenInto(out, o, "", 0)
	return out
}

func flattenInto(out, o *ojson.Object, prefix string, depth int) {
	for _, k := range o.Keys {
		v := o.Values[k]
		if sub := ojson.Obj(v); sub != nil && depth < 2 && sub.Len() > 0 {
			flattenInto(out, sub, prefix+k+".", depth+1)
		} else {
			out.Set(prefix+k, v)
		}
	}
}

// Cell formats a value for table/csv/plain output.
func Cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return t
	case json.Number:
		return t.String()
	case []any:
		scalars := true
		for _, x := range t {
			switch x.(type) {
			case *ojson.Object, []any:
				scalars = false
			}
		}
		if !scalars {
			return fmt.Sprintf("[%d items]", len(t))
		}
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = Cell(x)
		}
		return strings.Join(parts, ", ")
	case *ojson.Object:
		b, _ := ojson.Marshal(t)
		return string(b)
	default:
		return fmt.Sprint(t)
	}
}

type row struct {
	flat *ojson.Object
	orig any
}

func (rw row) get(col string) any {
	if v, ok := rw.flat.Get(col); ok {
		return v
	}
	v, _ := ojson.Path(rw.orig, col)
	return v
}

func (r *Renderer) rows(list []any) ([]string, []row) {
	rows := make([]row, len(list))
	var cols []string
	seen := map[string]bool{}
	for i, item := range list {
		var flat *ojson.Object
		if o := ojson.Obj(item); o != nil {
			flat = Flatten(o)
		} else {
			flat = ojson.NewObject()
			flat.Set("value", item)
		}
		rows[i] = row{flat, item}
		for _, k := range flat.Keys {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	if len(r.Columns) > 0 {
		return r.Columns, rows
	}
	return cols, rows
}

func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return []any{v}
}

func (r *Renderer) csv(data any) error {
	cols, rows := r.rows(asList(data))
	w := csv.NewWriter(r.Out)
	_ = w.Write(cols)
	for _, rw := range rows {
		rec := make([]string, len(cols))
		for i, c := range cols {
			rec[i] = Cell(rw.get(c))
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return w.Error()
}

func (r *Renderer) plain(data any) error {
	if o := ojson.Obj(data); o != nil {
		f := Flatten(o)
		for _, k := range f.Keys {
			fmt.Fprintf(r.Out, "%s\t%s\n", k, Cell(f.Values[k]))
		}
		return nil
	}
	list, ok := data.([]any)
	if !ok {
		fmt.Fprintln(r.Out, Cell(data))
		return nil
	}
	cols, rows := r.rows(list)
	for _, rw := range rows {
		vals := make([]string, len(cols))
		for i, c := range cols {
			vals[i] = Cell(rw.get(c))
		}
		fmt.Fprintln(r.Out, strings.Join(vals, "\t"))
	}
	return nil
}

func (r *Renderer) bold(s string) string {
	if r.Color {
		return "\033[1m" + s + "\033[0m"
	}
	return s
}

func (r *Renderer) table(data any) error {
	switch t := data.(type) {
	case nil:
		return nil
	case *ojson.Object:
		f := Flatten(t)
		width := 0
		for _, k := range f.Keys {
			width = max(width, utf8.RuneCountInString(k))
		}
		for _, k := range f.Keys {
			fmt.Fprintf(r.Out, "%s  %s\n", r.bold(pad(k, width)), Cell(f.Values[k]))
		}
		return nil
	case []any:
		if len(t) == 0 {
			r.Hint("No results.")
			return nil
		}
		allScalar := true
		for _, x := range t {
			if ojson.Obj(x) != nil {
				allScalar = false
				break
			}
		}
		if allScalar {
			for _, x := range t {
				fmt.Fprintln(r.Out, Cell(x))
			}
			return nil
		}
		cols, rows := r.rows(t)
		if len(r.Columns) == 0 && !r.Wide {
			if pref, ok := DefaultColumns[r.Resource]; ok {
				var keep []string
				for _, c := range pref {
					for _, rw := range rows {
						if _, ok := rw.flat.Get(c); ok {
							keep = append(keep, c)
							break
						}
					}
				}
				if len(keep) > 0 {
					cols = keep
				}
			}
		}
		if len(r.Columns) == 0 {
			// Hide columns that are empty in every row.
			var nonEmpty []string
			for _, c := range cols {
				for _, rw := range rows {
					if Cell(rw.get(c)) != "" {
						nonEmpty = append(nonEmpty, c)
						break
					}
				}
			}
			if len(nonEmpty) > 0 {
				cols = nonEmpty
			}
		}
		cells := make([][]string, len(rows))
		for i, rw := range rows {
			cells[i] = make([]string, len(cols))
			for j, c := range cols {
				cells[i][j] = Cell(rw.get(c))
			}
		}
		r.grid(cols, cells)
		return nil
	default:
		fmt.Fprintln(r.Out, Cell(data))
		return nil
	}
}

func pad(s string, w int) string {
	if n := utf8.RuneCountInString(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func (r *Renderer) grid(cols []string, cells [][]string) {
	// Long cells are only shortened for interactive terminals; pipes get full values.
	maxCell := 1 << 30
	if r.TTY && !r.Wide {
		maxCell = 60
	}
	for _, rowCells := range cells {
		for j, c := range rowCells {
			c = strings.ReplaceAll(c, "\n", " ")
			if utf8.RuneCountInString(c) > maxCell {
				c = string([]rune(c)[:maxCell-1]) + "…"
			}
			rowCells[j] = c
		}
	}
	widths := make([]int, len(cols))
	for j, h := range cols {
		widths[j] = utf8.RuneCountInString(h)
		for _, rowCells := range cells {
			widths[j] = max(widths[j], utf8.RuneCountInString(rowCells[j]))
		}
	}
	if r.TTY && r.Width > 0 && !r.Wide && len(r.Columns) == 0 {
		keep, total := 0, 0
		for _, w := range widths {
			sep := 0
			if keep > 0 {
				sep = 2
			}
			if keep > 0 && total+w+sep > r.Width {
				break
			}
			total += w + sep
			keep++
		}
		if keep < len(cols) {
			r.Hint(fmt.Sprintf("%d column(s) hidden to fit the terminal: %s (use --wide, --columns or -o json)",
				len(cols)-keep, strings.Join(cols[keep:], ", ")))
			cols, widths = cols[:keep], widths[:keep]
			for i := range cells {
				cells[i] = cells[i][:keep]
			}
		}
	}
	line := func(vals []string, upper bool) string {
		parts := make([]string, len(vals))
		for j, v := range vals {
			if upper {
				v = strings.ToUpper(v)
			}
			parts[j] = pad(v, widths[j])
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	fmt.Fprintln(r.Out, r.bold(line(cols, true)))
	for _, rowCells := range cells {
		fmt.Fprintln(r.Out, line(rowCells, false))
	}
}

// ------------------------------------------------------------------ YAML

var (
	yamlPlain  = regexp.MustCompile(`^[A-Za-z0-9_./@:+-][A-Za-z0-9_ ./@:+()-]*$`)
	yamlNumber = regexp.MustCompile(`^[-+]?[0-9][0-9_.eE+-]*$`)
	yamlReserv = map[string]bool{"true": true, "false": true, "yes": true, "no": true, "on": true, "off": true,
		"null": true, "~": true, "": true}
)

func yamlScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool, json.Number:
		return Cell(t)
	}
	s := fmt.Sprint(v)
	if yamlPlain.MatchString(s) && !yamlReserv[strings.ToLower(s)] && !strings.HasSuffix(s, ":") &&
		!strings.Contains(s, ": ") && !yamlNumber.MatchString(s) {
		return s
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// ToYAML renders a value as block-style YAML.
func ToYAML(v any, indent int) string {
	padStr := strings.Repeat("  ", indent)
	switch t := v.(type) {
	case *ojson.Object:
		if t.Len() == 0 {
			return padStr + "{}"
		}
		var lines []string
		for _, k := range t.Keys {
			val := t.Values[k]
			if isNonEmptyContainer(val) {
				lines = append(lines, padStr+yamlScalar(k)+":", ToYAML(val, indent+1))
			} else {
				lines = append(lines, padStr+yamlScalar(k)+": "+inline(val))
			}
		}
		return strings.Join(lines, "\n")
	case []any:
		if len(t) == 0 {
			return padStr + "[]"
		}
		var lines []string
		for _, item := range t {
			if isNonEmptyContainer(item) {
				lines = append(lines, padStr+"- "+strings.TrimLeft(ToYAML(item, indent+1), " "))
			} else {
				lines = append(lines, padStr+"- "+inline(item))
			}
		}
		return strings.Join(lines, "\n")
	default:
		return padStr + yamlScalar(v)
	}
}

func isNonEmptyContainer(v any) bool {
	switch t := v.(type) {
	case *ojson.Object:
		return t.Len() > 0
	case []any:
		return len(t) > 0
	}
	return false
}

func inline(v any) string {
	switch t := v.(type) {
	case *ojson.Object:
		return "{}"
	case []any:
		return "[]"
	default:
		return yamlScalar(t)
	}
}
