package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/db/dbtest"
	"lifelog/internal/photo/phototest"
)

// TestFileCommand is `lifelog file PATH` on a throwaway database: the original is hashed and never stored, its text
// read from --text, its picture made from it; the same file again keeps one page.
func TestFileCommand(t *testing.T) {
	dir, live := t.TempDir(), dbtest.File(t)
	pic, text := filepath.Join(dir, "IMG_0001.jpg"), filepath.Join(dir, "caption.txt")
	os.WriteFile(pic, phototest.JPEG(64, 48), 0o644)
	os.WriteFile(text, []byte("Dawn at the lake with [[Sam]]."), 0o644)
	e, _, _ := testEnv(t)
	for range 2 {
		if err := runArgs(t.Context(), e, []string{"file", pic, "--db", live, "--text", text, "--day", "2026-10-04"}); err != nil {
			t.Fatal(err)
		}
	}
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	id, _ := s.PageID(context.Background(), "IMG_0001.jpg")
	p, err := s.PageByID(context.Background(), id)
	if err != nil || p.Type != "file" || p.Body != "Dawn at the lake with [[Sam]]." || p.Day != "2026-10-04" || p.File == nil || !p.File.Preview {
		t.Fatalf("the file page: %v %+v", err, p)
	}
	var n int
	d.R.QueryRow(`SELECT count(*) FROM files`).Scan(&n)
	if n != 1 {
		t.Errorf("%d files rows after keeping one file twice", n)
	}
}

// TestFileCommandFilesInACategory is `lifelog file PATH --category TITLE`: the original kept and its page filed in the
// category in one write, the category made when it is new (docs/plans/117-one-browse-view.md, rule 6).
func TestFileCommandFilesInACategory(t *testing.T) {
	dir, live := t.TempDir(), dbtest.File(t)
	memo := filepath.Join(dir, "memo.txt")
	if err := os.WriteFile(memo, []byte("a synthetic memo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, _, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"file", memo, "--db", live, "--day", "2026-10-04", "--category", "Papers"}); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	file, err := s.PageID(t.Context(), "memo.txt")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := s.PageID(t.Context(), "Papers")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.PageByID(t.Context(), cat); err != nil || p.Type != "page" {
		t.Fatalf("the category Papers: %v %+v, want a plain page", err, p)
	}
	var all, filed int
	if err := d.R.QueryRowContext(t.Context(), `SELECT count(*), count(*) FILTER (WHERE from_id = ? AND to_id = ?)
		FROM links WHERE kind = 'part-of'`, file, cat).Scan(&all, &filed); err != nil {
		t.Fatal(err)
	}
	if all != 1 || filed != 1 || file == 0 {
		t.Errorf("%d part-of links, %d from memo.txt (%d) to Papers (%d): want the one link", all, filed, file, cat)
	}
}

func TestCommandCancellation(t *testing.T) {
	for _, args := range [][]string{{"get", "blocked"}, {"capture", "synthetic"}, {"search", "synthetic"}, {"actions"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
					close(stopped)
				case <-release:
				}
			}))
			defer srv.Close()
			// This independent escape must run before server shutdown even on Fatal.
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			e, _, _ := testEnv(t)
			e.o = opts{args: args, url: srv.URL, source: "cli"}
			go func() { done <- runContext(ctx, e) }()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("not started")
			}
			cancel()
			select {
			case <-stopped:
			case <-time.After(10 * time.Second):
				t.Fatal("handler not canceled")
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("command did not return after cancellation")
			}
		})
	}
}

// TestServeAddressFlag: --addr, in both of its forms, reaches the parse of the loopback address (api.LoopbackAddress,
// whose own test has the table of addresses): LOCALHOST becomes 127.0.0.1, and a refused address stops serve before
// the database opens, as the file is absent.
func TestServeAddressFlag(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.db")
	e, _, _ := testEnv(t)
	for _, at := range []func(addr string) []string{
		func(addr string) []string { return []string{"--addr", addr} },
		func(addr string) []string { return []string{"--addr=" + addr} },
	} {
		o, err := parse(append([]string{"serve"}, at("LOCALHOST:7777")...), noVars)
		if err != nil {
			t.Fatal(err)
		}
		if err := prepareServe(&o); err != nil || o.addr != "127.0.0.1:7777" {
			t.Errorf("serve %v: the address %q, %v; want 127.0.0.1:7777", at("LOCALHOST:7777"), o.addr, err)
		}
		argv := append(at("0.0.0.0:7777"), "serve", "--db", absent)
		if err := runArgs(t.Context(), e, argv); err == nil || !strings.Contains(err.Error(), "serve address") {
			t.Errorf("%v: %v, want the refusal of the address", argv, err)
		}
	}
}

func TestServeAuthorityMount(t *testing.T) {
	// Run the real serve branch against a disposable synthetic database. Read
	// the printed ephemeral listener address rather than guessing a free port.
	p := dbtest.File(t)
	e, _, _ := testEnv(t)
	scheme, address := servedAt(t, serveInProcess(t, e, "serve", "--addr", "localhost:0", "--db", p))
	if strings.HasSuffix(address, ":0") {
		t.Fatalf("ephemeral port not reported: %s", address)
	}
	base := scheme + "://" + address
	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	for _, tc := range []struct {
		host string
		want int
	}{{"", 200}, {"arbitrary.invalid:7777", 403}} {
		req, _ := http.NewRequest("GET", base+"/actions", nil)
		if tc.host != "" {
			req.Host = tc.host
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("Host=%q status=%d", tc.host, res.StatusCode)
		}
	}
}
