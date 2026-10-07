package api_test

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func TestBrowserSelectionReviewShowsClaimsChoicesAndUnknownDay(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Source")
	if e := os.Mkdir(source, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(source, "one.jpg"), phototest.JPEG(3, 2, photo.Meta{Orientation: 1}, true), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(source, "one.json"), []byte(`{"photoTakenTime":{"timestamp":"1577920200"},"creationTime":{"timestamp":"1700000000"}}`), 0600); e != nil {
		t.Fatal(e)
	}
	ws, e := importer.Open(source + ".lifelog")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ws.Setup(""); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "daily.csv"), []byte("Date,Distance (m)"+string(rune(10))+"2020-01-02,10"+string(rune(10))), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ws.MakeLedger(); e != nil {
		t.Fatal(e)
	}
	if e = ws.DraftRules("source: import:synthetic\n"); e != nil {
		t.Fatal(e)
	}
	stamp := func(name string) {
		t.Helper()
		r, e := ws.Review(name)
		if e != nil {
			t.Fatal(e)
		}
		if e = ws.Approve(name, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), r.Hash); e != nil {
			t.Fatal(e)
		}
	}
	stamp("rules.md")
	d, e := db.Open(ws.TrialDB())
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if _, e = (&core.Store{DB: d}).RegisterMetric(context.Background(), "cli", "Distance", "m", ""); e != nil {
		t.Fatal(e)
	}
	handler := api.New(&core.Store{DB: d}, ws)
	post := func(path string, values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	draft := post("/import/prepared", url.Values{"file": {"daily.csv"}, "profile": {"fit-date-csv-v1"}, "metrics": {`{"distance":"Distance"}`}})
	if draft.Code != 303 {
		t.Fatal("HTML prepared draft")
	}
	reviewBody := browse(t, handler, "/import/prepared")
	for _, want := range []string{"distance", "source_sha256", "2020-01-02"} {
		if !strings.Contains(reviewBody, want) {
			t.Fatal("HTML prepared evidence hidden")
		}
	}
	if blocked := post("/import/prepared/apply", nil); blocked.Code < 400 {
		t.Fatal("HTML prepared bypassed owner")
	}
	stamp("prepared.md")
	if applied := post("/import/prepared/apply", nil); applied.Code != 303 {
		t.Fatal("HTML prepared apply")
	}
	response := post("/import/selected-photo", url.Values{"file": {"one.jpg"}, "sidecar": {"one.json"}, "title": {"One.jpg"}, "choices": {`{"capture":"sidecar","gps":"none"}`}})
	if response.Code != 303 || response.Header().Get("Location") != "/import/selected-photo" {
		t.Fatalf("review redirect %d %s", response.Code, response.Body.String())
	}
	body := browse(t, handler, "/import/selected-photo")
	for _, expected := range []string{"source_claimed_capture_utc", "source_creation_claim_unestablished_meaning", "unresolved", "sidecar", "none", "original_sha256", "sidecar_sha256"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("hidden evidence %s", expected)
		}
	}
	response = post("/import/selected-photo/apply", nil)
	if response.Code < 400 {
		t.Fatal("HTML draft applied without owner stamp")
	}
	stamp("selected-photo.md")
	response = post("/import/selected-photo/apply", nil)
	if response.Code != 303 {
		t.Fatalf("HTML apply %d %s", response.Code, response.Body.String())
	}
	var unknown bool
	if e = d.R.QueryRow(`SELECT e.day IS NULL FROM files f JOIN entities e ON e.id=f.id`).Scan(&unknown); e != nil || !unknown {
		t.Fatal("HTML invented local capture day")
	}
}
