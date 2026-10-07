package importer

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// Creating a page is not editing it (lifelog_meta.edit_revisions): a page the vault import creates with its note's
// text is at revision 1, with updated_at equal to created_at, and the day view does not call it edited. Only a later
// change of the page's own content advances it.

type pageClock struct {
	revision           int64
	createdAt, updated string
}

// importedPageClocks are the edit clocks of the pages the import keyed, by title, read straight from the file.
func importedPageClocks(t *testing.T, s *core.Store) map[string]pageClock {
	t.Helper()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT n.title, e.revision, e.created_at, e.updated_at FROM entities e
	    JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
	   WHERE e.source = 'import:notebook' AND e.import_key IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]pageClock{}
	for rows.Next() {
		var title string
		var c pageClock
		if err := rows.Scan(&title, &c.revision, &c.createdAt, &c.updated); err != nil {
			t.Fatal(err)
		}
		out[title] = c
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func editedOnDay(t *testing.T, s *core.Store, day string) (pages, edited int) {
	t.Helper()
	d, err := s.Day(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range d.Rows {
		if strings.HasPrefix(r.What, "page") {
			pages++
		}
		if strings.Contains(r.What, "(edited)") {
			edited++
		}
	}
	return
}

func TestImportedPagesAreNotEdited(t *testing.T) {
	const day = "2031-05-01"
	// the notes that have no day of their own take one, so that the day view lists them
	bodied := []string{"Recipes", "Bob Sample", "Ferritin"}
	f := setup(t)
	f.approveRules(t, rulesBody)
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	for _, note := range []string{"Recipes.md", "Contacts/Bob Sample.md", "Medical/Ferritin.md"} {
		if _, err := f.w.FixPlan(ctx, f.s, note, "", day); err != nil {
			t.Fatal(err)
		}
	}
	res, err := f.w.ApplyVault(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 6 || res.Saved != 6 {
		t.Fatalf("first apply: %+v, want 6 pages created with 6 bodies", res)
	}

	check := func(label string, s *core.Store) {
		t.Helper()
		clocks := importedPageClocks(t, s)
		if len(clocks) != 6 {
			t.Fatalf("%s: %d imported pages, want the 6 notes", label, len(clocks))
		}
		for title, c := range clocks {
			if c.revision != 1 || c.updated != c.createdAt {
				t.Errorf("%s: %s is at revision %d, created %s, updated %s; want revision 1 and updated_at = created_at", label, title, c.revision, c.createdAt, c.updated)
			}
		}
		if pages, edited := editedOnDay(t, s, day); pages != len(bodied) || edited != 0 {
			t.Errorf("%s: the day view lists %d pages of %s, %d of them edited; want %d and none", label, pages, day, edited, len(bodied))
		}
	}
	check("after the first apply", f.s)

	// the bodies are the notes' texts, links included
	id, err := f.s.PageID(ctx, "2031-04-11")
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Body, "[[Bob Sample|Bob]]") || linkTitles(p.Out, "wikilink") != "Bob Sample,Recipes" {
		t.Errorf("the page of the 11th: body %q, wikilinks %q", p.Body, linkTitles(p.Out, "wikilink"))
	}

	// a repeated apply writes nothing: no body, no revision
	if res, err = f.w.ApplyVault(ctx, f.s); err != nil || res.Created != 0 || res.Saved != 0 || res.Same != 6 {
		t.Fatalf("second apply: %+v, %v", res, err)
	}
	check("after the second apply", f.s)

	// a replay into a fresh file creates the same pages the same way
	target := filepath.Join(t.TempDir(), "life.db")
	if _, err := f.w.Replay(ctx, f.s, target); err != nil {
		t.Fatalf("replay: %v", err)
	}
	d, err := db.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	check("in the replay target", &core.Store{DB: d})

	// the control: a later change of the body is an edit, and the day view says so
	page, err := f.s.PageByID(ctx, mustPageID(t, f.s, "Recipes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SaveBody(ctx, "cli", page.ID, page.Body+"\nMore flour.", page.Version); err != nil {
		t.Fatal(err)
	}
	if c := importedPageClocks(t, f.s)["Recipes"]; c.revision != 2 {
		t.Errorf("edited page: revision %d, want 2", c.revision)
	}
	if _, edited := editedOnDay(t, f.s, day); edited != 1 {
		t.Errorf("the day view flags %d pages after one edit, want 1", edited)
	}
}

// linkTitles are the sorted, comma-joined titles at the far end of a page's edges of one kind.
func linkTitles(es []core.Edge, kind string) string {
	var out []string
	for _, e := range es {
		if e.Kind == kind {
			out = append(out, e.Title)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func mustPageID(t *testing.T, s *core.Store, title string) int64 {
	t.Helper()
	id, err := s.PageID(ctx, title)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// A note changed in the vault is applied again as an edit of its page: the body is saved, the links follow it, the
// revision advances once.
func TestAChangedNoteEditsItsImportedPage(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	before := importedPageClocks(t, f.s)
	addSourceFile(t, f, "Recipes.md", "## Bread\n\nflour, water, salt. See [[Contacts/Bob Sample|Bob]]\n")
	res, err := f.w.ApplyVault(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 0 || res.Saved != 1 || res.Same != 5 {
		t.Errorf("apply after one note changed: %+v", res)
	}
	after := importedPageClocks(t, f.s)
	for title, c := range after {
		want := before[title].revision
		if title == "Recipes" {
			want++
		}
		if c.revision != want {
			t.Errorf("%s: revision %d, want %d", title, c.revision, want)
		}
	}
	p, err := f.s.PageByID(ctx, mustPageID(t, f.s, "Recipes"))
	if err != nil {
		t.Fatal(err)
	}
	if linkTitles(p.Out, "wikilink") != "Bob Sample" {
		t.Errorf("the links of the changed page: %q", linkTitles(p.Out, "wikilink"))
	}
}

// The pages and their text are committed before the links are synced, so a run can end between the two: pages that
// are whole, unlinked and at revision 1. The next run adds the links and rewrites no text. The interruption is made
// by deleting the wikilinks the first run synced (a link row is the one thing a writer may delete).
func TestAnInterruptedVaultPassIsCompletedByTheNextRun(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	page := func() *core.Page {
		t.Helper()
		p, err := f.s.PageByID(ctx, mustPageID(t, f.s, "2031-04-11"))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := page(); linkTitles(p.Out, "wikilink") != "Bob Sample,Recipes" {
		t.Fatalf("after the first run: wikilinks %q", linkTitles(p.Out, "wikilink"))
	}
	if _, err := f.s.DB.W.Exec(`DELETE FROM links WHERE kind = 'wikilink'`); err != nil {
		t.Fatal(err)
	}
	if p := page(); !strings.Contains(p.Body, "[[Bob Sample|Bob]]") || linkTitles(p.Out, "wikilink") != "" {
		t.Fatalf("the simulated interruption: body %q, wikilinks %q; want the text and no links", p.Body, linkTitles(p.Out, "wikilink"))
	}
	res, err := f.w.ApplyVault(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 0 || res.Saved != 0 {
		t.Errorf("the completing run: %+v, want no page created and no text saved", res)
	}
	if p := page(); linkTitles(p.Out, "wikilink") != "Bob Sample,Recipes" {
		t.Errorf("after the completing run: wikilinks %q", linkTitles(p.Out, "wikilink"))
	}
	for title, c := range importedPageClocks(t, f.s) {
		if c.revision != 1 {
			t.Errorf("%s is at revision %d after the completing run, want 1: the text was not rewritten", title, c.revision)
		}
	}
}
