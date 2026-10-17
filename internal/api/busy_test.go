package api_test

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
)

// TestABusyFileAnswers503WithRetryAfter: another writer holds the file's write lock, so the write finds SQLITE_BUSY
// when it begins. Nothing was applied, so the answer is 503 with the code busy and Retry-After, as JSON and as a page,
// and the log has no line: a busy file is not a fault of this program.
func TestABusyFileAnswers503WithRetryAfter(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	api.HoldWriteLock(t, s, 20)
	for _, accept := range []string{"application/vnd.siren+json", "text/html"} {
		t.Run(accept, func(t *testing.T) {
			var logged bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logged) // no test of this package runs in parallel
			t.Cleanup(func() { log.SetOutput(prev) })
			rec := request(h, "POST", "/pages", "title=Busy", map[string]string{"Accept": accept, "Host": "localhost"})
			body := rec.Body.String()
			if rec.Code != 503 {
				t.Fatalf("status %d, want 503: %.300s", rec.Code, body)
			}
			if got := rec.Header().Get("Retry-After"); got != "1" {
				t.Errorf("Retry-After = %q, want 1", got)
			}
			if accept == "text/html" {
				if !strings.Contains(body, "retry is safe") {
					t.Errorf("the page does not say that a retry is safe: %.600s", body)
				}
			} else if !strings.Contains(body, `"code": "busy"`) {
				t.Errorf("the entity does not carry the code busy: %.300s", body)
			}
			if logged.Len() != 0 {
				t.Errorf("a busy file was logged as a fault: %q", logged.String())
			}
		})
	}
}
