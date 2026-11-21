// Package oxibooru is a client of the REST API of Oxibooru, and of the
// Szurubooru servers it is compatible with.
package oxibooru

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Credentials authenticate requests: a token when Token is set, else the
// password with basic authentication, else none.
type Credentials struct {
	User     string
	Token    string
	Password string
}

func (cr Credentials) header() string {
	switch {
	case cr.User == "":
		return ""
	case cr.Token != "":
		return "Token " + base64.StdEncoding.EncodeToString([]byte(cr.User+":"+cr.Token))
	case cr.Password != "":
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(cr.User+":"+cr.Password))
	}
	return ""
}

// Client calls one server. Its methods are safe for concurrent use.
type Client struct {
	api  *url.URL // the API's root, ending in a slash
	site *url.URL // what the data URLs of resources are relative to
	http *http.Client

	mu    sync.RWMutex
	creds Credentials
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient makes the client send its requests with h.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithCredentials authenticates the client's requests.
func WithCredentials(cr Credentials) Option { return func(c *Client) { c.creds = cr } }

// New returns a client of the API at apiURL, whose resources' data URLs are
// relative to siteURL (the site's root, which serves /data).
func New(apiURL, siteURL string, opts ...Option) (*Client, error) {
	api, err := parseBase(apiURL)
	if err != nil {
		return nil, err
	}
	site, err := parseBase(siteURL)
	if err != nil {
		return nil, err
	}
	c := &Client{api: api, site: site, http: &http.Client{Timeout: 0}}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

func parseBase(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("no URL")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("the URL has no host")
	}
	u.RawQuery, u.Fragment = "", ""
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u, nil
}

// Endpoint is where a server's API and data are.
type Endpoint struct {
	API  string
	Site string
	Info *Info
}

// Discover finds the API of the server at raw, the address of its site
// (where the API is at /api, as the official images serve it) or of the API
// itself, and returns it with the server's information.
func Discover(ctx context.Context, raw string, opts ...Option) (*Endpoint, error) {
	base, err := parseBase(raw)
	if err != nil {
		return nil, err
	}
	candidates := []struct{ api, site string }{
		{base.JoinPath("api").String() + "/", base.String()},
		{base.String(), base.String()},
	}
	if p := strings.TrimSuffix(base.Path, "/"); strings.HasSuffix(p, "/api") {
		site := *base
		site.Path = strings.TrimSuffix(p, "api")
		candidates = []struct{ api, site string }{{base.String(), site.String()}}
	}
	var firstErr error
	for _, cand := range candidates {
		c, err := New(cand.api, cand.site, opts...)
		if err != nil {
			return nil, err
		}
		info, err := c.Info(ctx)
		if err == nil {
			return &Endpoint{API: c.api.String(), Site: c.site.String(), Info: info}, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, fmt.Errorf("no Oxibooru server at %s: %w", base, firstErr)
}

// APIURL returns the root of the API.
func (c *Client) APIURL() string { return c.api.String() }

// SiteURL returns the root of the site.
func (c *Client) SiteURL() string { return c.site.String() }

// Credentials returns what authenticates the client.
func (c *Client) Credentials() Credentials {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.creds
}

// SetCredentials changes what authenticates the client.
func (c *Client) SetCredentials(cr Credentials) {
	c.mu.Lock()
	c.creds = cr
	c.mu.Unlock()
}

// HTTPClient returns the client sending the requests, for data URLs.
func (c *Client) HTTPClient() *http.Client { return c.http }

// DataURL resolves a data URL of a resource, as a post's contentUrl, to an
// absolute URL.
func (c *Client) DataURL(ref string) string {
	if ref == "" {
		return ""
	}
	u, err := c.site.Parse(ref)
	if err != nil {
		return ""
	}
	return u.String()
}

// Error is an error the server reported.
type Error struct {
	Status      int    `json:"-"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (e *Error) Error() string {
	switch {
	case e.Description != "":
		return e.Description
	case e.Title != "":
		return e.Title
	}
	return fmt.Sprintf("the server answered %d %s", e.Status, http.StatusText(e.Status))
}

// ErrConflict is what the server reports for a resource changed since the
// version the client sent.
var ErrConflict = errors.New("the resource was changed by someone else")

// Is reports outdated versions as ErrConflict.
func (e *Error) Is(target error) bool {
	// Oxibooru names it ResourceModified, Szurubooru IntegrityError.
	return target == ErrConflict && e.Status == http.StatusConflict && (e.Name == "ResourceModified" || e.Name == "IntegrityError")
}

// StatusOf returns the HTTP status of a server's error, 0 for other errors.
func StatusOf(err error) int {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Status
	}
	return 0
}

func (c *Client) endpoint(path string, q url.Values) string {
	u := c.api.JoinPath(path)
	if strings.HasSuffix(path, "/") && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// do sends a request with a JSON body (none when nil) and decodes the
// answer into out (ignored when nil).
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, q), r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *Client) send(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	if h := c.Credentials().header(); h != "" {
		req.Header.Set("Authorization", h)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		apiErr := &Error{Status: res.StatusCode}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if json.Unmarshal(b, apiErr) != nil || (apiErr.Title == "" && apiErr.Description == "") {
			apiErr.Description = strings.TrimSpace(string(b))
			if len(apiErr.Description) > 200 || strings.HasPrefix(apiErr.Description, "<") {
				apiErr.Description = ""
			}
		}
		return apiErr
	}
	if out == nil {
		_, err = io.Copy(io.Discard, res.Body)
		return err
	}
	if err := json.UnmarshalRead(res.Body, out); err != nil {
		return fmt.Errorf("reading the answer to %s %s: %w", req.Method, req.URL.Path, err)
	}
	return nil
}

func get[T any](ctx context.Context, c *Client, path string, q url.Values) (T, error) {
	var v T
	err := c.do(ctx, http.MethodGet, path, q, nil, &v)
	return v, err
}

func send[T any](ctx context.Context, c *Client, method, path string, q url.Values, body any) (T, error) {
	var v T
	err := c.do(ctx, method, path, q, body, &v)
	return v, err
}

// fieldsQuery returns the query choosing fields of the resources, none for
// all.
func fieldsQuery(fields []string) url.Values {
	if len(fields) == 0 {
		return nil
	}
	return url.Values{"fields": {strings.Join(fields, ",")}}
}

// versioned is the body of a deletion.
type versioned struct {
	Version Version `json:"version"`
}
