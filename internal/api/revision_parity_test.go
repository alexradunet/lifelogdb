package api_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestRevisionTokenParity(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, source := range []string{"cli", "api", "ui", "agent:revision"} {
			t.Run(fmt.Sprintf("remote=%v/source=%s", remote, source), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "life.db")
				if err := db.Init(path); err != nil {
					t.Fatal(err)
				}
				d, err := db.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := d.Close(); err != nil {
						t.Error(err)
					}
				})
				store := &core.Store{DB: d}
				h := api.New(store)
				c := client.InProcess(h, source)
				if remote {
					server := httptest.NewServer(h)
					t.Cleanup(server.Close)
					c = client.Remote(server.URL, source)
				}
				id, _, err := store.CreatePage(context.Background(), "cli", "Version parity", "first")
				if err != nil {
					t.Fatal(err)
				}
				resource := fmt.Sprintf("/pages/%d", id)
				p, err := c.Get(resource)
				if err != nil {
					t.Fatal(err)
				}
				original, ok := p.Properties.(map[string]any)["version"].(string)
				if !ok {
					t.Fatalf("version is not an opaque string: %#v", p.Properties)
				}
				save := find(p, "save-body")
				var clock string
				if err := d.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, id).Scan(&clock); err != nil {
					t.Fatal(err)
				}
				if _, err := c.Do(save, map[string]string{"body": "committed [[Kept]]"}); err != nil {
					t.Fatal(err)
				}
				if _, err := d.W.Exec(`UPDATE entities SET updated_at=? WHERE id=?`, clock, id); err != nil {
					t.Fatal(err)
				}
				for _, token := range []string{original, clock} {
					_, err := c.Do(save, map[string]string{"body": "stale [[Forbidden]]", "version": token})
					var ce *client.Error
					if !errors.As(err, &ce) || ce.Status != 409 {
						t.Fatalf("token %q: %v; want conflict", token, err)
					}
				}
				state, err := store.PageByID(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				if state.Body != "committed [[Kept]]" || state.Version == original {
					t.Fatalf("stale token changed page: %+v", state)
				}
				if target, err := store.PageID(context.Background(), "Forbidden"); err != nil || target != 0 {
					t.Fatalf("stale target %d %v", target, err)
				}
				// Values beyond the float64 exact range must round-trip through properties,
				// prefilled action fields and each transport without numeric coercion.
				if _, err := d.W.Exec(`UPDATE entities SET revision=9223372036854775807 WHERE id=?`, id); err != nil {
					t.Fatal(err)
				}
				p, err = c.Get(resource)
				if err != nil {
					t.Fatal(err)
				}
				if got := p.Properties.(map[string]any)["version"]; got != "9223372036854775807" {
					t.Fatalf("token property %#v", got)
				}
				save = find(p, "save-body")
				var prefilled any
				for _, field := range save.Fields {
					if field.Name == "version" {
						prefilled = field.Value
					}
				}
				if prefilled != "9223372036854775807" {
					t.Fatalf("prefilled token %#v", prefilled)
				}
				if _, err := c.Do(save, map[string]string{"body": state.Body}); err != nil {
					t.Fatalf("max token no-op: %v", err)
				}
				_, err = c.Do(save, map[string]string{"body": "overflow [[Forbidden]]"})
				var ce *client.Error
				if !errors.As(err, &ce) || ce.Status != 422 {
					t.Fatalf("exhaustion error: %v; want refusal", err)
				}
				state, err = store.PageByID(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				if state.Body != "committed [[Kept]]" || state.Version != "9223372036854775807" {
					t.Fatalf("exhaustion changed state: %+v", state)
				}
			})
		}
	}
}

func TestBrowserFormCarriesRevisionAndRefusesOldClockToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(path); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	store := &core.Store{DB: d}
	h := api.New(store)
	id, _, err := store.CreatePage(context.Background(), "cli", "Browser version", "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`UPDATE entities SET revision=9007199254740993 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", fmt.Sprintf("/pages/%d", id), nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="version" value="9007199254740993"`) {
		t.Fatalf("HTML token missing: status %d", rec.Code)
	}
	var clock string
	if err := d.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, id).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	post := func(token, body string) int {
		t.Helper()
		req := httptest.NewRequest("POST", fmt.Sprintf("/pages/%d/body", id), strings.NewReader(url.Values{"version": {token}, "body": {body}}.Encode()))
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		res := rec.Result()
		defer res.Body.Close()
		if _, err := io.Copy(io.Discard, res.Body); err != nil {
			t.Fatal(err)
		}
		return rec.Code
	}
	if got := post("9007199254740993", "committed"); got != http.StatusSeeOther {
		t.Fatalf("form save %d", got)
	}
	for _, token := range []string{"9007199254740993", clock} {
		if got := post(token, "stale [[Forbidden]]"); got != 409 {
			t.Fatalf("stale browser token %q: %d", token, got)
		}
	}
	p, err := store.PageByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != "committed" || p.Version != "9007199254740994" {
		t.Fatalf("browser persisted %+v", p)
	}
	if target, err := store.PageID(context.Background(), "Forbidden"); err != nil || target != 0 {
		t.Fatalf("browser stale target %d %v", target, err)
	}
}
