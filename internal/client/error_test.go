package client

import (
	"net/http"
	"strings"
	"testing"
)

// An answer that is not a Siren entity (a picture, a page) is reported by its status and type, never by its bytes:
// a diagnostic carries nothing of a file (docs/plans/098-the-codebase-made-cohesive.md).
func TestAClientErrorNamesNoBody(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xff\xd8SYNTHETIC-PICTURE-BYTES"))
	})
	_, err := InProcess(h, "cli").Get(t.Context(), "/pages/1/preview")
	if err == nil {
		t.Fatal("a picture was read as an entity")
	}
	if strings.Contains(err.Error(), "SYNTHETIC-PICTURE-BYTES") {
		t.Errorf("the error carries the body: %v", err)
	}
	if !strings.Contains(err.Error(), "image/jpeg") || !strings.Contains(err.Error(), "200") {
		t.Errorf("the error names neither the type nor the status: %v", err)
	}
}
