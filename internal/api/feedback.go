package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	now     func() time.Time
}

func (s *feedbackStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
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
	now := s.clock()
	if s.entries == nil {
		s.entries = make(map[string]feedbackReceipt)
	}
	for key, r := range s.entries {
		if !now.Before(r.expires) {
			delete(s.entries, key)
		}
	}
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
	now := s.clock()
	for key, r := range s.entries {
		if !now.Before(r.expires) {
			delete(s.entries, key)
		}
	}
	r, ok := s.entries[token]
	if !ok || r.path != path {
		return ""
	}
	delete(s.entries, token)
	return r.text
}
