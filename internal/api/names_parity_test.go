package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestStableNameTransportParity(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, kind := range []string{"page", "person", "place", "metric", "file"} {
			t.Run(fmt.Sprintf("remote=%v/type=%s", remote, kind), func(t *testing.T) {
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
				s := &core.Store{DB: d}
				h := api.New(s)
				c := client.InProcess(h, "agent:names")
				if remote {
					server := httptest.NewServer(h)
					t.Cleanup(server.Close)
					c = client.Remote(server.URL, "agent:names")
				}
				old := "Old " + kind
				var id int64
				switch kind {
				case "page":
					id, _, err = s.CreatePage(context.Background(), "cli", old, "original prose")
				case "person":
					id, err = s.CreatePerson(context.Background(), "cli", old, "Synthetic Person", "", "2031-01-01")
				case "place":
					id, err = s.CreatePlace(context.Background(), "cli", old)
				case "metric":
					_, err = s.RegisterMetric(context.Background(), "cli", old, "kg", "original metric")
					if err == nil {
						id, err = s.PageID(context.Background(), old)
					}
				case "file":
					var kept core.Kept
					kept, err = s.AddFile(context.Background(), "cli", core.FileIn{Title: old, SHA256: strings.Repeat("a", 64), MIME: "text/plain", Body: "original file", Day: "2031-01-01"})
					id = kept.ID
				}
				if err != nil {
					t.Fatal(err)
				}
				ref, _, err := s.CreatePage(context.Background(), "cli", "Reference", "See [["+old+"]].")
				if err != nil {
					t.Fatal(err)
				}
				initial, err := s.PageByID(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				resource := fmt.Sprintf("/pages/%d", id)
				for _, title := range []string{"New " + kind, "NEW " + strings.ToUpper(kind), old, "Final " + kind} {
					page, err := c.Get(resource)
					if err != nil {
						t.Fatal(err)
					}
					if find(page, "rename").Name != "rename" {
						t.Fatal("typed entity lacks rename action")
					}
					renamed, err := c.Do(find(page, "rename"), map[string]string{"title": title})
					if err != nil {
						t.Fatal(err)
					}
					if renamed.Title != title {
						t.Fatalf("preferred response title=%q", renamed.Title)
					}
					resolved, err := c.Get("/pages?title=" + url.QueryEscape(old))
					if err != nil || resolved.Title != title {
						t.Fatalf("old alias GET: %+v, %v", resolved, err)
					}
					state, err := s.PageByID(context.Background(), id)
					if err != nil {
						t.Fatal(err)
					}
					if state.ID != id || state.Type != kind || state.Body != initial.Body || state.Day != initial.Day || !reflect.DeepEqual(state.Person, initial.Person) || !reflect.DeepEqual(state.File, initial.File) {
						t.Fatalf("rename changed domain fields: %+v", state)
					}
					reference, err := s.PageByID(context.Background(), ref)
					if err != nil {
						t.Fatal(err)
					}
					if reference.Body != "See [["+old+"]]." || len(reference.Out) != 1 || reference.Out[0].ID != id || reference.Out[0].Title != title {
						t.Fatalf("old reference: %+v", reference)
					}
				}
				_, _, err = s.CreatePage(context.Background(), "cli", "Ghost ref", "[[Reserved ghost]]")
				if err != nil {
					t.Fatal(err)
				}
				tomb, _, err := s.CreatePage(context.Background(), "cli", "Reserved tombstone", "")
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Tombstone(context.Background(), "cli", tomb); err != nil {
					t.Fatal(err)
				}
				before, err := s.PageByID(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				var names int
				if err := d.R.QueryRow("SELECT count(*) FROM entity_names").Scan(&names); err != nil {
					t.Fatal(err)
				}
				for title, status := range map[string]int{"Reserved ghost": 409, "Reserved tombstone": 409, "2031-02-01": 409, "Lab [unsafe]": 422} {
					page, err := c.Get(resource)
					if err != nil {
						t.Fatal(err)
					}
					_, err = c.Do(find(page, "rename"), map[string]string{"title": title})
					var ce *client.Error
					if !errors.As(err, &ce) || ce.Status != status {
						t.Fatalf("rename %q: %v; want %d", title, err, status)
					}
					after, err := s.PageByID(context.Background(), id)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(before, after) {
						t.Fatal("refused rename changed entity")
					}
				}
				var afterNames int
				if err := d.R.QueryRow("SELECT count(*) FROM entity_names").Scan(&afterNames); err != nil {
					t.Fatal(err)
				}
				if afterNames != names {
					t.Fatal("refused rename reserved an additional name")
				}
			})
		}
	}
}

func TestBrowserStableNameResolution(t *testing.T) {
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
	s := &core.Store{DB: d}
	h := api.New(s)
	id, err := s.CreatePerson(context.Background(), "cli", "Old person", "Synthetic Person", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := s.CreatePage(context.Background(), "cli", "Old reference", "See [[Old person]].")
	if err != nil {
		t.Fatal(err)
	}
	post := httptest.NewRequest("POST", fmt.Sprintf("/pages/%d/rename", id), strings.NewReader(url.Values{"title": {"New person"}}.Encode()))
	post.Header.Set("Accept", "text/html")
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != fmt.Sprintf("/pages/%d", id) {
		t.Fatalf("rename HTML: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, href := range []string{fmt.Sprintf("/pages/%d", id), fmt.Sprintf("/pages/%d", ref)} {
		req := httptest.NewRequest("GET", href, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("HTML: %d", rec.Code)
		}
		if href == fmt.Sprintf("/pages/%d", id) && !strings.Contains(rec.Body.String(), "New person") {
			t.Fatal("preferred title absent from HTML")
		}
		if href == fmt.Sprintf("/pages/%d", ref) && !strings.Contains(rec.Body.String(), fmt.Sprintf(`href="/pages/%d"`, id)) {
			t.Fatal("old reference rendered to wrong identity")
		}
	}
}
