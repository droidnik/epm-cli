package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/droidnik/epm-cli/internal/ojson"
	"github.com/droidnik/epm-cli/internal/spec"
)

// MCP (Model Context Protocol) server over stdio: newline-delimited JSON-RPC 2.0.
//
// Read-only by default: non-GET operations need `epm-cli mcp --allow-writes`, and
// destructive operations additionally need confirm=true in the tool call.

const mcpProtocolVersion = "2025-06-18"

// Built-in commands that only read state (by command path without "epm-cli ").
var mcpReadOnly = map[string]bool{
	"commands": true, "overview": true, "doctor": true, "version": true, "agent-context": true, "help": true,
	"config list": true, "config show": true, "config path": true, "auth status": true,
}

var mcpTools = []map[string]any{
	{
		"name": "list_commands",
		"description": "List BeyondTrust EPM API operations available as epm-cli commands " +
			"(command, HTTP method, path, summary, destructive flag).",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"filter": map[string]any{"type": "string", "description": "substring filter, e.g. 'computers' or 'policy'"}}},
	},
	{
		"name": "describe_command",
		"description": "Show path/query parameters, body fields and an example body for one command, " +
			"e.g. 'computers list' or 'groups assign-computers'.",
		"inputSchema": map[string]any{"type": "object", "required": []string{"command"}, "properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "'<resource> <verb>'"}}},
	},
	{
		"name": "run_command",
		"description": "Run an epm-cli command and return its output (JSON by default). Example args: " +
			`["computers", "list", "--os", "Mac", "--all"]. Write operations are only allowed when the server ` +
			"was started with --allow-writes; destructive ones also need confirm=true. Use --dry-run first for writes.",
		"inputSchema": map[string]any{"type": "object", "required": []string{"args"}, "properties": map[string]any{
			"args": map[string]any{
				"oneOf":       []any{map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, map[string]any{"type": "string"}},
				"description": "argument vector (array) or a shell-style string, without 'epm-cli'"},
			"confirm": map[string]any{"type": "boolean", "description": "confirm a destructive operation (adds --yes)"},
			"output":  map[string]any{"type": "string", "enum": []string{"json", "ndjson", "table", "csv", "yaml"}, "default": "json"},
		}},
	},
}

type MCPServer struct {
	AllowWrites bool
	Profile     string
}

func mcpCmd(a *App) *cobra.Command {
	var allowWrites bool
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Expose epm-cli to AI clients via the Model Context Protocol (stdio)",
		Long: `Run an MCP server on stdio with the tools list_commands, describe_command and run_command.

Read-only by default. --allow-writes permits write operations; destructive operations
additionally need confirm=true in the tool call.

Claude Code:  claude mcp add epm-cli -- epm-cli mcp
              claude mcp add epm-cli -- epm-cli mcp --profile sandbox --allow-writes`,
		Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error {
			mode := "read-only"
			if allowWrites {
				mode = "read-write"
			}
			fmt.Fprintf(os.Stderr, "epm-cli MCP server %s on stdio (%s)\n", Version, mode)
			s := &MCPServer{AllowWrites: allowWrites, Profile: a.Profile}
			return s.Serve(a.In, a.Out)
		},
	}
	c.Flags().BoolVar(&allowWrites, "allow-writes", false, "allow non-GET operations (default: read-only)")
	return c
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func rpcError(id json.RawMessage, code int, msg string) map[string]any {
	var idv any = nil
	if len(id) > 0 {
		idv = id
	}
	return map[string]any{"jsonrpc": "2.0", "id": idv, "error": map[string]any{"code": code, "message": msg}}
}

// Serve reads JSON-RPC messages line by line until EOF.
func (s *MCPServer) Serve(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if line[0] == '[' {
			var batch []rpcMsg
			if err := json.Unmarshal(line, &batch); err != nil {
				_ = enc.Encode(rpcError(nil, -32700, "parse error"))
				continue
			}
			var resps []any
			for _, m := range batch {
				if r := s.Handle(m); r != nil {
					resps = append(resps, r)
				}
			}
			if len(resps) > 0 {
				_ = enc.Encode(resps)
			}
			continue
		}
		var m rpcMsg
		if err := json.Unmarshal(line, &m); err != nil {
			_ = enc.Encode(rpcError(nil, -32700, "parse error"))
			continue
		}
		if r := s.Handle(m); r != nil {
			_ = enc.Encode(r)
		}
	}
	return sc.Err()
}

