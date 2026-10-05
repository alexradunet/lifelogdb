package api

import (
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"testing"
)

func TestMultipartUnknownNameProjection(t *testing.T) {
	// Generate modest, individually legal headers incrementally, not a stress fixture.
	const boundary = "synthetic-boundary"
	readers := []io.Reader{}
	for i := 0; i < 4; i++ {
		readers = append(readers, strings.NewReader("--"+boundary+"\r\nContent-Disposition: form-data; name=\"unknown"+fmt.Sprint(i)), io.LimitReader(nameBytes{}, 64<<10), strings.NewReader("\"\r\n\r\n\r\n"))
	}
	readers = append(readers, strings.NewReader("--"+boundary+"--\r\n"))
	mr := multipart.NewReader(io.MultiReader(readers...), boundary)
	retained := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := part.FormName()
		if len(name) < 64<<10 {
			t.Fatalf("unexpected name length %d", len(name))
		}
		if key := fileTextField(name); key != "" {
			retained[key] = ""
		}
		part.Close()
	}
	if len(retained) != 0 {
		t.Fatalf("retained %d unknown names despite zero text bytes", len(retained))
	}
	for _, name := range []string{"title", "sha256", "mime", "body", "day", "at", "radius", "dry_run"} {
		if got := fileTextField(name); got != name {
			t.Errorf("recognized %q projected to %q", name, got)
		}
	}
	for _, name := range []string{"", "original", "preview", "unknown", "Title", "title-extra"} {
		if got := fileTextField(name); got != "" {
			t.Errorf("unknown %q projected to %q", name, got)
		}
	}
}

type nameBytes struct{}

func (nameBytes) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
