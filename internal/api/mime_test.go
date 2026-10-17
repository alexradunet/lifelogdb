package api

import "testing"

// mimeOf names a file's type from its extension (a fixed table, the same on every machine), else from its first bytes.
func TestMimeOf(t *testing.T) {
	for name, want := range map[string]string{"a.JPG": "image/jpeg", "IMG_1.heic": "image/heic", "memo.m4a": "audio/mp4", "x.pdf": "application/pdf"} {
		if got := mimeOf(name, nil); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if got := mimeOf("noext", []byte("%PDF-1.7\n")); got != "application/pdf" {
		t.Errorf("sniffed: %s", got)
	}
	if got := mimeOf("noext", []byte("plain words")); got != "text/plain" {
		t.Errorf("sniffed text: %s", got)
	}
}
