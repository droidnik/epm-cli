package cli

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/droidnik/epm-cli/internal/apperr"
	"github.com/droidnik/epm-cli/internal/client"
	"github.com/droidnik/epm-cli/internal/config"
	"github.com/droidnik/epm-cli/internal/ojson"
	"github.com/droidnik/epm-cli/internal/spec"
)

func addBuiltins(root *cobra.Command, a *App) {
	core := func(c *cobra.Command) *cobra.Command {
		c.GroupID = "core"
		root.AddCommand(c)
		return c
	}
	core(configCmd(a))
	core(authCmd(a))
	core(&cobra.Command{Use: "doctor", Short: "Diagnose config, credentials and API reachability", Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error { return a.doctor() }})
	core(&cobra.Command{Use: "overview", Short: "Summary of the EPM tenant", Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error { return a.overview() }})
	core(backupCmd(a))
	core(apiCmd(a))
	core(&cobra.Command{Use: "commands [FILTER]", Short: "List every API operation with its CLI command",
		Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return a.Render(commandRows(filter), nil, "")
		}})
	core(mcpCmd(a))
	core(&cobra.Command{Use: "agent-context", Short: "Operating guidance for AI agents (auth, exit codes, flags)",
		Args: usageArgs(nil), RunE: func(*cobra.Command, []string) error {
			fmt.Fprint(a.Out, agentContext)
			return nil
		}})
	core(&cobra.Command{Use: "version", Short: "Print version information", Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error {
			t := spec.Load()
			if a.machineOutput() {
				o := ojson.NewObject()
				o.Set("version", Version)
				o.Set("api", t.Title)
				o.Set("apiVersion", t.Version)
				o.Set("go", runtime.Version())
				o.Set("platform", runtime.GOOS+"/"+runtime.GOARCH)
				return a.Render(o, nil, "")
			}
			fmt.Fprintf(a.Out, "epm-cli %s (%s, %s %s/%s)\n", Version, t.Title, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		}})
}

func commandRows(filter string) []any {
	filter = strings.ToLower(filter)
	var rows []any
	for _, r := range spec.Load().Resources {
		for _, c := range r.Commands {
			o := ojson.NewObject()
			o.Set("command", r.Name+" "+c.Verb)
			o.Set("method", c.Method)
			o.Set("path", c.Path)
			o.Set("summary", c.Summary)
			o.Set("destructive", c.Destructive)
			if filter != "" {
				b, _ := ojson.Marshal(o)
				if !strings.Contains(strings.ToLower(string(b)), filter) {
					continue
				}
			}
			rows = append(rows, o)
		}
	}
	return rows
}

func jwtClaims(token string) *ojson.Object {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ojson.NewObject()
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ojson.NewObject()
	}
	v, err := ojson.Decode(b)
	if o := ojson.Obj(v); err == nil && o != nil {
		return o
	}
	return ojson.NewObject()
}

func unixTime(v any) string {
	n, ok := v.(json.Number)
	if !ok {
		return ""
	}
	i, err := n.Int64()
	if err != nil {
		return ""
	}
	return time.Unix(i, 0).Format(time.RFC3339)
}

// ================================================================= config

