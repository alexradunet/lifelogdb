package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
)

func receiptToken(location string) string {
	u, _ := url.Parse(location)
	return u.Query().Get("feedback")
}

// TestTheFeedbackReceiptsKeepTheMonotonicReading: the server gives its feedback receipts the instant of the store's
// clock with its monotonic reading (core.Store.Now), not the same instant in the zone (core.Store.Local), which has
// none. Thus the life of a receipt does not change when the wall clock is set. A time that keeps the monotonic reading
// is not equal to its Round(0).
func TestTheFeedbackReceiptsKeepTheMonotonicReading(t *testing.T) {
	h := newServer(&core.Store{Zone: time.FixedZone("+14", 14*60*60)})
	if now := h.feedback.now(); now == now.Round(0) {
		t.Errorf("the clock of the receipts gives %v, with no monotonic reading: want core.Store.Now", now)
	}
}

func TestBrowserMutationFeedbackReceipts(t *testing.T) {
	now := time.Unix(0, 0)
	s := feedbackStore{now: func() time.Time { return now }}
	token := receiptToken(s.put("/pages/1", map[string]any{"revived": []string{"<script>private</script>"}}))
	if len(token) != 64 {
		t.Fatal("not an opaque random token")
	}
	if s.take("/pages/2", token) != "" || s.take("/pages/1", "unknown") != "" {
		t.Fatal("invalid token disclosed feedback")
	}
	if got := s.take("/pages/1", token); !strings.Contains(got, "private") {
		t.Fatal("wrong target consumed receipt")
	}
	if s.take("/pages/1", token) != "" {
		t.Fatal("replayed")
	}
	token = receiptToken(s.put("/pages/1", true))
	now = now.Add(feedbackLifetime)
	if s.take("/pages/1", token) != "" {
		t.Fatal("expired receipt")
	}
	token = receiptToken(s.put("/pages/1", true))
	for range feedbackEntries {
		now = now.Add(time.Second)
		s.put("/pages/1", true)
	}
	if len(s.entries) != feedbackEntries || s.take("/pages/1", token) != "" {
		t.Fatal("entry cap/eviction")
	}
	token = receiptToken(s.put("/pages/1", strings.Repeat("x", feedbackLimit*2)))
	got := s.take("/pages/1", token)
	if len(got) > feedbackLimit || !strings.Contains(got, "Feedback truncated") {
		t.Fatal("payload cap/notice")
	}
	restarted := feedbackStore{now: s.now}
	if restarted.take("/pages/1", token) != "" {
		t.Fatal("restart retained receipt")
	}
}

// TestTheFeedbackDestinationKeepsItsFragment: the receipt goes in the query, before the fragment that names the row
// the page opens at. The receipt is matched by the path alone, as a browser never sends a fragment.
func TestTheFeedbackDestinationKeepsItsFragment(t *testing.T) {
	s := feedbackStore{now: func() time.Time { return time.Unix(0, 0) }}
	got := s.put("/plans#row-1-3", map[string]any{"ok": true})
	u, err := url.Parse(got)
	if err != nil || u.Path != "/plans" || u.Fragment != "row-1-3" || len(u.Query().Get("feedback")) != 64 {
		t.Fatalf("put gave %q", got)
	}
	if !strings.HasPrefix(got, "/plans?feedback=") || !strings.HasSuffix(got, "#row-1-3") {
		t.Errorf("put gave %q, want /plans?feedback=...#row-1-3", got)
	}
	if s.take("/plans", u.Query().Get("feedback")) == "" {
		t.Error("the receipt was not found by the path alone")
	}
}

func TestBrowserMutationFeedbackIndependentTabs(t *testing.T) {
	s := feedbackStore{now: func() time.Time { return time.Unix(0, 0) }}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			destination := "/pages/" + strings.Repeat("x", i+1)
			token := receiptToken(s.put(destination, destination))
			if !strings.Contains(s.take(destination, token), destination) {
				t.Error("tab lost its own feedback")
			}
		})
	}
	wg.Wait()
}

func TestBrowserMutationFeedbackEscaping(t *testing.T) {
	h := &server{feedback: feedbackStore{now: func() time.Time { return time.Unix(0, 0) }}}
	endpoint := h.serve(func(r *http.Request) (*siren.Entity, error) {
		e := &siren.Entity{Class: []string{"page"}, Title: "Synthetic", Properties: map[string]any{"body": "", "entity_type": "page"}, Links: []siren.Link{link("self", "/pages/1", "Synthetic")}}
		if r.Method == "POST" {
			e.Result = map[string]any{"skipped": []string{"<script>alert(1)</script>"}}
		}
		return e, nil
	})
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		endpoint(w, r)
		return w
	}
	post := request("POST", "/pages/1")
	location := post.Header().Get("Location")
	body := request("GET", location).Body.String()
	if !strings.Contains(body, "Result") || strings.Contains(body, "<script>") || !strings.Contains(body, "alert(1)") {
		t.Fatalf("unsafe or missing feedback: %s", body)
	}
	if strings.Contains(request("GET", "/pages/1?feedback=<script>&message=forged-success").Body.String(), "forged-success") {
		t.Fatal("query trusted as message")
	}
	if strings.Contains(request("GET", location).Body.String(), "<h2>Result</h2>") {
		t.Fatal("refresh replayed")
	}
}
