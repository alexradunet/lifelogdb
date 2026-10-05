// Package client is the hypermedia client the CLI and the MCP server share. It speaks HTTP to the API, either
// to a running `lifelog serve` or to the same handler in-process, so every surface goes through one code path.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
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
	return c.GetContext(context.Background(), href)
}

func (c *Client) GetContext(ctx context.Context, href string) (*api.Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+href, nil)
	if err != nil {
		return nil, err
	}
	return c.send(req)
}

// fill is an action's href with its path fields filled, and its other field values (file fields are DoFiles').
func fill(a api.Action, values map[string]string) (string, url.Values, error) {
	href := a.Href
	form := url.Values{}
	for _, f := range a.Fields {
		v, ok := values[f.Name]
		if !ok && f.Value != nil {
			v, ok = fmt.Sprint(f.Value), true
		}
		switch {
		case f.Type == "file":
			if ok {
				return "", nil, fmt.Errorf("%s: %s is a file, sent with DoFiles", a.Name, f.Name)
			}
		case f.In == "path":
			if !ok {
				return "", nil, fmt.Errorf("%s needs %s", a.Name, f.Name)
			}
			href = strings.ReplaceAll(href, "{"+f.Name+"}", url.PathEscape(v))
		case ok:
			form.Set(f.Name, v)
		}
	}
	for k := range values {
		if !hasField(a, k) {
			return "", nil, fmt.Errorf("%s has no field %q", a.Name, k)
		}
	}
	return href, form, nil
}

// DoFiles submits an action as multipart/form-data: values as fields, and files (field name → path on disk)
// streamed from disk, never held whole in memory.
func (c *Client) DoFiles(a api.Action, values, files map[string]string) (*api.Entity, error) {
	return c.DoFilesContext(context.Background(), a, values, files)
}

func (c *Client) DoFilesContext(ctx context.Context, a api.Action, values, files map[string]string) (*api.Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	href, form, err := fill(a, values)
	if err != nil {
		return nil, err
	}
	for name := range files {
		if !hasField(a, name) {
			return nil, fmt.Errorf("%s has no field %q", a.Name, name)
		}
	}
	pr, pw := io.Pipe()
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { pr.CloseWithError(ctx.Err()); pw.CloseWithError(ctx.Err()) })
	defer func() { stop(); pr.Close(); pw.Close(); <-done }()
	mw := multipart.NewWriter(pw)
	go func() {
		defer close(done)
		pw.CloseWithError(func() error {
			for k, vs := range form {
				for _, v := range vs {
					if err := mw.WriteField(k, v); err != nil {
						return err
					}
				}
			}
			for name, path := range files {
				if err := ctx.Err(); err != nil {
					return err
				}
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				part, err := mw.CreateFormFile(name, filepath.Base(path))
				if err == nil {
					_, err = io.Copy(part, f)
				}
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
				if err != nil {
					return err
				}
			}
			return mw.Close()
		}())
	}()
	req, err := http.NewRequestWithContext(ctx, a.Method, c.base+href, pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	e, err := c.send(req)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return e, err
}

// Do submits an action with field values. Fields of a templated href ({id}) are filled from values.
func (c *Client) Do(a api.Action, values map[string]string) (*api.Entity, error) {
	return c.DoContext(context.Background(), a, values)
}

func (c *Client) DoContext(ctx context.Context, a api.Action, values map[string]string) (*api.Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	href, form, err := fill(a, values)
	if err != nil {
		return nil, err
	}
	var req *http.Request
	if a.Method == "GET" {
		req, err = http.NewRequestWithContext(ctx, "GET", c.base+href+"?"+form.Encode(), nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, a.Method, c.base+href, strings.NewReader(form.Encode()))
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
func (c *Client) Catalog() ([]api.Action, error) { return c.CatalogContext(context.Background()) }

func (c *Client) CatalogContext(ctx context.Context) ([]api.Action, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, err := c.GetContext(ctx, "/actions")
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
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	err = decoder.Decode(&e)
	if err == nil {
		var trailing any
		if next := decoder.Decode(&trailing); next != io.EOF {
			err = fmt.Errorf("trailing JSON: %v", next)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("HTTP %d, not a Siren entity: %.200s", res.StatusCode, body)
	}
	if res.StatusCode >= 300 {
		return &e, &Error{res.StatusCode, &e}
	}
	return &e, nil
}
