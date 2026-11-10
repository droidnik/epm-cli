// Package client talks to the BeyondTrust EPM Management API.
package client

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/droidnik/epm-cli/internal/apperr"
	"github.com/droidnik/epm-cli/internal/config"
	"github.com/droidnik/epm-cli/internal/ojson"
)

const maxRetries = 4

// Version is set by the CLI for the User-Agent header.
var Version = "dev"

// KV is one query parameter (order and repetition matter for arrays and Sorts[i]).
type KV struct {
	Key   string
	Value string
}

type Response struct {
	Status      int
	Header      http.Header
	Body        []byte
	Method, URL string
}

func (r *Response) ContentType() string {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return strings.ToLower(mt)
}

func (r *Response) IsJSON() bool { return strings.HasSuffix(r.ContentType(), "json") }

// Data returns parsed JSON (ordered) for JSON bodies, a string for text, raw bytes
// otherwise, and nil for an empty body.
func (r *Response) Data() any {
	if len(r.Body) == 0 {
		return nil
	}
	ct := r.ContentType()
	if r.IsJSON() || ct == "text/plain" {
		if v, err := ojson.Decode(r.Body); err == nil {
			return v
		}
	}
	if strings.HasPrefix(ct, "text/") || r.IsJSON() {
		return string(r.Body)
	}
	return r.Body
}

// Multipart is a multipart/form-data body.
type Multipart struct {
	Fields []KV
	Files  []KV // field name -> local file path
}

func (m *Multipart) encode() ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range m.Fields {
		if err := w.WriteField(f.Key, f.Value); err != nil {
			return nil, "", err
		}
	}
	for _, f := range m.Files {
		data, err := os.ReadFile(f.Value)
		if err != nil {
			return nil, "", apperr.Usagef("cannot read %s: %v", f.Value, err)
		}
		ctype := mime.TypeByExtension(filepath.Ext(f.Value))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, f.Key, filepath.Base(f.Value)))
		h.Set("Content-Type", ctype)
		part, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		part.Write(data)
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// Request describes one API call.
type Request struct {
	Method    string
	Path      string
	Query     []KV
	JSON      any // marshalled as the JSON body if non-nil
	Multipart *Multipart
	Header    map[string]string
}

type Client struct {
	Profile  *config.Profile
	Verbose  bool
	NoCache  bool
	Log      func(string)
	http     *http.Client
	token    string
	tokenExp time.Time
	sleep    func(time.Duration)
}

func New(p *config.Profile, verbose bool, log func(string)) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if !p.VerifyTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit user opt-out
	}
	if log == nil {
		log = func(s string) { fmt.Fprintln(os.Stderr, s) }
	}
	return &Client{
		Profile: p, Verbose: verbose, Log: log, sleep: time.Sleep,
		http: &http.Client{Transport: tr, Timeout: time.Duration(p.TimeoutSec) * time.Second},
	}
}

// ---------------------------------------------------------------- auth

type tokenCache struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"`
}

func (c *Client) cacheFile() string {
	sum := sha256.Sum256([]byte(c.Profile.EffectiveTokenURL() + "|" + c.Profile.ClientID))
	return filepath.Join(config.CacheDir(), "tokens", hex.EncodeToString(sum[:12])+".json")
}

func (c *Client) loadCachedToken() bool {
	if c.NoCache {
		return false
	}
	b, err := os.ReadFile(c.cacheFile())
	if err != nil {
		return false
	}
	var tc tokenCache
	if json.Unmarshal(b, &tc) != nil || tc.AccessToken == "" {
		return false
	}
	exp := time.Unix(tc.ExpiresAt, 0)
	if time.Until(exp) < time.Minute {
		return false
	}
	c.token, c.tokenExp = tc.AccessToken, exp
	return true
}

func (c *Client) saveCachedToken() {
	if c.NoCache {
		return
	}
	p := c.cacheFile()
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	b, _ := json.Marshal(tokenCache{c.token, c.tokenExp.Unix()})
	_ = os.WriteFile(p, b, 0o600)
}

// ClearTokenCache forgets the in-memory and on-disk token.
func (c *Client) ClearTokenCache() {
	c.token, c.tokenExp = "", time.Time{}
	_ = os.Remove(c.cacheFile())
}

