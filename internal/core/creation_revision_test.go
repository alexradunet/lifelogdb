package core

import (
	"strings"
	"testing"
)

// revisionClock is the edit token and the two audit instants of an entity, read straight from the file.
func revisionClock(t *testing.T, s *Store, id int64) (revision int64, createdAt, updatedAt string) {
	t.Helper()
	if err := s.DB.R.QueryRow(`SELECT revision, created_at, updated_at FROM entities WHERE id = ?`, id).
		Scan(&revision, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	return
}

// editedRows are the day-view rows flagged as edited.
func editedRows(t *testing.T, s *Store, day string) []string {
	t.Helper()
	d, err := s.Day(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range d.Rows {
		if strings.Contains(r.What, "(edited)") {
			out = append(out, r.What+" "+r.Detail)
		}
	}
	return out
}

// A row whose creation transaction is its only write is at revision 1 with updated_at equal to created_at, for every
// kind of page and every way the writer creates one; the day view then does not call it edited. Creation is not an
// edit: the owned preferred name and the typed detail row are part of it (lifelog_meta.edit_revisions).
func TestCreationIsNotAnEdit(t *testing.T) {
	const day = "2026-10-04"
	start, end := "2018-09", "2018-09-15"
	s := fresh(t)
	created := map[string]int64{}
	add := func(name string, id int64, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		created[name] = id
	}
	var id int64
	var err error
	err = s.Do(ctx, "cli", func(tx *Tx) (e error) { id, _, _, e = tx.CreatePage("Plain note", "text", day, ""); return })
	add("plain page", id, err)
	id, err = s.CreatePerson(ctx, "cli", "Sam Sample", "Sam Sample", "1990-01-02", "")
	add("person", id, err)
	err = s.Do(ctx, "cli", func(tx *Tx) (e error) {
		id, _, _, e = tx.CreatePage("Linked note", "met [[Sam Sample]] at [[Lakeside]] #health", day, "")
		return
	})
	add("page with wikilinks", id, err)
	id, err = s.CreatePlace(ctx, "cli", "Harbour")
	add("place", id, err)
	_, err = s.RegisterMetric(ctx, "cli", "Sleep", "h", "hours slept, see [[Lakeside]]")
	if err == nil {
		id, err = s.PageID(ctx, "Sleep")
	}
	add("metric with a note", id, err)
	_, err = s.RegisterMetric(ctx, "cli", "Steps", "count", "")
	if err == nil {
		id, err = s.PageID(ctx, "Steps")
	}
	add("metric without a note", id, err)
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "2026-10-04 Lake.jpg", SHA256: sha('a'), MIME: "image/jpeg",
		Body: "the lake with [[Sam Sample]]", Day: day, Preview: smallJPEG(t)})
	add("file", k.ID, err)
	err = s.Do(ctx, "cli", func(tx *Tx) (e error) {
		id, e = tx.CreatePeriod("Trip", "a trip, see [[Lakeside]]", &start, &end)
		return
	})
	add("period", id, err)
	// link targets a body save made: ghosts
	for _, title := range []string{"Lakeside", "health"} {
		id, err = s.PageID(ctx, title)
		add("ghost "+title, id, err)
	}

	for name, id := range created {
		if id == 0 {
			t.Errorf("%s was not created", name)
			continue
		}
		revision, createdAt, updatedAt := revisionClock(t, s, id)
		if revision != 1 || updatedAt != createdAt {
			t.Errorf("%s: revision %d, created %s, updated %s; want revision 1 and updated_at = created_at", name, revision, createdAt, updatedAt)
		}
	}
	if got := editedRows(t, s, day); len(got) != 0 {
		t.Errorf("never-edited pages shown as edited: %v", got)
	}

	// the clock alone is no edit: a skewed updated_at (a deliberate fixture, the revision untouched) is not flagged
	if _, err := s.DB.W.Exec(`UPDATE entities SET updated_at = '2030-01-01T00:00:00.000Z' WHERE id = ?`, created["plain page"]); err != nil {
		t.Fatal(err)
	}
	if got := editedRows(t, s, day); len(got) != 0 {
		t.Errorf("a page whose only change is its updated_at is shown as edited: %v", got)
	}

	// the control: a later change of the page's own content is an edit
	p, err := s.PageByID(ctx, created["plain page"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", p.ID, "text, rewritten", p.Version); err != nil {
		t.Fatal(err)
	}
	if revision, _, _ := revisionClock(t, s, p.ID); revision != 2 {
		t.Errorf("edited page: revision %d, want 2", revision)
	}
	got := editedRows(t, s, day)
	if len(got) != 1 || !strings.Contains(got[0], "Plain note") {
		t.Errorf("edited rows: %v; want only the page that was edited", got)
	}
}

// Creating a page by a first capture of a day writes its text after creating it, which the day view never flags
// (it lists the day page apart, not as a page written that day); a day with no text, only a mood, is untouched.
func TestCapturedDayPageWithoutTextIsAtRevisionOne(t *testing.T) {
	s := fresh(t)
	mood := 4.0
	id, _, err := s.Capture(ctx, "cli", "2026-10-05", "", &mood)
	if err != nil {
		t.Fatal(err)
	}
	if revision, createdAt, updatedAt := revisionClock(t, s, id); revision != 1 || updatedAt != createdAt {
		t.Errorf("day page made by a mood: revision %d, created %s, updated %s", revision, createdAt, updatedAt)
	}
}

// An incoming link is not an edit of the page it points at: a capture that tags #health must not make another
// writer's open edit of the health page stale. The page that links is the one whose content changed.
func TestIncomingLinkDoesNotInvalidateAnOpenEdit(t *testing.T) {
	s := fresh(t)
	health, _, err := s.CreatePage(ctx, "cli", "health", "notes on health")
	if err != nil {
		t.Fatal(err)
	}
	topic, _, err := s.CreatePage(ctx, "cli", "Topic", "a topic")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.CreatePage(ctx, "cli", "Other", "another")
	if err != nil {
		t.Fatal(err)
	}
	open, err := s.PageByID(ctx, health) // the page another writer holds open
	if err != nil {
		t.Fatal(err)
	}
	_, _, updatedAt := revisionClock(t, s, health)

	// an unrelated capture, a body save of another page and a link made by hand all point at it
	day, _, err := s.Capture(ctx, "cli", "2026-10-06", "ran, felt fine #health", nil)
	if err != nil {
		t.Fatal(err)
	}
	topicPage, err := s.PageByID(ctx, topic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "api", topic, "about [[health]]", topicPage.Version); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "ui", topic, health, "part-of", "context"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "ui", other, health, "part-of", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Unlink(ctx, "ui", other, health, "part-of"); err != nil {
		t.Fatal(err)
	}
	now, err := s.PageByID(ctx, health)
	if err != nil {
		t.Fatal(err)
	}
	if len(now.In) < 2 {
		t.Fatalf("the page is not linked to: %+v", now.In)
	}
	if now.Version != open.Version {
		t.Errorf("incoming links moved the version of the page they point at: %s -> %s", open.Version, now.Version)
	}
	if _, _, upd := revisionClock(t, s, health); upd != updatedAt {
		t.Errorf("incoming links moved updated_at: %s -> %s", updatedAt, upd)
	}
	if _, err := s.SaveBody(ctx, "ui", health, "notes on health, rewritten", open.Version); err != nil {
		t.Errorf("the open edit was refused after unrelated incoming links: %v", err)
	}
	// the pages that did the linking are the ones that changed
	if revision, _, _ := revisionClock(t, s, day); revision < 2 {
		t.Errorf("the day page that captured the tag is at revision %d", revision)
	}
}
