package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/droidnik/epm-cli/internal/config"
)

type recorded struct {
	Method, Path, Auth, CType string
	Query                     url.Values
	RawQuery                  string
	Body                      []byte
}

type mockEPM struct {
	mu         sync.Mutex
	token      string
	tokenCalls int
	throttle   int
	requests   []recorded
	computers  []map[string]any
}

func newMock() *mockEPM {
	m := &mockEPM{}
	for i := 0; i < 5; i++ {
		os := "Windows"
		if i%2 == 1 {
			os = "Mac"
		}
		m.computers = append(m.computers, map[string]any{
			"id": fmt.Sprintf("00000000-0000-0000-0000-%012d", i), "host": fmt.Sprintf("host-%d", i), "os": os,
			"created": fmt.Sprintf("2026-01-%02dT00:00:00Z", i+1), "connectionStatus": "Connected",
		})
	}
	return m
}

func (m *mockEPM) apiRequests() []recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []recorded
	for _, r := range m.requests {
		if r.Path != "/oauth/connect/token" {
			out = append(out, r)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (m *mockEPM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.requests = append(m.requests, recorded{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"),
		CType: r.Header.Get("Content-Type"), Query: r.URL.Query(), RawQuery: r.URL.RawQuery, Body: body})
	m.mu.Unlock()

	if r.URL.Path == "/oauth/connect/token" {
		form, _ := url.ParseQuery(string(body))
		if form.Get("client_secret") != "s3cret" {
			writeJSON(w, 400, map[string]any{"error": "invalid_client"})
			return
		}
		m.mu.Lock()
		m.tokenCalls++
		m.token = fmt.Sprintf("tok%d", m.tokenCalls)
		tok := m.token
		m.mu.Unlock()
		writeJSON(w, 200, map[string]any{"access_token": tok, "expires_in": 3600, "token_type": "Bearer",
			"scope": form.Get("scope")})
		return
	}
	m.mu.Lock()
	ok := r.Header.Get("Authorization") == "Bearer "+m.token
	m.mu.Unlock()
	if !ok {
		writeJSON(w, 401, map[string]any{"title": "Unauthorized"})
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/management-api")
	q := r.URL.Query()
	switch {
	case r.Method == "GET" && p == "/v3/About":
		m.mu.Lock()
		throttled := m.throttle > 0
		if throttled {
			m.throttle--
		}
		m.mu.Unlock()
		if throttled {
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, map[string]any{"title": "Too Many Requests"})
			return
		}
		writeJSON(w, 200, map[string]any{"version": "25.4.0"})
	case r.Method == "GET" && p == "/v3/Computers":
		size, _ := strconv.Atoi(q.Get("Pagination.PageSize"))
		page, _ := strconv.Atoi(q.Get("Pagination.PageNumber"))
		if size == 0 {
			size = 2
		}
		if page == 0 {
			page = 1
		}
		var items []map[string]any
		for _, c := range m.computers {
			if os := q.Get("Filter.OS"); os == "" || c["os"] == os {
				items = append(items, c)
			}
		}
		start, end := (page-1)*size, page*size
		if start > len(items) {
			start = len(items)
		}
		if end > len(items) {
			end = len(items)
		}
		writeJSON(w, 200, map[string]any{"pageNumber": page, "pageSize": size, "totalRecordCount": len(items),
			"pageCount": (len(items) + size - 1) / size, "data": items[start:end]})
	case r.Method == "GET" && strings.HasPrefix(p, "/v3/Computers/") && strings.Count(p, "/") == 3:
		id := p[strings.LastIndex(p, "/")+1:]
		for _, c := range m.computers {
			if c["id"] == id {
				writeJSON(w, 200, c)
				return
			}
		}
		writeJSON(w, 404, map[string]any{"title": "Not Found", "detail": "Computer not found"})
	case r.Method == "GET" && strings.HasSuffix(p, "/Content"):
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("<?xml version='1.0'?><Policy/>"))
	case r.Method == "POST" && p == "/v3/Groups":
		writeJSON(w, 201, "11111111-2222-3333-4444-555555555555")
	case r.Method == "POST" && p == "/v3/Policies":
		writeJSON(w, 201, "99999999-2222-3333-4444-555555555555")
	case r.Method == "POST" && strings.HasSuffix(p, "/AssignComputers"):
		writeJSON(w, 201, "aaaaaaaa-2222-3333-4444-555555555555")
	case r.Method == "DELETE" || r.Method == "PATCH":
		w.WriteHeader(200)
	default:
		writeJSON(w, 404, map[string]any{"title": "Not Found"})
	}
}

type env struct {
	t    *testing.T
	mock *mockEPM
	srv  *httptest.Server
	dir  string
}

func setup(t *testing.T) *env {
	t.Helper()
	m := newMock()
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	t.Setenv("EPM_URL", srv.URL)
	t.Setenv("EPM_CLIENT_ID", "cid")
	t.Setenv("EPM_CLIENT_SECRET", "s3cret")
	t.Setenv("EPM_CLI_CONFIG_DIR", filepath.Join(dir, "cfg"))
	t.Setenv("EPM_CLI_CACHE_DIR", filepath.Join(dir, "cache"))
	t.Setenv("EPM_CLI_NO_KEYCHAIN", "1")
	t.Setenv("EPM_PROFILE", "")
	t.Setenv("EPM_OUTPUT", "")
	t.Setenv("NO_COLOR", "1")
	return &env{t, m, srv, dir}
}

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	a := &App{Out: &out, Err: &errb, In: strings.NewReader("")}
	code := a.Execute(args)
	return code, out.String(), errb.String()
}

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid JSON %q: %v", s, err)
	}
	return v
}

