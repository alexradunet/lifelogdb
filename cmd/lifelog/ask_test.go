package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/db"
)

// TestAskCommand runs lifelog ask in-process against a fake model of the environment, then without one.
func TestAskCommand(t *testing.T) {
	live := filepath.Join(t.TempDir(), "synthetic.db")
	if err := db.Init(t.Context(), live); err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "Hello back."}}}})
	}))
	t.Cleanup(model.Close)

	t.Setenv("LIFELOG_MODEL_URL", model.URL+"/v1")
	t.Setenv("LIFELOG_MODEL", "synthetic")
	o, err := parse([]string{"ask", "Hello", "there", "--db", live})
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := runContext(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	})
	var e struct {
		Title  string `json:"title"`
		Result struct {
			Conversation int64  `json:"conversation"`
			Reply        string `json:"reply"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if !strings.HasPrefix(e.Title, "Conversation ") || e.Result.Reply != "Hello back." || e.Result.Conversation == 0 {
		t.Fatalf("answer = %+v", e)
	}

	// The same conversation, continued by its id.
	o, err = parse([]string{"ask", "Again", "--conversation", "999", "--db", live})
	if err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := runContext(context.Background(), o); err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("a missing conversation: err = %v, want 404", err)
		}
	})

	t.Setenv("LIFELOG_MODEL_URL", "")
	o, err = parse([]string{"ask", "Hello", "--db", live})
	if err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := runContext(context.Background(), o); err == nil || !strings.Contains(err.Error(), "LIFELOG_MODEL_URL") {
			t.Fatalf("no model: err = %v, want the refusal that names LIFELOG_MODEL_URL", err)
		}
	})
	if _, err := parse([]string{"capture", "x", "--conversation", "1"}); err == nil {
		t.Fatal("capture took --conversation")
	}
}
