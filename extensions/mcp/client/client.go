// Package client is the hypermedia client of the MCP server. It speaks HTTP to a running lifelog API. It is a copy of
// internal/client of lifelog without the in-process transport, because an extension never imports lifelog/internal. It
// is transport only: what the server makes of an answer (the MCP tools) is its own.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lifelog-mcp/siren"
)

type Client struct {
	hc     *http.Client
	base   string
	source string
	token  string // sent as a bearer credential to a public listener (docs/plans/078-network-server.md)
}

// Remote calls a running server, e.g. http://127.0.0.1:7777, with token as the owner's bearer credential when the
// server is --public ("" for none). Connecting and the TLS handshake are bounded by dialTimeout, and the wait for the
// answer's headers by headerTimeout. That timer starts after the request is sent, so an upload may take minutes. The
// answer's body is bounded in size (maxResponse). It is not bounded in time: the caller's context ends a body that
// stalls, and the command's context carries the interrupt.
func Remote(base, source, token string) *Client {
	return &Client{hc: &http.Client{Transport: remoteTransport(headerTimeout)}, base: strings.TrimRight(base, "/"), source: source, token: token}
}

const dialTimeout = 10 * time.Second

// headerTimeout is the longest wait for the headers of an answer once the request is sent. The server ends a request
// at its own limit of 2 minutes (lifelog internal/api, defaultLimits), and the slowest action is a snapshot: the client waits
// a little longer than that, so a slow answer is the server's 504 and not a timeout of this side.
const headerTimeout = 3 * time.Minute

// maxResponse is the largest answer body the client reads. An answer is one Siren entity, and a list is paged, so it
// is small; the limit stops a wrong or hostile server from filling memory.
const maxResponse = 16 << 20

func remoteTransport(headerWait time.Duration) *http.Transport {
	return &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: dialTimeout}).DialContext,
		TLSHandshakeTimeout: dialTimeout, ResponseHeaderTimeout: headerWait, ExpectContinueTimeout: time.Second, ForceAttemptHTTP2: true}
}

// Error is a non-2xx answer; Entity is the API's error entity.
type Error struct {
	Status int
	Entity *siren.Entity
}

// Code is the code of the error entity (property code; API.md, Errors), "" when the entity has none. A client branches
// on the code, never on the message.
func (e *Error) Code() string {
	if p, ok := e.Entity.Properties.(map[string]any); ok {
		code, _ := p["code"].(string)
		return code
	}
	return ""
}

func (e *Error) Error() string {
	if p, ok := e.Entity.Properties.(map[string]any); ok {
		return fmt.Sprintf("%d: %v", e.Status, p["message"])
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// Get fetches a resource by its href (a path such as /days/2026-09-29).
func (c *Client) Get(ctx context.Context, href string) (*siren.Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+href, nil)
	if err != nil {
		return nil, err
	}
	return c.send(req)
}

// fill is an action's href with its path fields filled, its other field values as a form, and its file fields (field
// name → path of a local file).
func fill(a siren.Action, values map[string]string) (string, url.Values, map[string]string, error) {
	href := a.Href
	form, files := url.Values{}, map[string]string{}
	for _, f := range a.Fields {
		v, ok := values[f.Name]
		if !ok && f.Value != nil {
			v, ok = fmt.Sprint(f.Value), true
		}
		switch {
		case f.Type == "file":
			if ok && v != "" {
				files[f.Name] = v
			}
		case f.In == "path":
			if !ok {
				return "", nil, nil, fmt.Errorf("%s needs %s", a.Name, f.Name)
			}
			href = strings.ReplaceAll(href, "{"+f.Name+"}", url.PathEscape(v))
		case ok:
			form.Set(f.Name, v)
		}
	}
	for k := range values {
		if a.Field(k) == nil {
			return "", nil, nil, fmt.Errorf("%s has no field %q", a.Name, k)
		}
	}
	return href, form, files, nil
}

// Do submits an action with field values. Fields of a templated href ({id}) are filled from values. The value of a
// file field is the path of a local file: the action is then sent as multipart/form-data, the file streamed from
// disk, never held whole in memory.
func (c *Client) Do(ctx context.Context, a siren.Action, values map[string]string) (*siren.Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	href, form, files, err := fill(a, values)
	if err != nil {
		return nil, err
	}
	if len(files) > 0 {
		return c.multipart(ctx, a.Method, href, form, files)
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

// multipart sends form and files (field name → path on disk) as multipart/form-data, each file streamed from disk.
func (c *Client) multipart(ctx context.Context, method, href string, form url.Values, files map[string]string) (*siren.Entity, error) {
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
	req, err := http.NewRequestWithContext(ctx, method, c.base+href, pr)
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

// Catalog is GET /actions: every action, with templated hrefs.
func (c *Client) Catalog(ctx context.Context) ([]siren.Action, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, err := c.Get(ctx, "/actions")
	if err != nil {
		return nil, err
	}
	return e.Actions, nil
}

func (c *Client) send(req *http.Request) (*siren.Entity, error) {
	req.Header.Set("Accept", "application/vnd.siren+json")
	if c.source != "" {
		req.Header.Set(siren.SourceHeader, c.source)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponse { // an error, never a cut body: half an entity could decode as another answer
		return nil, fmt.Errorf("HTTP %d, the answer is larger than %d bytes", res.StatusCode, maxResponse)
	}
	var e siren.Entity
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
		return nil, fmt.Errorf("HTTP %d, not a Siren entity (%s)", res.StatusCode, res.Header.Get("Content-Type")) // never the body: it may be a picture or a page
	}
	if res.StatusCode >= 300 {
		return &e, &Error{res.StatusCode, &e}
	}
	return &e, nil
}
