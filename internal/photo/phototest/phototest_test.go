package phototest

import (
	"errors"
	"image"
	"image/jpeg"
	"io"
	"runtime"
	"strings"
	"testing"

	"lifelog/internal/photo"
)

// TestJPEGPanicsWithTheEncodeError: a size that a JPEG cannot hold stops JPEG with the error of jpeg.Encode, not
// with an index error after the encode failed.
func TestJPEGPanicsWithTheEncodeError(t *testing.T) {
	const w, h = 1 << 16, 1
	want := jpeg.Encode(io.Discard, image.NewRGBA(image.Rect(0, 0, w, h)), nil)
	if want == nil {
		t.Fatalf("jpeg.Encode accepts a %d×%d image: the test needs another size", w, h)
	}
	defer func() {
		got, ok := recover().(error)
		var index runtime.Error
		if !ok || errors.As(got, &index) || !strings.Contains(got.Error(), want.Error()) {
			t.Errorf("JPEG panicked with %v, want the error of jpeg.Encode: %v", got, want)
		}
	}()
	JPEG(w, h, photo.Meta{}, false)
}
