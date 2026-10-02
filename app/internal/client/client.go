// Package client is the hypermedia client the CLI and the MCP server share. It speaks HTTP to the API, either
// to a running `lifelog serve` or to the same handler in-process, so every surface goes through one code path.
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"lifelog/internal/api"
)

type Client struct {
	hc     *http.Client
	base   string
	source string
}

// InProcess calls h directly; no socket is opened.
func InProcess(h http.Handler, source string) *Client {
	return &Client{hc: &http.Client{Transport: handlerTransport{h}}, base: "http://lifelog.local", source: source}
}

// Remote calls a running server, e.g. http://127.0.0.1:7777.
func Remote(base, source string) *Client {
	return &Client{hc: http.DefaultClient, base: strings.TrimRight(base, "/"), source: source}
}

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	res := rec.Result()
	res.Request = r
	return res, nil
}

// Error is a non-2xx answer; Entity is the API's error entity.
type Error struct {
	Status int
	Entity *api.Entity
}

func (e *Error) Error() string {
	if p, ok := e.Entity.Properties.(map[string]any); ok {
		return fmt.Sprintf("%d: %v", e.Status, p["message"])
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// Get fetches a resource by its href (a path such as /days/2026-09-29).
func (c *Client) Get(href string) (*api.Entity, error) {
	req, err := http.NewRequest("GET", c.base+href, nil)
	if err != nil {
		return nil, err
	}
	return c.send(req)
}

// Do submits an action with field values. Fields of a templated href ({id}) are filled from values.
func (c *Client) Do(a api.Action, values map[string]string) (*api.Entity, error) {
	href := a.Href
	form := url.Values{}
	for _, f := range a.Fields {
		v, ok := values[f.Name]
		if !ok && f.Value != nil {
			v, ok = fmt.Sprint(f.Value), true
		}
		if f.In == "path" {
			if !ok {
				return nil, fmt.Errorf("%s needs %s", a.Name, f.Name)
			}
			href = strings.ReplaceAll(href, "{"+f.Name+"}", url.PathEscape(v))
			continue
		}
		if ok {
			form.Set(f.Name, v)
		}
	}
	for k := range values {
		if !hasField(a, k) {
			return nil, fmt.Errorf("%s has no field %q", a.Name, k)
		}
	}
	var req *http.Request
	var err error
	if a.Method == "GET" {
		req, err = http.NewRequest("GET", c.base+href+"?"+form.Encode(), nil)
	} else {
		req, err = http.NewRequest(a.Method, c.base+href, strings.NewReader(form.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, err
	}
	return c.send(req)
}

func hasField(a api.Action, name string) bool {
	for _, f := range a.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// Catalog is GET /actions: every action, with templated hrefs.
func (c *Client) Catalog() ([]api.Action, error) {
	e, err := c.Get("/actions")
	if err != nil {
		return nil, err
	}
	return e.Actions, nil
}

func (c *Client) send(req *http.Request) (*api.Entity, error) {
	req.Header.Set("Accept", "application/vnd.siren+json")
	if c.source != "" {
		req.Header.Set(api.SourceHeader, c.source)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var e api.Entity
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, fmt.Errorf("HTTP %d, not a Siren entity: %.200s", res.StatusCode, body)
	}
	if res.StatusCode >= 300 {
		return &e, &Error{res.StatusCode, &e}
	}
	return &e, nil
}