// Handle processes one message; nil for notifications.
func (s *MCPServer) Handle(m rpcMsg) (resp map[string]any) {
	if len(m.ID) == 0 || string(m.ID) == "null" {
		return nil // notification
	}
	defer func() {
		if r := recover(); r != nil {
			resp = rpcError(m.ID, -32603, fmt.Sprintf("internal error: %v", r))
		}
	}()
	var result any
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = mcpProtocolVersion
		}
		mode := "Server is READ-ONLY."
		if s.AllowWrites {
			mode = "Writes are ENABLED (destructive operations need confirm=true)."
		}
		result = map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "epm-cli", "version": Version},
			"instructions": "BeyondTrust EPM (Windows & Mac) management via epm-cli. Use list_commands / " +
				"describe_command to discover operations and run_command to execute them. " + mode,
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": mcpTools}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return rpcError(m.ID, -32602, "invalid params")
		}
		var text string
		var isErr bool
		var err error
		switch p.Name {
		case "list_commands":
			var args struct{ Filter string }
			_ = json.Unmarshal(p.Arguments, &args)
			b, _ := ojson.MarshalIndent(commandRows(args.Filter))
			text = string(b)
		case "describe_command":
			var args struct{ Command string }
			_ = json.Unmarshal(p.Arguments, &args)
			text, err = describeCommand(args.Command)
		case "run_command":
			text, isErr, err = s.runCommand(p.Arguments)
		default:
			return rpcError(m.ID, -32602, "unknown tool: "+p.Name)
		}
		if err != nil {
			text, isErr = "error: "+err.Error(), true
		}
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": isErr}
	default:
		return rpcError(m.ID, -32601, "method not found: "+m.Method)
	}
	return map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result}
}

func describeCommand(command string) (string, error) {
	parts := strings.Fields(command)
	if len(parts) != 2 {
		return "", fmt.Errorf("expected '<resource> <verb>'; use list_commands")
	}
	res := spec.Load().Resource(parts[0])
	if res == nil || res.Command(parts[1]) == nil {
		return "", fmt.Errorf("unknown command %q; use list_commands", command)
	}
	c := res.Command(parts[1])
	o := ojson.NewObject()
	o.Set("command", command)
	o.Set("method", c.Method)
	o.Set("path", c.Path)
	o.Set("summary", c.Summary)
	o.Set("description", c.Description)
	o.Set("destructive", c.Destructive)
	pos := []any{}
	for _, p := range c.PathParams {
		pos = append(pos, p.Name)
	}
	o.Set("positional", pos)
	flags := []any{}
	for _, q := range c.QueryParams {
		f := ojson.NewObject()
		name := "--" + q.Flag
		if q.Name == "Sorts" {
			name = "--sort"
		}
		f.Set("flag", name)
		f.Set("param", q.Name)
		f.Set("type", q.Type)
		f.Set("array", q.Array)
		if len(q.Enum) > 0 {
			f.Set("enum", q.Enum)
		}
		f.Set("required", q.Required)
		f.Set("help", q.Help)
		flags = append(flags, f)
	}
	o.Set("flags", flags)
	o.Set("paged", c.Paged)
	if c.Body != nil {
		b := ojson.NewObject()
		b.Set("kind", c.Body.Kind)
		b.Set("schema", c.Body.Schema)
		b.Set("required", c.Body.Required)
		ff := []any{}
		for _, fl := range c.Body.Fields {
			f := ojson.NewObject()
			f.Set("flag", "--"+fl.Flag)
			f.Set("field", fl.Name)
			f.Set("type", fl.Type)
			f.Set("array", fl.Array)
			if len(fl.Enum) > 0 {
				f.Set("enum", fl.Enum)
			}
			f.Set("file", fl.Binary)
			f.Set("help", fl.Help)
			ff = append(ff, f)
		}
		b.Set("fieldFlags", ff)
		if t, err := ojson.Decode(c.Body.Template); err == nil {
			b.Set("template", t)
		}
		o.Set("body", b)
	}
	out, _ := ojson.MarshalIndent(o)
	return string(out), nil
}

