package api

import (
	"net/http"

	"lifelog/internal/core"
)

// NewOn is the API with the server's local day fixed to day, for the external tests: what is due, overdue and
// today is then read against fixed dates.
func NewOn(s *core.Store, day string, trustedOrigins ...string) http.Handler {
	return newHandler(s, func() string { return day }, trustedOrigins...)
}
