// Package config manages tenant profiles (~/.config/epm-cli/config.json) and credentials.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/droidnik/epm-cli/internal/apperr"
)

const (
	DefaultScope = "urn:management:api"
	cloudSuffix  = ".pm.beyondtrustcloud.com"
)

// Dir is the config directory (EPM_CLI_CONFIG_DIR, $XDG_CONFIG_HOME/epm-cli, ~/.config/epm-cli).
func Dir() string {
	if d := os.Getenv("EPM_CLI_CONFIG_DIR"); d != "" {
		return expand(d)
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "epm-cli")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "epm-cli")
}

// CacheDir holds cached tokens (EPM_CLI_CACHE_DIR, $XDG_CACHE_HOME/epm-cli, ~/.cache/epm-cli).
func CacheDir() string {
	if d := os.Getenv("EPM_CLI_CACHE_DIR"); d != "" {
		return expand(d)
	}
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "epm-cli")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "epm-cli")
}

func Path() string { return filepath.Join(Dir(), "config.json") }

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func writeFile0600(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

var (
	tenantNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)
	profileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// ValidProfileName reports whether name is usable as a profile name.
func ValidProfileName(name string) bool { return profileNameRe.MatchString(name) }

// NormalizeURL turns a tenant name, console URL or API URL into the API base URL.
//
//	"acme"                                    -> https://acme-services.pm.beyondtrustcloud.com
//	"acme.pm.beyondtrustcloud.com"            -> https://acme-services.pm.beyondtrustcloud.com
//	"https://acme-services.pm.beyondtrustcloud.com/management-api/swagger" -> https://acme-services.pm.beyondtrustcloud.com
//
// Any other host is kept as-is (scheme defaults to https).
func NormalizeURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", apperr.Usagef("tenant URL is empty")
	}
	if tenantNameRe.MatchString(value) {
		name := strings.TrimSuffix(value, "-services")
		return "https://" + name + "-services" + cloudSuffix, nil
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" {
		return "", apperr.Usagef("cannot parse tenant URL: %s", value)
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasSuffix(host, cloudSuffix) {
		sub := strings.TrimSuffix(host, cloudSuffix)
		if !strings.HasSuffix(sub, "-services") {
			host = sub + "-services" + cloudSuffix
		}
	}
	if p := u.Port(); p != "" {
		host += ":" + p
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + host, nil
}

// StoredProfile is what config.json holds for one profile (never the secret).
type StoredProfile struct {
	URL       string `json:"url"`
	ClientID  string `json:"client_id"`
	Scope     string `json:"scope,omitempty"`
	TokenURL  string `json:"token_url,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
	VerifyTLS *bool  `json:"verify_tls,omitempty"`
}

// Profile is a fully resolved profile, including the secret.
type Profile struct {
	Name         string
	URL          string
	ClientID     string
	ClientSecret string
	Scope        string
	TokenURL     string
	TimeoutSec   int
	VerifyTLS    bool
	FromEnv      bool
}

func (p *Profile) APIBase() string { return strings.TrimRight(p.URL, "/") + "/management-api" }

func (p *Profile) EffectiveTokenURL() string {
	if p.TokenURL != "" {
		return p.TokenURL
	}
	return strings.TrimRight(p.URL, "/") + "/oauth/connect/token"
}

type Config struct {
	CurrentProfile string                    `json:"current_profile,omitempty"`
	Profiles       map[string]*StoredProfile `json:"profiles"`
	path           string
}

// Load reads the config file; a missing file yields an empty config.
func Load() (*Config, error) {
	c := &Config{Profiles: map[string]*StoredProfile{}, path: Path()}
	b, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, apperr.Usagef("cannot read %s: %v", c.path, err)
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, apperr.Usagef("invalid config file %s: %v", c.path, err)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]*StoredProfile{}
	}
	return c, nil
}

func (c *Config) FilePath() string { return c.path }

func (c *Config) Exists() bool {
	_, err := os.Stat(c.path)
	return err == nil
}

func (c *Config) Save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(c.path, append(b, '\n'))
}

// Names returns profile names sorted.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (c *Config) SetProfile(name string, p *StoredProfile) {
	c.Profiles[name] = p
	if c.CurrentProfile == "" || c.Profiles[c.CurrentProfile] == nil {
		c.CurrentProfile = name
	}
}

func (c *Config) RemoveProfile(name string) {
	delete(c.Profiles, name)
	if c.CurrentProfile == name {
		c.CurrentProfile = ""
		if names := c.Names(); len(names) > 0 {
			c.CurrentProfile = names[0]
		}
	}
}

// Resolve returns the active profile. EPM_* environment variables override stored
// values; with EPM_URL + EPM_CLIENT_ID + EPM_CLIENT_SECRET no config file is needed (CI).
func (c *Config) Resolve(name string) (*Profile, error) {
	if name == "" {
		name = os.Getenv("EPM_PROFILE")
	}
	if name == "" {
		name = c.CurrentProfile
	}
	envURL, envID := os.Getenv("EPM_URL"), os.Getenv("EPM_CLIENT_ID")
	stored := c.Profiles[name]
	if name != "" && stored == nil && (envURL == "" || envID == "") {
		known := strings.Join(c.Names(), ", ")
		if known == "" {
			known = "none"
		}
		return nil, apperr.Usagef("profile '%s' not found (configured: %s). Run: epm-cli config add %s", name, known, name)
	}
	if stored == nil {
		stored = &StoredProfile{}
	}
	p := &Profile{
		Name: name, URL: first(envURL, stored.URL), ClientID: first(envID, stored.ClientID),
		Scope:    first(os.Getenv("EPM_SCOPE"), stored.Scope, DefaultScope),
		TokenURL: first(os.Getenv("EPM_TOKEN_URL"), stored.TokenURL), TimeoutSec: stored.Timeout,
		VerifyTLS: stored.VerifyTLS == nil || *stored.VerifyTLS,
		FromEnv:   envURL != "" && envID != "",
	}
	if p.URL == "" || p.ClientID == "" {
		return nil, apperr.Usagef("no EPM tenant configured. Run `epm-cli config add <name>` " +
			"or set EPM_URL, EPM_CLIENT_ID and EPM_CLIENT_SECRET.")
	}
	if p.Name == "" {
		p.Name = "env"
	}
	u, err := NormalizeURL(p.URL)
	if err != nil {
		return nil, err
	}
	p.URL = u
	if t, err := strconv.Atoi(os.Getenv("EPM_TIMEOUT")); err == nil && t > 0 {
		p.TimeoutSec = t
	}
	if p.TimeoutSec <= 0 {
		p.TimeoutSec = 60
	}
	if v := strings.ToLower(os.Getenv("EPM_VERIFY_TLS")); v == "0" || v == "false" || v == "no" {
		p.VerifyTLS = false
	}
	if s, ok := os.LookupEnv("EPM_CLIENT_SECRET"); ok {
		p.ClientSecret = s
	} else if c.Profiles[name] != nil {
		p.ClientSecret = GetSecret(name)
	}
	return p, nil
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Describe returns a short human description of where a profile came from.
func (p *Profile) Describe() string {
	src := "config"
	if p.FromEnv {
		src = "environment"
	}
	return fmt.Sprintf("%s → %s (%s)", p.Name, p.URL, src)
}
