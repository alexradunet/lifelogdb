package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func receiptToken(location string) string {
	u, _ := url.Parse(location)
	return u.Query().Get("feedback")
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
	restarted := feedbackStore{}
	if restarted.take("/pages/1", token) != "" {
		t.Fatal("restart retained receipt")
	}
}
func TestBrowserMutationFeedbackIndependentTabs(t *testing.T) {
	s := feedbackStore{}
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
	h := &server{}
	endpoint := h.serve(func(r *http.Request) (*Entity, error) {
		e := &Entity{Class: []string{"page"}, Title: "Synthetic", Properties: map[string]any{"body": "", "entity_type": "page"}, Links: []Link{link("self", "/pages/1", "Synthetic")}}
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