func configCmd(a *App) *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Manage tenant profiles and credentials", Args: cobra.ArbitraryArgs,
		RunE: parentRunE}

	var url, clientID, scope, tokenURL string
	var secretStdin, insecure, skipVerify, setDefault bool
	add := &cobra.Command{
		Use:   "add [NAME]",
		Short: "Add or update a profile (prompts for missing values)",
		Long: `Add or update a tenant profile. The client secret is stored in the macOS Keychain
(service "epm-cli"); on other systems in a 0600 file next to the config.

Create the API account in the EPM console under Configuration > Settings > API Settings.
The tenant can be given as a name ("acme"), the console URL, or the API URL.

Examples:
  epm-cli config add prod
  epm-cli config add prod --url acme --client-id 1234… --client-secret-stdin < secret.txt`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return a.configAdd(name, url, clientID, scope, tokenURL, secretStdin, insecure, skipVerify, setDefault)
		},
	}
	add.Flags().StringVar(&url, "url", "", "tenant name ('acme'), console URL or https://acme-services.pm.beyondtrustcloud.com")
	add.Flags().StringVar(&clientID, "client-id", "", "API account client ID")
	add.Flags().BoolVar(&secretStdin, "client-secret-stdin", false, "read the client secret from stdin")
	add.Flags().StringVar(&scope, "scope", "", "OAuth scope (default "+config.DefaultScope+")")
	add.Flags().StringVar(&tokenURL, "token-url", "", "override the token URL (default <url>/oauth/connect/token)")
	add.Flags().BoolVar(&insecure, "insecure", false, "skip TLS verification (testing only)")
	add.Flags().BoolVar(&skipVerify, "skip-verify", false, "don't test the credentials after saving")
	add.Flags().BoolVar(&setDefault, "set-default", false, "make this the default profile")

	list := &cobra.Command{Use: "list", Short: "List profiles", Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			rows := []any{}
			for _, n := range cfg.Names() {
				p := cfg.Profiles[n]
				o := ojson.NewObject()
				o.Set("name", n)
				o.Set("default", n == cfg.CurrentProfile)
				o.Set("url", p.URL)
				o.Set("clientId", p.ClientID)
				secret := "missing"
				if config.GetSecret(n) != "" {
					secret = "stored"
				}
				o.Set("secret", secret)
				rows = append(rows, o)
			}
			if len(rows) == 0 {
				a.Hint("No profiles configured. Run: epm-cli config add <name>")
				if !a.machineOutput() {
					return nil
				}
			}
			return a.Render(rows, nil, "")
		}}

	show := &cobra.Command{Use: "show [NAME]", Short: "Show a profile (without secret)", Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				a.Profile = args[0]
			}
			p, err := a.ResolveProfile()
			if err != nil {
				return err
			}
			cfg, _ := a.Config()
			o := ojson.NewObject()
			o.Set("name", p.Name)
			o.Set("default", p.Name == cfg.CurrentProfile)
			o.Set("url", p.URL)
			o.Set("apiBase", p.APIBase())
			o.Set("tokenUrl", p.EffectiveTokenURL())
			o.Set("clientId", p.ClientID)
			o.Set("scope", p.Scope)
			secret := "missing"
			if p.ClientSecret != "" {
				secret = "set"
			}
			o.Set("secret", secret)
			o.Set("verifyTls", p.VerifyTLS)
			src := "config"
			if p.FromEnv {
				src = "environment"
			}
			o.Set("source", src)
			return a.Render(o, nil, "")
		}}

	use := &cobra.Command{Use: "use NAME", Short: "Set the default profile", Args: usageArgs([]string{"name"}),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			if cfg.Profiles[args[0]] == nil {
				return apperr.Usagef("unknown profile '%s'", args[0])
			}
			cfg.CurrentProfile = args[0]
			if err := cfg.Save(); err != nil {
				return apperr.New(apperr.General, "saving config: %v", err)
			}
			a.Success("Default profile is now '" + args[0] + "'")
			return nil
		}}

	remove := &cobra.Command{Use: "remove NAME", Short: "Remove a profile and its stored secret",
		Args: usageArgs([]string{"name"}), RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			name := args[0]
			if cfg.Profiles[name] == nil {
				return apperr.Usagef("unknown profile '%s'", name)
			}
			if err := a.Confirm(fmt.Sprintf("About to remove profile '%s' and its stored secret", name)); err != nil {
				return err
			}
			if p, err := cfg.Resolve(name); err == nil {
				client.New(p, false, nil).ClearTokenCache()
			}
			cfg.RemoveProfile(name)
			config.DeleteSecret(name)
			if err := cfg.Save(); err != nil {
				return apperr.New(apperr.General, "saving config: %v", err)
			}
			a.Success("Removed profile '" + name + "'")
			return nil
		}}

	var fromStdin bool
	setSecret := &cobra.Command{Use: "set-secret [NAME]", Short: "Store or replace the client secret of a profile",
		Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			name := a.Profile
			if len(args) > 0 {
				name = args[0]
			}
			if name == "" {
				name = cfg.CurrentProfile
			}
			if cfg.Profiles[name] == nil {
				return apperr.Usagef("unknown profile '%s'", name)
			}
			var secret string
			if fromStdin {
				secret, _ = a.readLine()
			} else if secret, err = a.Prompt("Client secret", "", true); err != nil {
				return err
			}
			if secret = strings.TrimSpace(secret); secret == "" {
				return apperr.Usagef("empty secret")
			}
			where, err := config.StoreSecret(name, secret)
			if err != nil {
				return apperr.New(apperr.General, "storing secret: %v", err)
			}
			a.Profile = name
			a.resetClient()
			if c, err := a.Client(); err == nil {
				c.ClearTokenCache()
			}
			a.Success(fmt.Sprintf("Secret for '%s' stored in %s", name, where))
			return nil
		}}
	setSecret.Flags().BoolVar(&fromStdin, "stdin", false, "read the secret from stdin")

	path := &cobra.Command{Use: "path", Short: "Print the config file location", Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error {
			fmt.Fprintln(a.Out, config.Path())
			return nil
		}}
	c.AddCommand(add, list, show, use, remove, setSecret, path)
	return c
}

