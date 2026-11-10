package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/droidnik/epm-cli/internal/spec"
)

// Resource groups (same order as `epm-cli --help`).
var docGroups = []struct {
	Title string
	Names []string
}{
	{"Endpoints", []string{"computers", "groups", "tasks"}},
	{"Policies", []string{"policies", "policy-editor"}},
	{"Requests & Events", []string{"admin-access-requests", "authorization-requests", "authorization-request-audits", "events", "activity-audits"}},
	{"Users & Access", []string{"users", "roles", "global-roles", "api-accounts", "accepted-domains", "authentication-provider"}},
	{"SCIM", []string{"scim-users", "scim-roles", "scim-schemas", "scim-resource-types", "scim-service-provider-config"}},
	{"Tenant", []string{"about", "api-definition", "app-switcher"}},
}

func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

func typeLabel(typ, format string, array bool, enum []string) string {
	t := map[string]string{"string": "string", "integer": "integer", "number": "number", "boolean": "switch", "object": "object"}[typ]
	if t == "" {
		t = typ
	}
	switch format {
	case "uuid":
		t = "UUID"
	case "date-time":
		t = "date-time"
	}
	if array {
		t = "list of " + t
	}
	return t
}

func values(enum []string) string {
	if len(enum) == 0 {
		return ""
	}
	parts := make([]string, len(enum))
	for i, e := range enum {
		parts[i] = "`" + e + "`"
	}
	return strings.Join(parts, ", ")
}

func placeholder(name string) string { return "<" + strings.ToUpper(name) + ">" }

func exampleFor(r *spec.Resource, c *spec.Command) string {
	parts := []string{"epm-cli", r.Name, c.Verb}
	for _, p := range c.PathParams {
		parts = append(parts, placeholder(p.Name))
	}
	for _, q := range c.QueryParams {
		if q.Required {
			v := "<VALUE>"
			if len(q.Enum) > 0 {
				v = q.Enum[0]
			}
			parts = append(parts, "--"+q.Flag, v)
		}
	}
	if c.Body != nil {
		for _, f := range c.Body.Fields {
			if f.Binary {
				parts = append(parts, "--"+f.Flag, "./file")
			}
		}
		if c.Body.Kind == "json" && len(c.Body.Fields) > 0 {
			f := c.Body.Fields[0]
			v := "<VALUE>"
			switch {
			case len(f.Enum) > 0:
				v = f.Enum[0]
			case f.Type == "boolean":
				v = ""
			case f.Array:
				v = "<ID1>,<ID2>"
			}
			if v == "" {
				parts = append(parts, "--"+f.Flag)
			} else {
				parts = append(parts, "--"+f.Flag, v)
			}
		} else if c.Body.Kind == "json" {
			parts = append(parts, "--body", "@body.json")
		}
	}
	if c.Paged {
		parts = append(parts, "--all")
	}
	if c.Method == "GET" && c.IsList {
		parts = append(parts, "-o", "json")
	}
	if c.Destructive {
		parts = append(parts, "--dry-run")
	}
	return strings.Join(parts, " ")
}

func writeCommand(b *bytes.Buffer, r *spec.Resource, c *spec.Command) {
	fmt.Fprintf(b, "## `%s %s`\n\n", r.Name, c.Verb)
	fmt.Fprintf(b, "%s\n\n", mdEscape(c.Summary))
	if c.Description != "" {
		fmt.Fprintf(b, "%s\n\n", mdEscape(c.Description))
	}
	if c.Destructive {
		b.WriteString("> **Destructive:** asks for confirmation before it runs; scripts must pass `--yes`. Preview with `--dry-run` first.\n\n")
	}
	usage := "epm-cli " + r.Name + " " + c.Verb
	for _, p := range c.PathParams {
		usage += " " + placeholder(p.Name)
	}
	usage += " [flags]"
	fmt.Fprintf(b, "```\n%s\n```\n\n", usage)
	fmt.Fprintf(b, "API: `%s %s`", c.Method, c.Path)
	if c.ResponseSchema != "" {
		fmt.Fprintf(b, " · Response: `%s`", c.ResponseSchema)
	}
	b.WriteString("\n\n")

	if len(c.PathParams) > 0 {
		b.WriteString("**Arguments**\n\n| Argument | Description |\n|---|---|\n")
		for _, p := range c.PathParams {
			help := p.Help
			if help == "" {
				help = p.Name
			}
			fmt.Fprintf(b, "| `%s` | %s |\n", placeholder(p.Name), mdEscape(help))
		}
		b.WriteString("\n")
	}

	if len(c.QueryParams) > 0 || c.Paged {
		b.WriteString("**Filters and parameters**\n\n| Flag | Type | Values | Description |\n|---|---|---|---|\n")
		for _, q := range c.QueryParams {
			if q.Name == "Sorts" {
				b.WriteString("| `--sort FIELD[:asc\\|desc]` | repeatable | | sort order, e.g. `--sort created:desc` |\n")
				continue
			}
			help := q.Help
			if help == "" || help == q.Name {
				help = "`" + q.Name + "`"
			}
			req := ""
			if q.Required {
				req = " **(required)**"
			}
			fmt.Fprintf(b, "| `--%s` | %s | %s | %s%s |\n", q.Flag, typeLabel(q.Type, q.Format, q.Array, q.Enum),
				values(q.Enum), mdEscape(help), req)
		}
		if c.Paged {
			b.WriteString("| `--all`, `-A` | switch | | fetch every page |\n")
			b.WriteString("| `--max-pages` | integer | | with `--all`: stop after N pages |\n")
		}
		b.WriteString("\n")
	}

	if c.Body != nil {
		kind := "JSON"
		if c.Body.Kind == "multipart" {
			kind = "form with file upload"
		}
		fmt.Fprintf(b, "**Request body** (%s", kind)
		if c.Body.Schema != "" {
			fmt.Fprintf(b, ", `%s`", c.Body.Schema)
		}
		if c.Body.Required {
			b.WriteString(", required")
		}
		b.WriteString(")\n\n")
		if len(c.Body.Fields) > 0 {
			b.WriteString("| Flag | Field | Type | Values | Description |\n|---|---|---|---|---|\n")
			for _, f := range c.Body.Fields {
				t := typeLabel(f.Type, f.Format, f.Array, f.Enum)
				if f.Binary {
					t = "file path"
				}
				help := f.Help
				if help == "" {
					help = "–"
				}
				fmt.Fprintf(b, "| `--%s` | `%s` | %s | %s | %s |\n", f.Flag, f.Name, t, values(f.Enum), mdEscape(help))
			}
			b.WriteString("\n")
		}
		if c.Body.Kind == "json" {
			b.WriteString("Other or nested fields: `--set path=value`, or pass the whole body with `--body` (inline, `@file.json` or `-` for stdin). Example body (`--body-template`):\n\n")
			var pretty bytes.Buffer
			if json.Indent(&pretty, c.Body.Template, "", "  ") == nil {
				fmt.Fprintf(b, "```json\n%s\n```\n\n", pretty.String())
			}
		}
	}
	fmt.Fprintf(b, "**Example**\n\n```sh\n%s\n```\n\n", exampleFor(r, c))
}

