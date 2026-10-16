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

// fileKind is the part of a type before the '/'. A row that is not a file has no type and no kind. files_mime refuses
// a type with no '/' in the file; fileKind gives such a value whole, and does not stop the list.
func TestFileKind(t *testing.T) {
	for mime, want := range map[string]string{"image/jpeg": "image", "application/pdf": "application", "": "", "noslash": "noslash"} {
		if got := fileKind(mime); got != want {
			t.Errorf("fileKind(%q) = %q, want %q", mime, got, want)
		}
	}
}
