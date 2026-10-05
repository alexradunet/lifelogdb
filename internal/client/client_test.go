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
	"lifelog/internal/db"
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
					c = Remote(srv.URL, "cli")
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
						_, err = c.GetContext(ctx, "/")
					case "POST":
						_, err = c.DoContext(ctx, api.Action{Method: "POST", Href: "/"}, nil)
					default:
						_, err = c.DoFilesContext(ctx, api.Action{Method: "POST", Href: "/", Fields: []api.Field{{Name: "original", Type: "file"}}}, nil, map[string]string{"original": path})
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
			c = Remote(srv.URL, "cli")
		}
		_, err := c.GetContext(ctx, "/")
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
	path := filepath.Join(t.TempDir(), "synthetic.db")
	if err := db.Init(path); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	held, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- d.Write(context.Background(), func(*sql.Tx) error { close(held); <-release; return nil })
	}()
	wait(t, held)
	released := false
	defer func() {
		if released {
			return
		}
		close(release)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	entered := make(chan struct{})
	h := api.New(&core.Store{DB: d}, nil)
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
		c = Remote(srv.URL, "cli")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.DoContext(ctx, api.Action{Method: "POST", Href: "/days/{day}/capture", Fields: []api.Field{{Name: "day", In: "path"}, {Name: "text"}}}, map[string]string{"day": "2026-10-05", "text": "synthetic queued"})
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
	if err := d.R.QueryRow("SELECT count(*) FROM pages WHERE title = ?", "2026-10-05").Scan(&n); err != nil {
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
		_, err := c.DoFilesContext(context.Background(), api.Action{Method: "POST", Href: "/", Fields: []api.Field{{Name: "original", Type: "file"}}}, nil, map[string]string{"original": path})
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