func writeDocs(in, outDir string) error {
	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	var t spec.Table
	if err := json.Unmarshal(raw, &t); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	// Remove stale pages of resources that no longer exist.
	old, _ := filepath.Glob(filepath.Join(outDir, "*.md"))
	for _, f := range old {
		_ = os.Remove(f)
	}
	byName := map[string]*spec.Resource{}
	for _, r := range t.Resources {
		byName[r.Name] = r
	}
	header := "<!-- Generated by `make docs` from internal/spec/commands.json — do not edit by hand. -->\n\n"

	var idx bytes.Buffer
	idx.WriteString(header)
	idx.WriteString("# Command reference\n\n")
	fmt.Fprintf(&idx, "All %d API commands of epm-cli, generated from the OpenAPI specification (%s %s).\n",
		countCommands(&t), t.Title, t.Version)
	idx.WriteString("Global flags (`-o`, `--field`, `--dry-run`, …) work with every command: see [Basics](../02-basics.md).\n\n")
	listed := map[string]bool{}
	for _, g := range docGroups {
		fmt.Fprintf(&idx, "## %s\n\n| Resource | Commands |\n|---|---|\n", g.Title)
		for _, n := range g.Names {
			r := byName[n]
			if r == nil {
				continue
			}
			listed[n] = true
			var verbs []string
			for _, c := range r.Commands {
				anchor := strings.ReplaceAll(r.Name+"-"+c.Verb, " ", "-")
				verbs = append(verbs, fmt.Sprintf("[%s](%s.md#%s)", c.Verb, r.Name, anchor))
			}
			fmt.Fprintf(&idx, "| [%s](%s.md) | %s |\n", r.Name, r.Name, strings.Join(verbs, " · "))
		}
		idx.WriteString("\n")
	}
	var rest []string
	for n := range byName {
		if !listed[n] {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	if len(rest) > 0 {
		idx.WriteString("## Other\n\n")
		for _, n := range rest {
			fmt.Fprintf(&idx, "- [%s](%s.md)\n", n, n)
		}
		idx.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(outDir, "README.md"), idx.Bytes(), 0o644); err != nil {
		return err
	}

	for _, r := range t.Resources {
		var b bytes.Buffer
		b.WriteString(header)
		fmt.Fprintf(&b, "# %s\n\n", r.Name)
		fmt.Fprintf(&b, "[← Command reference](README.md) · API tag `%s` · %d commands\n\n", r.Tag, len(r.Commands))
		b.WriteString("| Command | Description |\n|---|---|\n")
		for _, c := range r.Commands {
			mark := ""
			if c.Destructive {
				mark = " ⚠️"
			}
			fmt.Fprintf(&b, "| [`%s`](#%s) | %s%s |\n", c.Verb, r.Name+"-"+c.Verb, mdEscape(shorten(c.Summary, 100)), mark)
		}
		b.WriteString("\n")
		for _, c := range r.Commands {
			writeCommand(&b, r, c)
		}
		if err := os.WriteFile(filepath.Join(outDir, r.Name+".md"), b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d resource pages\n", outDir, len(t.Resources))
	return nil
}

func countCommands(t *spec.Table) int {
	n := 0
	for _, r := range t.Resources {
		n += len(r.Commands)
	}
	return n
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