// TokenResponse is the OAuth token endpoint answer.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// FetchToken requests a new token with the client-credentials grant.
func (c *Client) FetchToken() (*TokenResponse, error) {
	if c.Profile.ClientSecret == "" {
		return nil, apperr.Authf("no client secret for profile '%s'. Run `epm-cli config set-secret %s` or set EPM_CLIENT_SECRET",
			c.Profile.Name, c.Profile.Name)
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {c.Profile.ClientID},
		"client_secret": {c.Profile.ClientSecret}}
	if c.Profile.Scope != "" {
		form.Set("scope", c.Profile.Scope)
	}
	tokenURL := c.Profile.EffectiveTokenURL()
	resp, err := c.send("POST", tokenURL, []byte(form.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, false)
	if err != nil {
		return nil, err
	}
	if resp.Status >= 400 {
		msg, details := problemMessage(resp)
		return nil, &apperr.Error{Msg: fmt.Sprintf("token request failed (%s): %s", tokenURL, msg),
			Code: apperr.Auth, Status: resp.Status, Details: details}
	}
	var tr TokenResponse
	if err := json.Unmarshal(resp.Body, &tr); err != nil {
		return nil, apperr.Authf("token endpoint returned a non-JSON response (%s)", resp.ContentType())
	}
	if tr.AccessToken == "" {
		return nil, apperr.Authf("token response has no access_token")
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 3600
	}
	c.token, c.tokenExp = tr.AccessToken, time.Now().Add(time.Duration(tr.ExpiresIn)*time.Second)
	c.saveCachedToken()
	return &tr, nil
}

// Token returns a valid access token (memory, disk cache, or a new one).
func (c *Client) Token() (string, error) {
	if c.token != "" && time.Until(c.tokenExp) > time.Minute {
		return c.token, nil
	}
	if c.loadCachedToken() {
		return c.token, nil
	}
	if _, err := c.FetchToken(); err != nil {
		return "", err
	}
	return c.token, nil
}

// ----------------------------------------------------------- transport

func (c *Client) send(method, rawURL string, body []byte, header map[string]string, auth bool) (*Response, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, rdr)
	if err != nil {
		return nil, apperr.Usagef("invalid request: %v", err)
	}
	req.Header.Set("User-Agent", "github.com/droidnik/epm-cli/"+Version)
	req.Header.Set("Accept", "application/json, */*;q=0.5")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if auth {
		tok, err := c.Token()
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if c.Verbose {
		c.Log("> " + method + " " + rawURL)
		if auth && len(body) > 0 && len(body) < 4000 && !bytes.Contains(body, []byte{0}) &&
			!strings.HasPrefix(header["Content-Type"], "multipart/") {
			c.Log("> " + string(body))
		}
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		host := req.URL.Host
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, apperr.Serverf("timeout talking to %s", host)
		}
		return nil, apperr.Serverf("cannot reach %s: %v", host, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, apperr.Serverf("reading response from %s: %v", req.URL.Host, err)
	}
	r := &Response{Status: resp.StatusCode, Header: resp.Header, Body: data, Method: method, URL: rawURL}
	if c.Verbose {
		ct := r.ContentType()
		if ct == "" {
			ct = "-"
		}
		c.Log(fmt.Sprintf("< %d %s %dB %dms", r.Status, ct, len(data), time.Since(start).Milliseconds()))
	}
	return r, nil
}

// BuildURL resolves an API path (with or without /management-api) and query.
func (c *Client) BuildURL(path string, query []KV) string {
	u := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		path = strings.TrimPrefix(path, "/management-api")
		u = c.Profile.APIBase() + path
	}
	if len(query) > 0 {
		var parts []string
		for _, kv := range query {
			parts = append(parts, url.QueryEscape(kv.Key)+"="+url.QueryEscape(kv.Value))
		}
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + strings.Join(parts, "&")
	}
	return u
}

// Do executes a request with re-auth on 401 and retries on 429/502/503/504.
func (c *Client) Do(r Request) (*Response, error) {
	method := strings.ToUpper(r.Method)
	u := c.BuildURL(r.Path, r.Query)
	header := map[string]string{}
	for k, v := range r.Header {
		header[k] = v
	}
	var body []byte
	switch {
	case r.Multipart != nil:
		b, ct, err := r.Multipart.encode()
		if err != nil {
			return nil, err
		}
		body, header["Content-Type"] = b, ct
	case r.JSON != nil:
		b, err := ojson.Marshal(r.JSON)
		if err != nil {
			return nil, apperr.Usagef("cannot encode body: %v", err)
		}
		body = b
		if header["Content-Type"] == "" {
			header["Content-Type"] = "application/json"
		}
	}
	idempotent := method == "GET" || method == "HEAD" || method == "PUT" || method == "DELETE"
	reauthed := false
	var resp *Response
	for attempt := 0; ; {
		var err error
		resp, err = c.send(method, u, body, header, true)
		if err != nil {
			return nil, err
		}
		if resp.Status == 401 && !reauthed {
			// Token revoked (secret regenerated) or expired early: fetch a new one once.
			reauthed = true
			c.ClearTokenCache()
			continue
		}
		retryable := resp.Status == 429 || (idempotent && (resp.Status == 502 || resp.Status == 503 || resp.Status == 504))
		if retryable && attempt < maxRetries {
			attempt++
			delay, ok := retryAfter(resp.Header.Get("Retry-After"))
			if !ok {
				delay = time.Duration(1<<attempt)*time.Second + time.Duration(rand.Intn(1000))*time.Millisecond
				if delay > 30*time.Second {
					delay = 30 * time.Second
				}
			}
			if c.Verbose || resp.Status == 429 {
				c.Log(fmt.Sprintf("! HTTP %d, retrying in %.1fs (%d/%d)", resp.Status, delay.Seconds(), attempt, maxRetries))
			}
			c.sleep(delay)
			continue
		}
		break
	}
	if resp.Status >= 400 {
		msg, details := problemMessage(resp)
		pu, _ := url.Parse(u)
		return resp, apperr.FromStatus(resp.Status, fmt.Sprintf("%s %s: %s", method, pu.Path, msg), details)
	}
	return resp, nil
}