func TestAboutAndTokenCache(t *testing.T) {
	e := setup(t)
	code, out, errs := run("about", "show", "-o", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if v := mustJSON(t, out).(map[string]any); v["version"] != "25.4.0" {
		t.Fatalf("unexpected %v", v)
	}
	calls := e.mock.tokenCalls
	run("about", "show", "-o", "json")
	if e.mock.tokenCalls != calls {
		t.Fatal("token should come from the cache")
	}
	tokReq := e.mock.requests[0]
	form, _ := url.ParseQuery(string(tokReq.Body))
	if form.Get("grant_type") != "client_credentials" || form.Get("scope") != "urn:management:api" {
		t.Fatalf("bad token form: %v", form)
	}
	if !strings.HasPrefix(e.mock.apiRequests()[0].Auth, "Bearer tok") {
		t.Fatal("missing bearer token")
	}
	if st, _ := os.Stat(filepath.Join(e.dir, "cache", "tokens")); st == nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("token cache dir should be 0700")
	}
}

func TestReauthOn401(t *testing.T) {
	e := setup(t)
	run("about", "show")
	e.mock.mu.Lock()
	e.mock.token = "rotated" // server-side revocation
	e.mock.mu.Unlock()
	code, out, errs := run("about", "show", "-o", "json")
	if code != 0 || !strings.Contains(out, "25.4.0") {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
}

func TestRetryOn429(t *testing.T) {
	e := setup(t)
	e.mock.throttle = 2
	code, _, errs := run("about", "show", "-o", "json")
	if code != 0 || !strings.Contains(errs, "429") {
		t.Fatalf("exit %d err %q", code, errs)
	}
}

func TestListSinglePage(t *testing.T) {
	setup(t)
	code, out, errs := run("computers", "list", "--page-size", "2", "-o", "json")
	if code != 0 {
		t.Fatal(errs)
	}
	if n := len(mustJSON(t, out).([]any)); n != 2 {
		t.Fatalf("want 2 items, got %d", n)
	}
	if !strings.Contains(errs, "--all") {
		t.Fatalf("expected paging hint, got %q", errs)
	}
}

func TestListAllPagesWithDefaultSort(t *testing.T) {
	e := setup(t)
	code, out, errs := run("computers", "list", "--all", "--page-size", "2", "-o", "json")
	if code != 0 {
		t.Fatal(errs)
	}
	items := mustJSON(t, out).([]any)
	if len(items) != 5 || items[4].(map[string]any)["host"] != "host-4" {
		t.Fatalf("unexpected items %v", items)
	}
	reqs := e.mock.apiRequests()
	if len(reqs) != 3 {
		t.Fatalf("want 3 page requests, got %d", len(reqs))
	}
	if reqs[0].Query.Get("Sorts[0].by") != "created" || reqs[0].Query.Get("Sorts[0].order") != "Asc" {
		t.Fatalf("default sort missing: %s", reqs[0].RawQuery)
	}
}

func TestEnumCaseInsensitive(t *testing.T) {
	setup(t)
	_, out, _ := run("computers", "list", "--os", "mac", "--all", "-o", "json")
	for _, c := range mustJSON(t, out).([]any) {
		if c.(map[string]any)["os"] != "Mac" {
			t.Fatalf("filter not applied: %v", c)
		}
	}
	code, _, errs := run("computers", "list", "--os", "linux")
	if code != 2 || !strings.Contains(errs, "Windows, Mac") {
		t.Fatalf("exit %d err %q", code, errs)
	}
}

func TestFieldCSVTableYAML(t *testing.T) {
	setup(t)
	_, out, _ := run("computers", "list", "--all", "--field", "host")
	if strings.Fields(out)[0] != "host-0" || len(strings.Fields(out)) != 5 {
		t.Fatalf("field output %q", out)
	}
	_, out, _ = run("computers", "list", "--all", "-o", "csv", "--columns", "id,host")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "id,host" || len(lines) != 6 {
		t.Fatalf("csv %q", out)
	}
	_, out, _ = run("computers", "list", "--all")
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "HOST") || !strings.HasPrefix(first, "ID") {
		t.Fatalf("table header %q", first)
	}
	_, out, _ = run("computers", "get", "00000000-0000-0000-0000-000000000001", "-o", "yaml")
	if !strings.Contains(out, "host: host-1") {
		t.Fatalf("yaml %q", out)
	}
}

