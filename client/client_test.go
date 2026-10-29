package client

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
	"lifelog/siren"
)

func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("completion timed out")
	}
}

func TestClientCancellation(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, method := range []string{"GET", "POST", "multipart"} {
			t.Run(method+map[bool]string{false: "/local", true: "/remote"}[remote], func(t *testing.T) {
				started, stopped := make(chan struct{}), make(chan struct{})
				cleanup := make(chan struct{})
				defer func() {
					select {
					case <-cleanup:
					default:
						close(cleanup)
					}
				}()
				h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if method == "multipart" {
						b := make([]byte, 1)
						if _, err := r.Body.Read(b); err != nil {
							t.Error(err)
						}
					}
					close(started)
					if method == "multipart" {
						_, _ = io.Copy(io.Discard, r.Body)
					}
					select {
					case <-r.Context().Done():
					case <-cleanup:
					}
					close(stopped)
				})
				c := InProcess(h, "cli")
				if remote {
					srv := httptest.NewServer(h)
					defer srv.Close()
					defer func() {
						select {
						case <-cleanup:
						default:
							close(cleanup)
						}
					}()
					c = Remote(srv.URL, "cli", "")
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := make(chan error, 1)
				path := filepath.Join(t.TempDir(), "synthetic.txt")
				if err := os.WriteFile(path, make([]byte, 1<<20), 0600); err != nil {
					t.Fatal(err)
				}
				go func() {
					var err error
					switch method {
					case "GET":
						_, err = c.Get(ctx, "/")
					case "POST":
						_, err = c.Do(ctx, siren.Action{Method: "POST", Href: "/"}, nil)
					default:
						_, err = c.Do(ctx, siren.Action{Method: "POST", Href: "/", Fields: []siren.Field{{Name: "original", Type: "file"}}}, map[string]string{"original": path})
					}
					result <- err
				}()
				wait(t, started)
				cancel()
				wait(t, stopped)
				select {
				case err := <-result:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("got %v", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("request/producer hung")
				}
			})
		}
	}
}

func TestClientCancellationBeforeSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("canceled request sent") })
	for _, remote := range []bool{false, true} {
		c := InProcess(h, "cli")
		if remote {
			srv := httptest.NewServer(h)
			defer srv.Close()
			c = Remote(srv.URL, "cli", "")
		}
		_, err := c.Get(ctx, "/")
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}

func TestClientCancellationQueuedWrite(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "remote"}[remote], func(t *testing.T) { testQueuedWrite(t, remote) })
	}
}

func testQueuedWrite(t *testing.T, remote bool) {
	d := dbtest.New(t)
	held, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- d.Write(context.Background(), func(*sql.Tx) error { close(held); <-release; return nil })
	}()
	wait(t, held)
	released := false

	entered := make(chan struct{})
	h := api.New(&core.Store{DB: d})
	completed := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		defer close(completed)
		h.ServeHTTP(w, r)
	})
	c := InProcess(handler, "cli")
	if remote {
		srv := httptest.NewServer(handler)
		defer srv.Close()
		c = Remote(srv.URL, "cli", "")
	}
	defer func() {
		if released {
			return
		}
		close(release)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.Do(ctx, siren.Action{Method: "POST", Href: "/days/{day}/capture", Fields: []siren.Field{{Name: "day", In: "path"}, {Name: "text"}}}, map[string]string{"day": "2026-10-05", "text": "synthetic queued"})
		result <- err
	}()
	wait(t, entered)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("queued write hung")
	}
	wait(t, completed)
	close(release)
	released = true
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE name_key = ?", "2026-10-05").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("canceled write committed")
	}
}

func TestClientCancellationEarlyRefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.txt")
	if err := os.WriteFile(path, make([]byte, 1<<20), 0600); err != nil {
		t.Fatal(err)
	}
	c := InProcess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403); w.Write([]byte(`{}`)) }), "cli")
	done := make(chan error, 1)
	go func() {
		_, err := c.Do(context.Background(), siren.Action{Method: "POST", Href: "/", Fields: []siren.Field{{Name: "original", Type: "file"}}}, map[string]string{"original": path})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("refusal succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("producer hung after refusal")
	}
}

func TestErrorCodeIsTheCodeOfTheErrorEntity(t *testing.T) {
	withCode := &Error{Status: 409, Entity: &siren.Entity{Properties: map[string]any{"code": "exists", "message": "m"}}}
	if got := withCode.Code(); got != "exists" {
		t.Errorf("Code() = %q, want %q", got, "exists")
	}
	without := &Error{Status: 500, Entity: &siren.Entity{Properties: map[string]any{"message": "m"}}}
	if got := without.Code(); got != "" {
		t.Errorf("Code() = %q, want empty", got)
	}
}

func TestRemoteSendsTheWriterTheTokenAndAFileByItsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(path, []byte("synthetic"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(siren.SourceHeader) != "agent:t" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("headers: %v", r.Header)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
		} else if _, h, err := r.FormFile("original"); err != nil || h.Filename != "a.png" || r.URL.Path != "/files/2031-05-20" {
			t.Errorf("file %v, %v, path %s", h, err, r.URL.Path)
		}
		w.Write([]byte(`{"title":"a.png"}`))
	}))
	defer srv.Close()
	c := Remote(srv.URL+"/", "agent:t", "secret")
	act := siren.Action{Name: "add-file", Method: "POST", Href: "/files/{day}", Fields: []siren.Field{
		{Name: "day", Type: "date", In: "path"}, {Name: "original", Type: "file"}}}
	e, err := c.Do(t.Context(), act, map[string]string{"day": "2031-05-20", "original": path})
	if err != nil || e.Title != "a.png" {
		t.Fatalf("%v %v", e, err)
	}
	if _, err := c.Do(t.Context(), act, map[string]string{"nope": "x"}); err == nil {
		t.Error("a field that the action does not list was sent")
	}
}

func TestAnErrorEntityIsATypedErrorWithItsCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"properties":{"status":409,"code":"stale_version","message":"old"}}`))
	}))
	defer srv.Close()
	e, err := Remote(srv.URL, "", "").Get(t.Context(), "/x")
	var ce *Error
	if !errors.As(err, &ce) || ce.Status != 409 || ce.Code() != "stale_version" || e == nil {
		t.Fatalf("%v %v", e, err)
	}
}
