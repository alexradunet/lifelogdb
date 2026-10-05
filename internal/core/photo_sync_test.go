package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func daySyncJSON(t *testing.T, k Kept) *Sync {
	t.Helper()
	b, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		DaySync *Sync `json:"day_sync"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result.DaySync
}

func TestPhotoDaySynchronizationFeedback(t *testing.T) {
	s := fresh(t)
	id, _, err := s.Capture(ctx, "cli", "2026-10-04", "[[Old target]] [[bad/title]]", nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.PageID(ctx, "Old target")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", target); err != nil {
		t.Fatal(err)
	}
	f := FileIn{Title: "New photo.jpg", SHA256: sha('d'), MIME: "image/jpeg", Preview: smallJPEG(t), Day: "2026-10-04", Body: "[[File target]]"}
	assert := func(k Kept) {
		t.Helper()
		sync := daySyncJSON(t, k)
		if sync == nil || strings.Join(sync.Revived, ",") != "Old target" || strings.Join(sync.Skipped, ",") != "bad/title" {
			t.Errorf("day sync: %+v", sync)
		}
		if k.Sync == nil || strings.Join(k.Sync.Created, ",") != "File target" {
			t.Errorf("file sync: %+v", k.Sync)
		}
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	dry, err := s.TryAddFile(ctx, "cli", f)
	if err != nil {
		t.Fatal(err)
	}
	assert(dry)
	old, err := s.PageByID(ctx, target)
	if err != nil || old.DeletedAt == "" {
		t.Fatalf("dry revival leaked: %+v %v", old, err)
	}
	after, err := s.PageByID(ctx, id)
	if err != nil || after.Body != before.Body {
		t.Fatal("dry body leaked")
	}
	k, err := s.AddFile(ctx, "cli", f)
	if err != nil {
		t.Fatal(err)
	}
	assert(k)
	old, err = s.PageByID(ctx, target)
	if err != nil || old.DeletedAt != "" {
		t.Fatal("not revived")
	}
	again, err := s.AddFile(ctx, "cli", f)
	if err != nil || again.Embedded || daySyncJSON(t, again) != nil || again.Sync != nil {
		t.Fatalf("no-op: %+v %v", again, err)
	}
}

func TestPhotoEmbedNFDLongTitleDeduplication(t *testing.T) {
	s := fresh(t)
	title := strings.Repeat("é", 100) + ".jpg"
	body := "![[" + strings.Repeat("e\u0301", 100) + ".jpg|existing]]"
	day, _, err := s.Capture(ctx, "cli", "2026-10-04", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddFile(ctx, "cli", FileIn{Title: title, SHA256: sha('e'), MIME: "image/jpeg", Preview: smallJPEG(t), Day: "2026-10-04"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PageByID(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	if k.Embedded || p.Body != body || titles(p.Out, "wikilink") != title {
		t.Fatalf("duplicate/graph: %+v %+v", k, p)
	}
}