func TestNotFoundExitCodeAndJSONError(t *testing.T) {
	setup(t)
	code, _, errs := run("computers", "get", "nope", "-o", "json")
	if code != 4 {
		t.Fatalf("want exit 4, got %d (%s)", code, errs)
	}
	e := mustJSON(t, errs).(map[string]any)
	if e["status"].(float64) != 404 || !strings.Contains(e["error"].(string), "Computer not found") {
		t.Fatalf("bad error %v", e)
	}
}

func TestCreateWithFlags(t *testing.T) {
	e := setup(t)
	code, out, errs := run("groups", "create", "--name", "Pilot", "--is-default=false")
	if code != 0 || !strings.Contains(out, "11111111") {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
	r := e.mock.apiRequests()[0]
	if string(r.Body) != `{"name":"Pilot","isDefault":false}` || r.CType != "application/json" {
		t.Fatalf("body %s ctype %s", r.Body, r.CType)
	}
}

func TestBodyMergeSetAndFile(t *testing.T) {
	e := setup(t)
	bf := filepath.Join(e.dir, "body.json")
	_ = os.WriteFile(bf, []byte(`{"allComputers": false}`), 0o600)
	code, _, errs := run("groups", "assign-computers", "g1", "--body", "@"+bf, "--computer-ids", "a,b", "--set", "filter.os=Mac")
	if code != 0 {
		t.Fatal(errs)
	}
	got := string(e.mock.apiRequests()[0].Body)
	want := `{"allComputers":false,"computerIds":["a","b"],"filter":{"os":"Mac"}}`
	if got != want {
		t.Fatalf("body\n got %s\nwant %s", got, want)
	}
}

func TestMultipartUpload(t *testing.T) {
	e := setup(t)
	pf := filepath.Join(e.dir, "policy.xml")
	_ = os.WriteFile(pf, []byte("<Policy/>"), 0o600)
	code, _, errs := run("policies", "create", "--name", "Base", "--policy-file", pf)
	if code != 0 {
		t.Fatal(errs)
	}
	r := e.mock.apiRequests()[0]
	for _, want := range []string{`name="Name"`, `filename="policy.xml"`, "<Policy/>", "Base"} {
		if !bytes.Contains(r.Body, []byte(want)) {
			t.Fatalf("multipart body lacks %q", want)
		}
	}
	if !strings.HasPrefix(r.CType, "multipart/form-data; boundary=") {
		t.Fatalf("ctype %s", r.CType)
	}
	code, _, errs = run("policies", "create", "--name", "Base")
	if code != 2 || !strings.Contains(errs, "--policy-file") {
		t.Fatalf("missing file should be a usage error: %d %s", code, errs)
	}
}

func TestDownloadToFile(t *testing.T) {
	e := setup(t)
	target := filepath.Join(e.dir, "p.xml")
	code, _, errs := run("policies", "content", "p1", "--out-file", target)
	if code != 0 {
		t.Fatal(errs)
	}
	if b, _ := os.ReadFile(target); string(b) != "<?xml version='1.0'?><Policy/>" {
		t.Fatalf("file content %q", b)
	}
}

func TestDestructiveRequiresYes(t *testing.T) {
	e := setup(t)
	code, _, errs := run("groups", "delete", "g1", "--no-input")
	if code != 2 || !strings.Contains(errs, "--yes") {
		t.Fatalf("exit %d err %q", code, errs)
	}
	if len(e.mock.apiRequests()) != 0 {
		t.Fatal("no request may be sent without confirmation")
	}
	if code, _, errs = run("groups", "delete", "g1", "--yes"); code != 0 {
		t.Fatal(errs)
	}
	if e.mock.apiRequests()[0].Method != "DELETE" {
		t.Fatal("expected DELETE")
	}
}

func TestDryRunSendsNothing(t *testing.T) {
	e := setup(t)
	code, out, _ := run("computers", "delete", "--computer-ids", "x", "-n")
	if code != 0 {
		t.Fatal(out)
	}
	plan := mustJSON(t, out).(map[string]any)
	if plan["method"] != "DELETE" || plan["destructive"] != true {
		t.Fatalf("plan %v", plan)
	}
	if len(e.mock.requests) != 0 {
		t.Fatal("dry run sent a request")
	}
}

func TestUsageErrors(t *testing.T) {
	setup(t)
	cases := [][]string{
		{"events", "search", "--start-date", "2026-01-01"}, // missing required flags
		{"computers", "get"},           // missing ID
		{"computers", "lst"},           // unknown verb
		{"nonsense"},                   // unknown command
		{"about", "show", "-o", "xml"}, // bad output format
	}
	for _, c := range cases {
		if code, _, errs := run(c...); code != 2 {
			t.Errorf("%v: want exit 2, got %d (%s)", c, code, errs)
		}
	}
}

func TestRawAPICommand(t *testing.T) {
	setup(t)
	code, out, errs := run("api", "GET", "/v3/Computers", "-Q", "Filter.OS=Windows", "--all", "-o", "json")
	if code != 0 || len(mustJSON(t, out).([]any)) != 3 {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
}

func TestGlobalFlagsAnywhere(t *testing.T) {
	setup(t)
	_, out, _ := run("-o", "json", "about", "show")
	if !strings.Contains(out, "25.4.0") {
		t.Fatalf("got %q", out)
	}
}

func TestBadSecretIsAuthError(t *testing.T) {
	e := setup(t)
	t.Setenv("EPM_CLIENT_SECRET", "wrong")
	t.Setenv("EPM_CLI_CACHE_DIR", filepath.Join(e.dir, "cache2"))
	code, _, errs := run("about", "show")
	if code != 3 || !strings.Contains(errs, "invalid_client") {
		t.Fatalf("exit %d err %q", code, errs)
	}
}

func TestProfiles(t *testing.T) {
	e := setup(t)
	t.Setenv("EPM_URL", "")
	t.Setenv("EPM_CLIENT_ID", "")
	cfg, _ := config.Load()
	cfg.SetProfile("lab", &config.StoredProfile{URL: e.srv.URL, ClientID: "cid"})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(config.Path()); st.Mode().Perm() != 0o600 {
		t.Fatalf("config should be 0600, is %v", st.Mode().Perm())
	}
	_, out, _ := run("config", "list", "-o", "json")
	if mustJSON(t, out).([]any)[0].(map[string]any)["name"] != "lab" {
		t.Fatalf("config list %q", out)
	}
	if code, _, errs := run("-p", "lab", "about", "show"); code != 0 {
		t.Fatal(errs)
	}
	if code, _, errs := run("-p", "missing", "about", "show"); code != 2 || !strings.Contains(errs, "not found") {
		t.Fatalf("exit %d err %q", code, errs)
	}
}

func TestConfigAddNonInteractive(t *testing.T) {
	e := setup(t)
	t.Setenv("EPM_URL", "")
	t.Setenv("EPM_CLIENT_ID", "")
	t.Setenv("EPM_CLIENT_SECRET", "")
	os.Unsetenv("EPM_CLIENT_SECRET")
	var out, errb bytes.Buffer
	a := &App{Out: &out, Err: &errb, In: strings.NewReader("s3cret\n")}
	code := a.Execute([]string{"config", "add", "lab", "--url", e.srv.URL, "--client-id", "cid", "--client-secret-stdin"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "Authenticated") {
		t.Fatalf("expected verification, got %q", errb.String())
	}
	if config.GetSecret("lab") != "s3cret" {
		t.Fatal("secret not stored")
	}
	if code, _, errs := run("about", "show"); code != 0 {
		t.Fatalf("default profile should work: %s", errs)
	}
	if code, _, _ := run("config", "add", "bad name!", "--url", "x", "--client-id", "y", "--skip-verify"); code != 2 {
		t.Fatal("invalid profile name accepted")
	}
}

// ------------------------------------------------------------------ MCP

func mcpCall(t *testing.T, s *MCPServer, method string, params any) map[string]any {
	t.Helper()
	p, _ := json.Marshal(params)
	resp := s.Handle(rpcMsg{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: method, Params: p})
	b, _ := json.Marshal(resp)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func toolText(r map[string]any) (string, bool) {
	res := r["result"].(map[string]any)
	return res["content"].([]any)[0].(map[string]any)["text"].(string), res["isError"].(bool)
}

func TestMCPHandshake(t *testing.T) {
	setup(t)
	s := &MCPServer{}
	r := mcpCall(t, s, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	if r["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "epm-cli" {
		t.Fatalf("initialize %v", r)
	}
	tools := mcpCall(t, s, "tools/list", nil)["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools %v", tools)
	}
	if mcpCall(t, s, "nope", nil)["error"] == nil {
		t.Fatal("unknown method must error")
	}
	if s.Handle(rpcMsg{JSONRPC: "2.0", Method: "notifications/initialized"}) != nil {
		t.Fatal("notifications get no response")
	}
}

func TestMCPReadOnly(t *testing.T) {
	e := setup(t)
	s := &MCPServer{}
	text, isErr := toolText(mcpCall(t, s, "tools/call", map[string]any{"name": "run_command",
		"arguments": map[string]any{"args": []string{"about", "show"}}}))
	if isErr || !strings.Contains(text, "25.4.0") {
		t.Fatalf("read failed: %s", text)
	}
	text, isErr = toolText(mcpCall(t, s, "tools/call", map[string]any{"name": "run_command",
		"arguments": map[string]any{"args": "groups delete g1"}}))
	if !isErr || !strings.Contains(text, "read-only") {
		t.Fatalf("write should be refused: %s", text)
	}
	// dry-run of a write is allowed
	_, isErr = toolText(mcpCall(t, s, "tools/call", map[string]any{"name": "run_command",
		"arguments": map[string]any{"args": []string{"groups", "create", "--name", "x", "--dry-run"}}}))
	if isErr {
		t.Fatal("dry-run should be allowed in read-only mode")
	}
	for _, r := range e.mock.apiRequests() {
		if r.Method != "GET" {
			t.Fatalf("read-only server sent %s", r.Method)
		}
	}
	_, isErr = toolText(mcpCall(t, s, "tools/call", map[string]any{"name": "run_command",
		"arguments": map[string]any{"args": []string{"auth", "token"}}}))
	if !isErr {
		t.Fatal("auth token must not be exposed over MCP")
	}
}

func TestMCPWritesNeedConfirm(t *testing.T) {
	setup(t)
	s := &MCPServer{AllowWrites: true}
	text, isErr := toolText(mcpCall(t, s, "tools/call", map[string]any{"name": "run_command",
		"arguments": map[string]any{"args": []string{"groups", "delete", "g1"}}}))
	if !isErr || !strings.Contains(text, "--yes") {
		t.Fatalf("destructive without confirm should fail: %s", text)
	}
	text, isErr = toolText(mcpCall(t, s, "tools/call", map[string]any{"name": "run_command",
		"arguments": map[string]any{"args": []string{"groups", "delete", "g1"}, "confirm": true}}))
	if isErr {
		t.Fatalf("confirmed delete failed: %s", text)
	}
}

func TestMCPHelpAndDescribe(t *testing.T) {
	setup(t)
	s := &MCPServer{}
	text, _ := toolText(mcpCall(t, s, "tools/call", map[string]any{"name": "run_command",
		"arguments": map[string]any{"args": []string{"computers", "list", "-h"}}}))
	if !strings.Contains(text, "--host") {
		t.Fatalf("help not captured: %s", text)
	}
	text, isErr := toolText(mcpCall(t, s, "tools/call", map[string]any{"name": "describe_command",
		"arguments": map[string]any{"command": "computers authorise"}}))
	if isErr || !strings.Contains(text, "/v3/Computers/Authorise") || !strings.Contains(text, "--group-id") {
		t.Fatalf("describe: %s", text)
	}
}

func TestMCPServeStdio(t *testing.T) {
	setup(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_commands","arguments":{"filter":"archive"}}}` + "\n")
	var out bytes.Buffer
	if err := (&MCPServer{}).Serve(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "bulk-archive") {
		t.Fatalf("stdio output %q", out.String())
	}
}

func TestSplitArgs(t *testing.T) {
	got, err := SplitArgs(`--profile "my prof" -o json --set 'a=b c'`)
	want := []string{"--profile", "my prof", "-o", "json", "--set", "a=b c"}
	if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}