func (a *App) configAdd(name, rawURL, clientID, scope, tokenURL string, secretStdin, insecure, skipVerify, setDefault bool) error {
	cfg, err := a.Config()
	if err != nil {
		return err
	}
	if name == "" {
		if name, err = a.Prompt("Profile name", "default", false); err != nil {
			return err
		}
	}
	if !config.ValidProfileName(name) {
		return apperr.Usagef("invalid profile name %q (letters, digits, '.', '_', '-')", name)
	}
	existing := cfg.Profiles[name]
	if existing == nil {
		existing = &config.StoredProfile{}
	}
	if rawURL == "" {
		rawURL = existing.URL
	}
	if rawURL == "" {
		if rawURL, err = a.Prompt("Tenant (name like 'acme' or URL)", "", false); err != nil {
			return err
		}
	}
	u, err := config.NormalizeURL(rawURL)
	if err != nil {
		return err
	}
	if clientID == "" {
		clientID = existing.ClientID
	}
	if clientID == "" {
		if clientID, err = a.Prompt("Client ID", "", false); err != nil {
			return err
		}
	}
	if clientID == "" {
		return apperr.Usagef("client ID is required")
	}
	hasSecret := config.GetSecret(name) != ""
	secret := ""
	switch {
	case secretStdin:
		secret, _ = a.readLine()
		secret = strings.TrimSpace(secret)
	case hasSecret && !a.interactive():
		// keep the stored secret
	default:
		label := "Client secret"
		if hasSecret {
			label += " (empty keeps the stored one)"
		}
		if secret, err = a.Prompt(label, "", true); err != nil {
			return err
		}
	}
	if secret == "" && !hasSecret {
		return apperr.Usagef("a client secret is required (use --client-secret-stdin or interactive input)")
	}
	sp := *existing
	sp.URL, sp.ClientID = u, clientID
	if scope != "" {
		sp.Scope = scope
	}
	if tokenURL != "" {
		sp.TokenURL = tokenURL
	}
	if insecure {
		f := false
		sp.VerifyTLS = &f
	}
	cfg.SetProfile(name, &sp)
	if setDefault {
		cfg.CurrentProfile = name
	}
	store := ""
	if secret != "" {
		if store, err = config.StoreSecret(name, secret); err != nil {
			return apperr.New(apperr.General, "storing secret: %v", err)
		}
	}
	if err := cfg.Save(); err != nil {
		return apperr.New(apperr.General, "saving config: %v", err)
	}
	msg := fmt.Sprintf("Saved profile '%s' → %s", name, u)
	if store != "" {
		msg += " (secret in " + store + ")"
	}
	a.Success(msg)
	if store == "file" {
		a.Hint("Note: no keychain available; secret stored in " + config.SecretsFile() + " (mode 0600).")
	}
	if !skipVerify {
		a.Profile = name
		a.client = nil
		c, err := a.Client()
		if err != nil {
			return err
		}
		c.ClearTokenCache()
		if _, err := c.FetchToken(); err != nil {
			return err
		}
		about, err := c.GetJSON("/v3/About")
		if err != nil {
			return err
		}
		a.Success("Authenticated against " + c.Profile.URL + aboutVersion(about))
	}
	if cfg.CurrentProfile == name {
		a.Hint("'" + name + "' is the default profile.")
	}
	return nil
}

