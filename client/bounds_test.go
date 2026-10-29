package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRemoteWaitForHeadersIsBounded: a server that takes the request and never answers does not hang the client. The
// transport gives up after its response-header timeout (short here, through remoteTransport's parameter), and the
// error says so. The production value is headerTimeout.
func TestRemoteWaitForHeadersIsBounded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })
	c := &Client{hc: &http.Client{Transport: remoteTransport(100 * time.Millisecond)}, base: srv.URL}
	start := time.Now()
	_, err := c.Get(t.Context(), "/")
	if err == nil || !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Fatalf("error = %v, want a response-header timeout", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the call took %v", d)
	}
	if tr := Remote("http://127.0.0.1:1", "cli", "").hc.Transport.(*http.Transport); tr.ResponseHeaderTimeout != headerTimeout || headerTimeout <= 0 {
		t.Errorf("Remote's ResponseHeaderTimeout = %v, want headerTimeout (%v)", tr.ResponseHeaderTimeout, headerTimeout)
	}
}

// TestRemoteStalledBodyEndsWithTheCallersContext: the server sends the headers and part of the body, then stops. The
// caller's context ends the read; the call does not wait for the server.
func TestRemoteStalledBodyEndsWithTheCallersContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte(`{"class":`))
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	c := Remote(srv.URL, "cli", "")
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Get(ctx, "/")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the context's deadline", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the call took %v", d)
	}
}

// TestAnAnswerOverTheLimitIsAnError: an answer of maxResponse bytes is read whole, and one byte more is an error that
// says the size and never shows the body. The read is not cut silently: a cut entity would decode as another answer.
func TestAnAnswerOverTheLimitIsAnError(t *testing.T) {
	const entity = `{"class":["home"],"title":"Home"}`
	pad := func(n int) string { return entity + strings.Repeat(" ", n-len(entity)) } // trailing space is valid JSON
	serve := func(body string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.siren+json")
			w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	c := Remote(serve(pad(maxResponse)).URL, "cli", "")
	e, err := c.Get(t.Context(), "/")
	if err != nil || e.Title != "Home" {
		t.Fatalf("an answer of exactly maxResponse bytes: %v, %v", e, err)
	}
	c = Remote(serve(pad(maxResponse+1)).URL, "cli", "")
	e, err = c.Get(t.Context(), "/")
	if err == nil || e != nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("an answer of maxResponse+1 bytes: %v, %v; want an error about the size", e, err)
	}
	if strings.Contains(err.Error(), "home") || strings.Contains(err.Error(), "Home") {
		t.Errorf("the error shows the body: %v", err)
	}
}
