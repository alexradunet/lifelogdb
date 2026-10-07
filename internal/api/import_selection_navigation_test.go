package api_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func TestImportStatusLinksToExistingSelectionReviews(t *testing.T) {
	source := filepath.Join(t.TempDir(), "Source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "daily.csv"), []byte("Date,Distance (m)\n2020-01-02,10.5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "one.jpg"), phototest.JPEG(3, 2, photo.Meta{Orientation: 1}, true), 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := importer.Open(source + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.Setup(""); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	if err = ws.DraftRules("source: import:synthetic\n"); err != nil {
		t.Fatal(err)
	}
	review, err := ws.Review("rules.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = ws.Approve("rules.md", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), review.Hash); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DraftPrepared(context.Background(), "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DraftSelectedPhoto(context.Background(), "one.jpg", "", "One.jpg", "<script>synthetic-caption</script>", importer.PhotoChoices{Capture: "none", GPS: "none"}); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ws.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	handler := api.New(&core.Store{DB: d}, ws)
	server := httptest.NewServer(handler)
	defer server.Close()
	c := client.Remote(server.URL, "agent:synthetic")
	status, err := c.Get("/import")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/import/prepared", "/import/selected-photo"} {
		// These GET resources exist; the status document should let clients discover them.
		if _, err := c.Get(want); err != nil {
			t.Fatalf("review resource control %s: %v", want, err)
		}
		found := false
		for _, link := range status.Links {
			if link.Href == want {
				found = true
			}
		}
		if !found {
			t.Errorf("existing selection review %s is absent from import status navigation: %+v", want, status.Links)
		}
	}
	html := browse(t, handler, "/import")
	for _, href := range []string{"/import/prepared", "/import/selected-photo"} {
		if !strings.Contains(html, `href="`+href+`"`) {
			t.Fatalf("HTML cannot discover %s", href)
		}
	}
	reviewHTML := browse(t, handler, "/import/selected-photo")
	if strings.Contains(reviewHTML, "<script>synthetic-caption</script>") || !strings.Contains(reviewHTML, "&lt;script&gt;") {
		t.Fatal("review evidence not safely escaped")
	}
	for _, path := range []string{"/import/prepared", "/import/selected-photo"} {
		e, err := c.Get(path)
		if err != nil {
			t.Fatal(err)
		}
		check, apply := false, false
		for _, a := range e.Actions {
			if strings.Contains(a.Name, "approve") {
				t.Fatal("stamp action exposed")
			}
			check = check || strings.HasPrefix(a.Name, "check-")
			apply = apply || strings.HasPrefix(a.Name, "apply-")
		}
		if !check || !apply {
			t.Fatal("review lost check/apply actions")
		}
	}

}