func aboutVersion(about any) string {
	if s, ok := about.(string); ok && s != "" {
		return " (EPM " + s + ")"
	}
	if o := ojson.Obj(about); o != nil {
		if v, ok := o.GetFold("version"); ok {
			return " (EPM " + fmt.Sprint(v) + ")"
		}
	}
	return ""
}

// =================================================================== auth

func authCmd(a *App) *cobra.Command {
	c := &cobra.Command{Use: "auth", Short: "Token utilities (status, token, logout)", Args: cobra.ArbitraryArgs,
		RunE: parentRunE}
	c.AddCommand(&cobra.Command{Use: "status", Short: "Request a new token and show its claims", Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error {
			cl, err := a.Client()
			if err != nil {
				return err
			}
			cl.ClearTokenCache()
			tok, err := cl.FetchToken()
			if err != nil {
				return err
			}
			claims := jwtClaims(tok.AccessToken)
			o := ojson.NewObject()
			o.Set("profile", cl.Profile.Name)
			o.Set("url", cl.Profile.URL)
			o.Set("tokenType", tok.TokenType)
			o.Set("expiresIn", tok.ExpiresIn)
			o.Set("expiresAt", unixTime(claims.Values["exp"]))
			scope := tok.Scope
			if scope == "" {
				scope = strCell(claims.Values["scope"])
			}
			o.Set("scope", scope)
			o.Set("clientId", claims.Values["client_id"])
			o.Set("issuer", claims.Values["iss"])
			return a.Render(o, nil, "")
		}})
	c.AddCommand(&cobra.Command{Use: "token", Short: "Print a valid access token (for curl & co.)", Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error {
			cl, err := a.Client()
			if err != nil {
				return err
			}
			tok, err := cl.Token()
			if err != nil {
				return err
			}
			if a.OutTTY {
				a.Hint("Treat this token like a password; it is valid for about one hour.")
			}
			fmt.Fprintln(a.Out, tok)
			return nil
		}})
	c.AddCommand(&cobra.Command{Use: "logout", Short: "Delete the cached token", Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error {
			cl, err := a.Client()
			if err != nil {
				return err
			}
			cl.ClearTokenCache()
			a.Success("Cached token removed")
			return nil
		}})
	return c
}

func strCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = fmt.Sprint(x)
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(t)
	}
}

// ================================================================= doctor

