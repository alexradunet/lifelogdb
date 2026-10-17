package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const feedbackLimit = 64 << 10
const feedbackEntries = 64
const feedbackLifetime = 10 * time.Minute

// Receipts are private, per-server, one-use presentation state, not sessions.
// A restart or expiry loses only feedback, never the saved resource.
type feedbackReceipt struct {
	path, text string
	expires    time.Time
}
type feedbackStore struct {
	mu      sync.Mutex
	entries map[string]feedbackReceipt
	now     func() time.Time // the clock of the receipts: the server gives the store's (core.Store.Now)
}

func (s *feedbackStore) put(destination string, result any) string {
	if result == nil {
		return destination
	}
	u, err := url.Parse(destination)
	if err != nil {
		return destination
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return destination
	}
	// Keep a bounded textual preview rather than a whole entity or request. The
	// explicit notice distinguishes an incomplete list from a complete outcome.
	const notice = "\nFeedback truncated: more result details were omitted from this temporary preview."
	if len(b) > feedbackLimit {
		b = append(b[:feedbackLimit-len(notice):feedbackLimit-len(notice)], notice...)
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return destination
	}
	token := hex.EncodeToString(random[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.entries == nil {
		s.entries = make(map[string]feedbackReceipt)
	}
	s.prune(now)
	if len(s.entries) >= feedbackEntries {
		var oldest string
		var expiry time.Time
		for key, r := range s.entries {
			if oldest == "" || r.expires.Before(expiry) {
				oldest, expiry = key, r.expires
			}
		}
		delete(s.entries, oldest)
	}
	s.entries[token] = feedbackReceipt{u.Path, string(b), now.Add(feedbackLifetime)}
	q := u.Query()
	q.Set("feedback", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *feedbackStore) take(path, token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(s.now())
	r, ok := s.entries[token]
	if !ok || r.path != path {
		return ""
	}
	delete(s.entries, token)
	return r.text
}

// prune deletes the receipts that expired at now. The caller holds mu.
func (s *feedbackStore) prune(now time.Time) {
	for key, r := range s.entries {
		if !now.Before(r.expires) {
			delete(s.entries, key)
		}
	}
}

// move gives the receipt for path to destination, the page a browser is sent to instead (the page of a metric opens
// its metric, docs/plans/100-one-page-a-metric.md), and reports whether there was one to give.
func (s *feedbackStore) move(token, path, destination string) bool {
	u, err := url.Parse(destination)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.entries[token]
	if !ok || r.path != path || !s.now().Before(r.expires) {
		return false
	}
	r.path = u.Path
	s.entries[token] = r
	return true
}

// redirectKeepingFeedback is the redirect of a browser's GET to the page to, the page that stands for this one. A
// receipt of feedback that this request carries moves to that page, so that the toast of a write is read there.
func (h *server) redirectKeepingFeedback(r *http.Request, to string) redirect {
	if t := r.URL.Query().Get("feedback"); t != "" && h.feedback.move(t, r.URL.Path, to) {
		to += "?" + url.Values{"feedback": {t}}.Encode()
	}
	return redirect{to}
}