// GetJSON performs a GET and returns the parsed body.
func (c *Client) GetJSON(path string, query ...KV) (any, error) {
	resp, err := c.Do(Request{Method: "GET", Path: path, Query: query})
	if err != nil {
		return nil, err
	}
	return resp.Data(), nil
}

// Progress is called after each page: page number, page count, total records.
type Progress func(page, pages, total int)

// Paginate fetches every page of a PagedResponse endpoint and returns all items.
func (c *Client) Paginate(path string, query []KV, pageSize, maxPages int, progress Progress) ([]any, error) {
	var base []KV
	for _, kv := range query {
		if kv.Key != "Pagination.PageNumber" && kv.Key != "Pagination.PageSize" {
			base = append(base, kv)
		}
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	items := []any{}
	for page := 1; ; page++ {
		q := append(append([]KV{}, base...), KV{"Pagination.PageNumber", strconv.Itoa(page)},
			KV{"Pagination.PageSize", strconv.Itoa(pageSize)})
		data, err := c.GetJSON(path, q...)
		if err != nil {
			return items, err
		}
		if arr, ok := data.([]any); ok {
			return append(items, arr...), nil
		}
		o := ojson.Obj(data)
		if o == nil {
			return items, nil
		}
		dv, ok := o.Get("data")
		if !ok {
			return items, nil
		}
		chunk, _ := dv.([]any)
		items = append(items, chunk...)
		pageCount, total := intOf(o, "pageCount"), intOf(o, "totalRecordCount")
		if progress != nil {
			progress(page, pageCount, total)
		}
		if len(chunk) == 0 || page >= pageCount || (maxPages > 0 && page >= maxPages) {
			return items, nil
		}
	}
}

func intOf(o *ojson.Object, key string) int {
	v, _ := o.Get(key)
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	}
	return 0
}

func retryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0, false
	}
	if f < 0 {
		f = 0
	}
	if f > 120 {
		f = 120
	}
	return time.Duration(f * float64(time.Second)), true
}

var reasons = map[int]string{400: "Bad Request", 401: "Unauthorized", 403: "Forbidden", 404: "Not Found",
	409: "Conflict", 423: "Locked", 429: "Too Many Requests", 500: "Internal Server Error"}

// problemMessage turns an RFC 7807 ProblemDetails (or anything else) into a message.
func problemMessage(r *Response) (string, any) {
	head := fmt.Sprintf("HTTP %d", r.Status)
	if reason := reasons[r.Status]; reason != "" {
		head += " " + reason
	}
	if len(r.Body) == 0 {
		return head, nil
	}
	v, err := ojson.Decode(r.Body)
	if err != nil {
		text := strings.TrimSpace(string(r.Body))
		if len(text) > 300 {
			text = text[:300]
		}
		return head + ": " + text, nil
	}
	o := ojson.Obj(v)
	if o == nil {
		return head, v
	}
	var parts []string
	seen := map[string]bool{}
	for _, k := range []string{"title", "detail", "message", "error_description", "error"} {
		if s, ok := o.Values[k].(string); ok && s != "" && !seen[s] {
			parts = append(parts, s)
			seen[s] = true
		}
	}
	if errs := ojson.Obj(o.Values["errors"]); errs != nil {
		for _, k := range errs.Keys {
			var msgs []string
			if arr, ok := errs.Values[k].([]any); ok {
				for _, m := range arr {
					msgs = append(msgs, fmt.Sprint(m))
				}
			} else {
				msgs = append(msgs, fmt.Sprint(errs.Values[k]))
			}
			parts = append(parts, k+": "+strings.Join(msgs, "; "))
		}
	}
	if len(parts) == 0 {
		b, _ := ojson.Marshal(v)
		s := string(b)
		if len(s) > 300 {
			s = s[:300]
		}
		parts = append(parts, s)
	}
	return head + ": " + strings.Join(parts, " — "), v
}
