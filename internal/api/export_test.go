package api

import (
	"net/http"
	"time"

	"lifelog/internal/core"
)

// NewOn is the API with the server's local day fixed to day and its zone UTC, for the external tests: what is due,
// overdue and today is then read against fixed dates, and a time is written in fixed words.
func NewOn(s *core.Store, day string, trustedOrigins ...string) http.Handler {
	return newHandler(s, func() string { return day }, time.UTC, trustedOrigins...)
}
