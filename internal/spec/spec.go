// Package spec holds the command table generated from the EPM OpenAPI spec.
//
// Regenerate with:  go generate ./...   (or: make generate)
package spec

//go:generate go run ../../tools/specgen gen -spec ../../spec/openapi-v3.json -out commands.json

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed commands.json
var commandsJSON []byte

type Table struct {
	Title     string      `json:"title"`
	Version   string      `json:"version"`
	Resources []*Resource `json:"resources"`
}

type Resource struct {
	Name     string     `json:"name"`
	Tag      string     `json:"tag"`
	Commands []*Command `json:"commands"`
}

type Command struct {
	Verb           string      `json:"verb"`
	Method         string      `json:"method"`
	Path           string      `json:"path"`
	Summary        string      `json:"summary"`
	Description    string      `json:"description,omitempty"`
	Tag            string      `json:"tag"`
	PathParams     []PathParam `json:"path_params"`
	QueryParams    []Param     `json:"query_params"`
	Body           *Body       `json:"body,omitempty"`
	ResponseSchema string      `json:"response_schema,omitempty"`
	Paged          bool        `json:"paged"`
	IsList         bool        `json:"is_list"`
	Destructive    bool        `json:"destructive"`
	DefaultSort    string      `json:"default_sort,omitempty"`
}

type PathParam struct {
	Name string `json:"name"`
	Help string `json:"help,omitempty"`
}

type Param struct {
	Name     string   `json:"name"`
	Flag     string   `json:"flag"`
	Type     string   `json:"type"`
	Format   string   `json:"format,omitempty"`
	Array    bool     `json:"array"`
	Enum     []string `json:"enum,omitempty"`
	Required bool     `json:"required"`
	Help     string   `json:"help,omitempty"`
}

type Body struct {
	Kind     string          `json:"kind"` // "json" or "multipart"
	Schema   string          `json:"schema,omitempty"`
	Required bool            `json:"required"`
	IsArray  bool            `json:"is_array"`
	Fields   []Field         `json:"fields"`
	Template json.RawMessage `json:"template,omitempty"`
}

type Field struct {
	Name   string   `json:"name"`
	Flag   string   `json:"flag"`
	Type   string   `json:"type"`
	Format string   `json:"format,omitempty"`
	Array  bool     `json:"array"`
	Binary bool     `json:"binary"`
	Enum   []string `json:"enum,omitempty"`
	Help   string   `json:"help,omitempty"`
}

var (
	once  sync.Once
	table *Table
)

// Load returns the embedded command table.
func Load() *Table {
	once.Do(func() {
		table = &Table{}
		if err := json.Unmarshal(commandsJSON, table); err != nil {
			panic("embedded commands.json is invalid: " + err.Error())
		}
	})
	return table
}

func (t *Table) Resource(name string) *Resource {
	for _, r := range t.Resources {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func (r *Resource) Command(verb string) *Command {
	for _, c := range r.Commands {
		if c.Verb == verb {
			return c
		}
	}
	return nil
}

// Verbs lists the verbs of a resource in table order.
func (r *Resource) Verbs() []string {
	out := make([]string, len(r.Commands))
	for i, c := range r.Commands {
		out[i] = c.Verb
	}
	return out
}
