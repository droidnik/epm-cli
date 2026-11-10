package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/droidnik/epm-cli/internal/ojson"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	v, err := ojson.Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func render(t *testing.T, format, data string, opts ...func(*Renderer)) string {
	t.Helper()
	var buf bytes.Buffer
	r := &Renderer{Format: format, Out: &buf}
	for _, o := range opts {
		o(r)
	}
	if err := r.Render(decode(t, data), nil, ""); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestJSONKeepsKeyOrder(t *testing.T) {
	out := render(t, "json", `{"z":1,"a":{"y":true,"b":null}}`)
	if strings.Index(out, `"z"`) > strings.Index(out, `"a"`) || strings.Index(out, `"y"`) > strings.Index(out, `"b"`) {
		t.Fatalf("key order lost:\n%s", out)
	}
}

func TestUnwrapPaged(t *testing.T) {
	out := render(t, "ndjson", `{"pageNumber":1,"pageSize":2,"totalRecordCount":2,"pageCount":1,"data":[{"id":1},{"id":2}]}`)
	if out != "{\"id\":1}\n{\"id\":2}\n" {
		t.Fatalf("got %q", out)
	}
	out = render(t, "ndjson", `{"schemas":[],"totalResults":1,"Resources":[{"id":"u"}]}`)
	if out != "{\"id\":\"u\"}\n" {
		t.Fatalf("scim unwrap %q", out)
	}
}

func TestTableDefaultColumnsAndFlatten(t *testing.T) {
	data := `[{"id":"1","host":"a","os":"Mac","errorInfo":null,"extra":"x"},{"id":"2","host":"b","os":"Windows","extra":"y"}]`
	out := render(t, "table", data, func(r *Renderer) { r.Resource = "computers" })
	header := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if strings.Join(header, ",") != "ID,HOST,OS" {
		t.Fatalf("header %v", header)
	}
	out = render(t, "table", `{"a":{"b":{"c":1}},"list":[1,2]}`)
	if !strings.Contains(out, "a.b.c") || !strings.Contains(out, "1, 2") {
		t.Fatalf("kv table %q", out)
	}
}

func TestTableTruncatesToWidth(t *testing.T) {
	var hint string
	out := render(t, "table", `[{"aaaaaaaaaa":"1","bbbbbbbbbb":"2","cccccccccc":"3"}]`, func(r *Renderer) {
		r.TTY, r.Width = true, 25
		r.Hint = func(s string) { hint = s }
	})
	if strings.Contains(out, "CCCC") || !strings.Contains(hint, "cccccccccc") {
		t.Fatalf("out %q hint %q", out, hint)
	}
}

func TestCSVAndPlain(t *testing.T) {
	out := render(t, "csv", `[{"id":1,"tags":["a","b"]},{"id":2,"n":{"x":"y"}}]`)
	want := "id,tags,n.x\n1,\"a, b\",\n2,,y\n"
	if out != want {
		t.Fatalf("csv\n got %q\nwant %q", out, want)
	}
	if out := render(t, "plain", `{"a":1,"b":"x"}`); out != "a\t1\nb\tx\n" {
		t.Fatalf("plain %q", out)
	}
}

func TestYAML(t *testing.T) {
	out := render(t, "yaml", `{"name":"Pilot","default":true,"count":3,"empty":"","ver":"1.0","items":[{"a":1,"b":[]},"x: y"],"o":{}}`)
	want := `name: Pilot
default: true
count: 3
empty: ""
ver: "1.0"
items:
  - a: 1
    b: []
  - "x: y"
o: {}
`
	if out != want {
		t.Fatalf("yaml\n got:\n%s\nwant:\n%s", out, want)
	}
}

func TestField(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Format: "table", Out: &buf}
	_ = r.Render(decode(t, `[{"a":{"b":"x"}},{"a":{"b":"y"}}]`), nil, "a.b")
	if buf.String() != "x\ny\n" {
		t.Fatalf("got %q", buf.String())
	}
}