func (a *App) doctor() error {
	var checks []any
	failed := false
	add := func(name, status, detail string) {
		o := ojson.NewObject()
		o.Set("check", name)
		o.Set("status", status)
		o.Set("detail", detail)
		checks = append(checks, o)
		if status == "fail" {
			failed = true
		}
	}
	cfgPath := config.Path()
	if _, err := os.Stat(cfgPath); err == nil {
		add("config file", "ok", cfgPath)
	} else {
		add("config file", "warn", cfgPath+" (not created yet)")
	}
	p, err := a.ResolveProfile()
	if err != nil {
		add("profile", "fail", err.Error())
	} else {
		add("profile", "ok", p.Describe())
		if p.ClientSecret != "" {
			if config.KeychainAvailable() && !p.FromEnv {
				add("client secret", "ok", "present (keychain)")
			} else {
				add("client secret", "ok", "present")
			}
		} else {
			add("client secret", "fail", "missing — run: epm-cli config set-secret "+p.Name)
		}
		u, _ := url.Parse(p.URL)
		host, port := u.Hostname(), u.Port()
		if port == "" {
			port = map[bool]string{true: "443", false: "80"}[u.Scheme == "https"]
		}
		netOK := false
		if addrs, err := net.LookupHost(host); err != nil {
			add("dns", "fail", host+": "+err.Error())
		} else {
			if len(addrs) > 3 {
				addrs = addrs[:3]
			}
			add("dns", "ok", host+" → "+strings.Join(addrs, ", "))
			start := time.Now()
			if u.Scheme == "https" {
				conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", net.JoinHostPort(host, port),
					&tls.Config{ServerName: host, InsecureSkipVerify: !p.VerifyTLS}) //nolint:gosec // user opt-out
				if err != nil {
					add("tls", "fail", err.Error())
				} else {
					st := conn.ConnectionState()
					conn.Close()
					add("tls", "ok", fmt.Sprintf("%s in %dms", tls.VersionName(st.Version), time.Since(start).Milliseconds()))
					netOK = true
				}
			} else {
				conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
				if err != nil {
					add("tcp", "fail", err.Error())
				} else {
					conn.Close()
					add("tcp", "warn", "plain HTTP (no TLS)")
					netOK = true
				}
			}
		}
		if netOK && p.ClientSecret != "" {
			c, _ := a.Client()
			c.ClearTokenCache()
			if tok, err := c.FetchToken(); err != nil {
				add("token", "fail", err.Error())
			} else {
				exp := unixTime(jwtClaims(tok.AccessToken).Values["exp"])
				if exp == "" {
					exp = fmt.Sprintf("in %ds", tok.ExpiresIn)
				}
				add("token", "ok", "valid, expires "+exp)
				if about, err := c.GetJSON("/v3/About"); err != nil {
					add("api", "fail", err.Error())
				} else {
					b, _ := ojson.Marshal(about)
					add("api", "ok", "GET /v3/About → "+short(string(b), 120))
				}
			}
		}
	}
	a.Wide = true // the detail column is the point of doctor; never truncate it
	if err := a.Render(checks, nil, ""); err != nil {
		return err
	}
	if failed {
		return &apperr.Error{Msg: "doctor found problems", Code: apperr.General}
	}
	return nil
}

// =============================================================== overview

func (a *App) overview() error {
	c, err := a.Client()
	if err != nil {
		return err
	}
	about, err := c.GetJSON("/v3/About")
	if err != nil {
		return err
	}
	info := ojson.NewObject()
	info.Set("profile", c.Profile.Name)
	info.Set("url", c.Profile.URL)
	if o := ojson.Obj(about); o != nil {
		for _, k := range o.Keys {
			info.Set("about."+k, o.Values[k])
		}
	} else {
		info.Set("version", about)
	}
	for _, x := range []struct{ label, path string }{{"computers", "/v3/Computers"}, {"groups", "/v3/Groups"},
		{"policies", "/v3/Policies"}, {"users", "/v3/Users"}} {
		d, err := c.GetJSON(x.path, client.KV{Key: "Pagination.PageNumber", Value: "1"},
			client.KV{Key: "Pagination.PageSize", Value: "1"})
		if err != nil {
			info.Set(x.label, nil)
			continue
		}
		if o := ojson.Obj(d); o != nil {
			info.Set(x.label, o.Values["totalRecordCount"])
		}
	}
	if groups, err := c.Paginate("/v3/Groups", nil, 100, 0, nil); err == nil {
		noPolicy, awaiting, def := 0, 0, ""
		for _, g := range groups {
			o := ojson.Obj(g)
			if o == nil {
				continue
			}
			if strCell(o.Values["policyId"]) == "" {
				noPolicy++
			}
			if o.Values["policyRevisionStatus"] == "AwaitingPolicyUpdate" {
				awaiting++
			}
			if b, _ := o.Values["default"].(bool); b {
				def = strCell(o.Values["name"])
			}
		}
		info.Set("groupsWithoutPolicy", noPolicy)
		info.Set("groupsAwaitingPolicyUpdate", awaiting)
		info.Set("defaultGroup", def)
	}
	if pols, err := c.Paginate("/v3/Policies", nil, 100, 0, nil); err == nil {
		drafts, unassigned := 0, 0
		for _, p := range pols {
			o := ojson.Obj(p)
			if o == nil {
				continue
			}
			if b, _ := o.Values["hasOpenDraft"].(bool); b {
				drafts++
			}
			if b, _ := o.Values["isAssignedToGroup"].(bool); !b {
				unassigned++
			}
		}
		info.Set("policiesWithOpenDraft", drafts)
		info.Set("policiesUnassigned", unassigned)
	}
	return a.Render(info, nil, "")
}

