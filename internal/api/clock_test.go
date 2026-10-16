package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

// TestTheStoresClockIsTheOneClock: the store's clock is in a zone far from UTC, at an instant where the UTC day and
// the local day are not the same. Each read of today or now agrees with the local day or the instant of that clock:
// the root's today, the redirect of /days/today, the day page that a capture with no day makes, the day of a page and
// of a kept file that have no day, the words for a time, the name of a snapshot, and the life of a feedback receipt.
func TestTheStoresClockIsTheOneClock(t *testing.T) {
	d := dbtest.New(t)
	const today, utcDay = "2026-03-11", "2026-03-10"
	zone := time.FixedZone("+14", 14*60*60)               // the offset of Pacific/Kiritimati, with no zone database
	now := time.Date(2026, 3, 10, 11, 30, 0, 0, time.UTC) // 2026-03-11 01:30:00 in the zone
	s := &core.Store{DB: d, SnapshotDir: t.TempDir(), Clock: func() time.Time { return now }, Zone: zone}
	h := api.New(s)
	c := client.InProcess(h, "cli")
	ctx := t.Context()

	root := must(c.Get(ctx, "/"))
	if got := props(root)["today"]; got != today {
		t.Errorf("the root's today is %v, want %s", got, today)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/days/today", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/days/"+today {
		t.Errorf("/days/today: %d to %q, want 303 to /days/%s", rec.Code, rec.Header().Get("Location"), today)
	}

	// A capture with no day, from a browser: it writes on the local day, and the answer carries a receipt.
	capture := func() string {
		t.Helper()
		form := url.Values{"text": {"Woke before the sun."}}
		req := httptest.NewRequest("POST", "/days/today/capture", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		to := rec.Header().Get("Location")
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(to, "/days/"+today+"?feedback=") {
			t.Fatalf("a capture with no day: %d to %q, want 303 to /days/%s with a receipt", rec.Code, to, today)
		}
		return to
	}
	feedbackOf := func(to string) bool {
		t.Helper()
		return strings.Contains(browse(t, h, to), "<h2>Result</h2>")
	}
	if !feedbackOf(capture()) {
		t.Error("a receipt is not shown at the instant it was made")
	}
	if id, err := s.PageID(ctx, today); err != nil || id == 0 {
		t.Errorf("the capture made no day page %s: %v", today, err)
	}
	if id, err := s.PageID(ctx, utcDay); err != nil || id != 0 {
		t.Errorf("the capture made the day page of the UTC day %s: id %d, %v", utcDay, id, err)
	}

	// A page and a file that is not a photo, both with no day, take the local day.
	must(c.Do(ctx, find(root, "create-page"), map[string]string{"title": "Night thought"}))
	must(c.Do(ctx, find(root, "add-file"), map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ab", 32), "mime": "application/pdf"}))
	for _, title := range []string{"Night thought", "Scan.pdf"} {
		id, err := s.PageID(ctx, title)
		if err != nil {
			t.Fatal(err)
		}
		page, err := s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if page.Day != today {
			t.Errorf("%s has the day %q, want %s", title, page.Day, today)
		}
	}

	// The words for a time are in the zone of the clock: a reading taken at the instant of the clock was taken today
	// at 01:30, not yesterday at 11:30.
	must(c.Do(ctx, find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	m := must(c.Do(ctx, find(root, "record"), map[string]string{"metric": "weight", "day": today, "value": "71", "taken_at": "2026-03-10T11:30:00.000Z"}))
	if want := `>today, 01:30</time>`; !strings.Contains(browse(t, h, href(m, "self")), want) {
		t.Errorf("the page of a reading taken now does not say %s", want)
	}

	// A snapshot is named by the local day, and a second one of that day by the local time too.
	var names []string
	for range 2 {
		e := must(c.Do(ctx, find(root, "snapshot"), nil))
		path, _ := props(e)["snapshot"].(string)
		names = append(names, filepath.Base(path))
	}
	if want := "life-2026-03-11.db life-2026-03-11T013000.db"; strings.Join(names, " ") != want {
		t.Errorf("the snapshots are %v, want %s", names, want)
	}

	// A receipt lives for some minutes of the store's clock: an hour later by that clock, it is gone.
	to := capture()
	now = now.Add(time.Hour)
	if feedbackOf(to) {
		t.Error("a receipt is shown an hour after it was made, by the store's clock")
	}
}