// classify reports whether argv would write and whether it is destructive, without running it.
func classify(argv []string) (write, destructive bool, err error) {
	probe := &App{Out: io.Discard, Err: io.Discard, In: strings.NewReader("")}
	root := NewRootCmd(probe)
	cmd, rest, ferr := root.Find(argv)
	if ferr != nil || cmd == root {
		return false, false, nil // unknown command: harmless, running it prints the error
	}
	if err := cmd.ParseFlags(rest); err != nil {
		return false, false, nil // --help or flag error: harmless
	}
	dry := probe.DryRun
	if t, _ := cmd.Flags().GetBool(flagBodyTemplate); t {
		dry = true
	}
	path := strings.TrimPrefix(cmd.CommandPath(), "epm-cli ")
	if method, ok := cmd.Annotations["method"]; ok {
		return method != "GET" && !dry, cmd.Annotations["destructive"] == "true" && !dry, nil
	}
	switch {
	case path == "mcp":
		return false, false, fmt.Errorf("cannot start an MCP server from within MCP")
	case path == "auth token":
		return false, false, fmt.Errorf("auth token is not available over MCP")
	case path == "api":
		pos := cmd.Flags().Args()
		if len(pos) > 0 {
			m := strings.ToUpper(pos[0])
			return m != "GET" && !dry, m == "DELETE" && !dry, nil
		}
		return false, false, nil
	case mcpReadOnly[path] || strings.HasPrefix(path, "completion"):
		return false, false, nil
	}
	// Parent commands without a verb only print help.
	if cmd.HasSubCommands() {
		return false, false, nil
	}
	return true, false, nil
}

func (s *MCPServer) runCommand(raw json.RawMessage) (string, bool, error) {
	var p struct {
		Args    json.RawMessage `json:"args"`
		Confirm bool            `json:"confirm"`
		Output  string          `json:"output"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", true, fmt.Errorf("invalid arguments")
	}
	var argv []string
	if err := json.Unmarshal(p.Args, &argv); err != nil {
		var line string
		if err := json.Unmarshal(p.Args, &line); err != nil {
			return "", true, fmt.Errorf("args must be an array of strings or a string")
		}
		if argv, err = SplitArgs(line); err != nil {
			return "", true, err
		}
	}
	if len(argv) > 0 && argv[0] == "epm-cli" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return "", true, fmt.Errorf("args is empty")
	}
	write, destructive, err := classify(argv)
	if err != nil {
		return "", true, err
	}
	if write && !s.AllowWrites {
		return "refused: this is a write operation and the MCP server is read-only. Ask the user to start it " +
			"with `epm-cli mcp --allow-writes`, or add --dry-run to preview the request.", true, nil
	}
	extra := []string{"--no-input"}
	hasFlag := func(names ...string) bool {
		for _, a := range argv {
			for _, n := range names {
				if a == n || strings.HasPrefix(a, n+"=") {
					return true
				}
			}
		}
		return false
	}
	if !hasFlag("-o", "--output") {
		o := p.Output
		if o == "" {
			o = "json"
		}
		extra = append(extra, "-o", o)
	}
	if s.Profile != "" && !hasFlag("-p", "--profile") {
		extra = append(extra, "--profile", s.Profile)
	}
	if destructive && p.Confirm {
		extra = append(extra, "--yes")
	}
	var out, errBuf bytes.Buffer
	app := &App{Out: &out, Err: &errBuf, In: strings.NewReader("")}
	code := app.Execute(append(argv, extra...))
	text := out.String()
	if e := strings.TrimSpace(errBuf.String()); e != "" {
		if text != "" {
			text += "\n"
		}
		text += "[stderr]\n" + e
	}
	if code != 0 {
		text += fmt.Sprintf("\n[exit code %d]", code)
	}
	if text == "" {
		text = "(no output)"
	}
	return text, code != 0, nil
}

// SplitArgs splits a command line with simple single/double quote handling.
func SplitArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inArg := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in args")
	}
	if inArg {
		out = append(out, cur.String())
	}
	return out, nil
}
