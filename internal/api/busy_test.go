package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/core"
)

// holdWriteLock makes a second writer on the file of s that holds the write lock until the test ends, and shortens the
// busy wait of the store's one writer connection, so that a write meets a real SQLITE_BUSY at once. Only this test's
// connection changes: the settings that the program sets on a new connection stay as they are (connections.md).
func holdWriteLock(t *testing.T, s *core.Store) {
	t.Helper()
	if _, err := s.DB.W.ExecContext(t.Context(), "PRAGMA busy_timeout = 20"); err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", "file:"+filepath.ToSlash(s.DB.Path())) // the plain driver: this writer is the test's
	if err != nil {
		t.Fatal(err)
	}
	conn, err := other.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.ExecContext(context.WithoutCancel(t.Context()), "ROLLBACK")
		conn.Close()
		other.Close()
	})
}

// TestABusyFileAnswers503WithRetryAfter: another writer holds the file's write lock, so the write finds SQLITE_BUSY
// when it begins. Nothing was applied, so the answer is 503 with the code busy and Retry-After, as JSON and as a page,
// and the log has no line: a busy file is not a fault of this program.
func TestABusyFileAnswers503WithRetryAfter(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	holdWriteLock(t, s)
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