// ================================================================= backup

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeName(s string) string {
	s = strings.Trim(unsafeChars.ReplaceAllString(s, "_"), "_")
	if len(s) > 80 {
		s = s[:80]
	}
	if s == "" {
		return "unnamed"
	}
	return s
}

func backupCmd(a *App) *cobra.Command {
	var dir string
	var withComputers, noContent bool
	c := &cobra.Command{
		Use:   "backup",
		Short: "Export tenant configuration (policies incl. XML, groups, users, …)",
		Long: `Export the tenant configuration to a directory: about, accepted domains, API accounts,
authentication provider, roles, global roles, users, groups, and every policy with its
metadata, revisions, assigned groups and the latest policy XML. A manifest.json lists
all files and any errors. Put the directory under git to track changes over time.`,
		Args: usageArgs(nil),
		RunE: func(*cobra.Command, []string) error { return a.backup(dir, withComputers, noContent) },
	}
	c.Flags().StringVar(&dir, "dir", "", "target directory (default ./epm-backup-<profile>-<timestamp>)")
	c.Flags().BoolVar(&withComputers, "include-computers", false, "also export the computer inventory")
	c.Flags().BoolVar(&noContent, "no-policy-content", false, "skip downloading policy XML")
	return c
}

func (a *App) backup(dir string, withComputers, noContent bool) error {
	c, err := a.Client()
	if err != nil {
		return err
	}
	if dir == "" {
		dir = fmt.Sprintf("epm-backup-%s-%s", safeName(c.Profile.Name), time.Now().Format("20060102-150405"))
	}
	root := expandHome(dir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return apperr.New(apperr.General, "cannot create %s: %v", root, err)
	}
	files := ojson.NewObject()
	var errs []any

	save := func(rel string, data any) error {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return apperr.New(apperr.General, "%v", err)
		}
		var b []byte
		count := 1
		switch t := data.(type) {
		case []byte:
			b, count = t, len(t)
		case string:
			b, count = []byte(t), len(t)
		default:
			if l, ok := data.([]any); ok {
				count = len(l)
			}
			jb, err := ojson.MarshalIndent(data)
			if err != nil {
				return apperr.New(apperr.General, "%v", err)
			}
			b = append(jb, '\n')
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return apperr.New(apperr.General, "%v", err)
		}
		files.Set(rel, count)
		return nil
	}
	step := func(label string, fn func() error) bool {
		a.Hint("• " + label)
		if err := fn(); err != nil {
			e := ojson.NewObject()
			e.Set("step", label)
			e.Set("error", err.Error())
			errs = append(errs, e)
			a.Log("  ! " + label + ": " + err.Error())
			return false
		}
		return true
	}
	getSave := func(rel, path string) func() error {
		return func() error {
			d, err := c.GetJSON(path)
			if err != nil {
				return err
			}
			return save(rel, d)
		}
	}
	pageSave := func(rel, path string, q []client.KV) func() error {
		return func() error {
			d, err := c.Paginate(path, q, 100, 0, nil)
			if err != nil {
				return err
			}
			return save(rel, d)
		}
	}

	step("about.json", getSave("about.json", "/v3/About"))
	step("accepted-domains.json", getSave("accepted-domains.json", "/v3/AcceptedDomains"))
	step("api-accounts.json", getSave("api-accounts.json", "/v3/ApiAccounts"))
	step("authentication-provider.json", getSave("authentication-provider.json", "/v3/AuthenticationProvider"))
	step("roles.json", getSave("roles.json", "/v3/Roles"))
	step("global-roles.json", pageSave("global-roles.json", "/v3/GlobalRoles", nil))
	step("users.json", pageSave("users.json", "/v3/Users", nil))
	step("groups.json", pageSave("groups.json", "/v3/Groups", nil))
	if withComputers {
		step("computers.json", pageSave("computers.json", "/v3/Computers",
			[]client.KV{{Key: "Sorts[0].by", Value: "created"}, {Key: "Sorts[0].order", Value: "Asc"}}))
	}
	var policies []any
	step("policies.json", func() error {
		var err error
		if policies, err = c.Paginate("/v3/Policies", nil, 100, 0, nil); err != nil {
			return err
		}
		return save("policies.json", policies)
	})
	for _, pv := range policies {
		pol := ojson.Obj(pv)
		if pol == nil {
			continue
		}
		id, name := strCell(pol.Values["id"]), strCell(pol.Values["name"])
		short := id
		if len(short) > 8 {
			short = short[:8]
		}
		folder := "policies/" + safeName(name) + "-" + short
		step("policy "+name, func() error {
			for _, x := range []struct{ file, path string }{
				{"policy.json", "/v3/Policies/" + id},
				{"revisions.json", "/v3/Policies/" + id + "/Revisions"},
				{"assigned-groups.json", "/v3/Policies/" + id + "/AssignedGroups"},
			} {
				if err := getSave(folder+"/"+x.file, x.path)(); err != nil {
					return err
				}
			}
			if noContent {
				return nil
			}
			resp, err := c.Do(client.Request{Method: "GET", Path: "/v3/Policies/" + id + "/Content"})
			if err != nil {
				return err
			}
			return save(folder+"/latest.xml", resp.Body)
		})
	}

	manifest := ojson.NewObject()
	manifest.Set("tool", "epm-cli "+Version)
	manifest.Set("profile", c.Profile.Name)
	manifest.Set("url", c.Profile.URL)
	manifest.Set("created", time.Now().UTC().Format(time.RFC3339))
	manifest.Set("files", files)
	if errs == nil {
		errs = []any{}
	}
	manifest.Set("errors", errs)
	if err := save("manifest.json", manifest); err != nil {
		return err
	}
	msg := fmt.Sprintf("Backup written to %s (%d files", root, files.Len())
	if a.machineOutput() {
		manifest.Set("dir", root)
		_ = a.Render(manifest, nil, "")
	}
	if len(errs) > 0 {
		a.Log(fmt.Sprintf("%s, %d errors)", msg, len(errs)))
		return &apperr.Error{Msg: "backup finished with errors", Code: apperr.General}
	}
	a.Success(msg + ")")
	return nil
}

