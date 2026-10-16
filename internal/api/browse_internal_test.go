package api

import "testing"

// fileKind is the part of a type before the '/'. A row that is not a file has no type and no kind. files_mime refuses
// a type with no '/' in the file; fileKind gives such a value whole, and does not stop the list.
func TestFileKind(t *testing.T) {
	for mime, want := range map[string]string{"image/jpeg": "image", "application/pdf": "application", "": "", "noslash": "noslash"} {
		if got := fileKind(mime); got != want {
			t.Errorf("fileKind(%q) = %q, want %q", mime, got, want)
		}
	}
}