// ==================================================================== api

func apiCmd(a *App) *cobra.Command {
	var query, headers []string
	var body string
	var all bool
	c := &cobra.Command{
		Use:   "api METHOD PATH",
		Short: "Raw API request (escape hatch for any endpoint)",
		Long: `Send a raw request to the Management API. PATH is relative to /management-api.

Examples:
  epm-cli api GET /v3/About
  epm-cli api GET /v3/Computers --all -Q Filter.OS=Mac
  epm-cli api POST /v3/Groups -d '{"name":"Pilot"}'`,
		Args: usageArgs([]string{"method", "path"}),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			method := strings.ToUpper(args[0])
			switch method {
			case "GET", "POST", "PUT", "PATCH", "DELETE":
			default:
				return apperr.Usagef("invalid method %q", args[0])
			}
			req := client.Request{Method: method, Path: args[1], Header: map[string]string{}}
			for _, q := range query {
				k, v, err := parseKV(q, "--query")
				if err != nil {
					return err
				}
				req.Query = append(req.Query, client.KV{Key: k, Value: v})
			}
			for _, h := range headers {
				k, v, ok := strings.Cut(h, ":")
				if !ok {
					return apperr.Usagef("--header expects NAME:VALUE, got %q", h)
				}
				req.Header[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
			if body != "" {
				v, err := ReadBodyArg(body, a.In)
				if err != nil {
					return err
				}
				req.JSON = v
			}
			if a.DryRun {
				return a.printPlan(&req, method == "DELETE")
			}
			cl, err := a.Client()
			if err != nil {
				return err
			}
			if method == "DELETE" {
				if err := a.Confirm("About to send DELETE " + args[1]); err != nil {
					return err
				}
			}
			if all {
				if method != "GET" {
					return apperr.Usagef("--all only works with GET")
				}
				items, err := cl.Paginate(req.Path, req.Query, 100, 0, nil)
				if err != nil {
					return err
				}
				return a.Render(items, nil, "")
			}
			resp, err := cl.Do(req)
			if err != nil {
				return err
			}
			data := resp.Data()
			if data == nil {
				a.Success(fmt.Sprintf("HTTP %d", resp.Status))
				return nil
			}
			return a.Render(data, resp.Body, "")
		},
	}
	c.Flags().StringArrayVarP(&query, "query", "Q", nil, "query parameter KEY=VALUE (repeatable)")
	c.Flags().StringVarP(&body, "body", "d", "", "JSON body: inline, @file or - (stdin)")
	c.Flags().StringArrayVarP(&headers, "header", "H", nil, "extra header NAME:VALUE (repeatable)")
	c.Flags().BoolVarP(&all, "all", "A", false, "follow pagination (GET)")
	return c
}

// ========================================================== agent context

const agentContext = `epm-cli — operating guidance for AI agents
==========================================

Product: BeyondTrust Endpoint Privilege Management for Windows and Mac (PM Cloud),
Management API v3. Base: https://<tenant>-services.pm.beyondtrustcloud.com/management-api

Authentication
- OAuth2 client credentials. Profiles live in ~/.config/epm-cli/config.json, secrets in
  the macOS Keychain (service "epm-cli"). Tokens are cached (~1h) in ~/.cache/epm-cli.
- CI / non-interactive: set EPM_URL, EPM_CLIENT_ID, EPM_CLIENT_SECRET (no config needed).
- Check the setup with: epm-cli doctor -o json

Command shape
- epm-cli <resource> <verb> [ID…] [flags]      e.g. epm-cli computers list --os Mac --all -o json
- epm-cli commands [filter] -o json              every operation (method, path, destructive)
- epm-cli <resource> <verb> --help               flags for one operation
- epm-cli <resource> <verb> --body-template      example JSON body for write operations
- epm-cli api GET /v3/... -Q key=value           raw escape hatch
- Boolean filter flags: --flag sets true, --flag=false sets false.
- Array flags take comma-separated values or repeat: --computer-ids a,b or --computer-ids a --computer-ids b

Output
- Always use -o json (or ndjson) for parsing. Paged responses are unwrapped to the item array.
- A single page prints a hint on stderr; add --all to fetch every page (--max-pages N to cap).
- --field PATH extracts one value per item; --out-file writes to disk; downloads (policy XML,
  logs) are written as raw bytes.
- Hints and progress go to stderr; -q silences them.

Safety
- Destructive operations (delete, reject, archive, deactivate, clear-policy, disable,
  discard-draft, unassign-computers) prompt for confirmation. Without a TTY they FAIL unless
  --yes is given. Never pass --yes without explicit user approval for that exact action.
- Use -n/--dry-run first to show the exact request (method, URL, body) without sending it.
- Rate limit: 1000 requests per 100 s per tenant; the client retries HTTP 429 with backoff.

Exit codes
  0 ok | 1 general | 2 usage/config | 3 auth (401/403) | 4 not found | 5 other 4xx | 6 5xx/network | 130 cancelled
With -o json and a non-TTY stderr, errors are JSON: {"error":…, "exit_code":…, "status":…, "details":…}

MCP
- epm-cli mcp                 read-only MCP server (tools: list_commands, describe_command, run_command)
- epm-cli mcp --allow-writes  also allows writes; destructive ones additionally need confirm=true
`
