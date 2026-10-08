package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// the ledger as an index: files added later are appended, a pattern marks many files, a later pass holds files
// without blocking the notes pass (the guide's "ledger.md", issues 0023 and 0024)

func (f *fixture) ledgerState(t *testing.T, file string) Line {
	t.Helper()
	lines, _, err := f.w.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if l.File == file {
			return l
		}
	}
	t.Fatalf("%s is not in the ledger", file)
	return Line{}
}

func (f *fixture) addSource(t *testing.T, rel, body string) {
	t.Helper()
	full := filepath.Join(f.w.Source, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLedgerRefreshAppendsAddedFiles(t *testing.T) {
	f := setup(t)
	first, err := f.w.RefreshLedger()
	if err != nil || first.Files != 8 || first.Added != 8 || len(first.Missing) != 0 {
		t.Fatalf("first ledger: %+v, %v", first, err)
	}
	if err := f.w.Skip("photo.png", "attachment"); err != nil {
		t.Fatal(err)
	}
	f.addSource(t, "Journal/2031-04-13.md", "A new day.\n")
	f.addSource(t, "Aaa.md", "sorts first, lands last\n")
	if err := os.Remove(filepath.Join(f.w.Source, "Recipes.md")); err != nil {
		t.Fatal(err)
	}
	again, err := f.w.RefreshLedger()
	if err != nil {
		t.Fatal(err)
	}
	if again.Files != 10 || again.Added != 2 || len(again.Missing) != 1 || again.Missing[0] != "Recipes.md" {
		t.Errorf("refresh: %+v", again)
	}
	lines, _, _ := f.w.Ledger()
	var order []string
	for _, l := range lines {
		order = append(order, l.File)
	}
	// the old lines keep their place (the replay's order) and their marks; the new ones follow, in path order
	want := []string{"Contacts/Bob Sample.md", "Journal/2031-04-11.md", "Journal/2031-04-12.md", "Medical/Ferritin.md", "Medical/Twice.md", "Notes/A — B.png", "Recipes.md", "photo.png", "Aaa.md", "Journal/2031-04-13.md"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Errorf("order:\n got %v\nwant %v", order, want)
	}
	if l := f.ledgerState(t, "photo.png"); l.State != "-" || l.Note != "skip: attachment" {
		t.Errorf("a mark was lost by the refresh: %+v", l)
	}
	if l := f.ledgerState(t, "Journal/2031-04-13.md"); l.State != " " {
		t.Errorf("an added file is to do: %+v", l)
	}
	third, err := f.w.RefreshLedger()
	if err != nil || third.Added != 0 || third.Files != 10 {
		t.Errorf("a third run adds nothing: %+v, %v", third, err)
	}
}

func TestMarkFilesByPattern(t *testing.T) {
	f := setup(t)
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	f.approveRules(t, rulesBody)
	f.metrics(t)
	if err := f.facts(t, "Journal/2031-04-11.md", dayFacts); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, "Journal/2031-04-11.md"); err != nil {
		t.Fatal(err)
	}
	// a pattern across folders marks every file still to do and leaves a done file alone
	n, err := f.w.MarkFiles("**/*.png", ">", "an attachment: kept with its text in the later pass")
	if err != nil || n != 2 {
		t.Fatalf("defer **/*.png: %d, %v", n, err)
	}
	for _, p := range []string{"photo.png", "Notes/A — B.png"} {
		if l := f.ledgerState(t, p); l.State != ">" || !strings.HasPrefix(l.Note, "later: an attachment") {
			t.Errorf("%s: %+v", p, l)
		}
	}
	n, err = f.w.MarkFiles("Journal/*.md", "-", "already done or not")
	if err != nil || n != 1 {
		t.Errorf("skip Journal/*.md marks only the file still to do: %d, %v", n, err)
	}
	if l := f.ledgerState(t, "Journal/2031-04-11.md"); l.State != "x" {
		t.Errorf("a done file was re-marked: %+v", l)
	}
	if l := f.ledgerState(t, "Journal/2031-04-12.md"); l.State != "-" {
		t.Errorf("the to-do file was not skipped: %+v", l)
	}
	// a later file can still be skipped by a pattern; a pattern matching no ledger file is refused; a literal
	// path keeps the old refusals
	if n, err := f.w.MarkFiles("photo.*", "-", "not kept after all"); err != nil || n != 1 {
		t.Errorf("skip a later file: %d, %v", n, err)
	}
	if _, err := f.w.MarkFiles("Nowhere/**", "-", "typo"); code(err) != 422 {
		t.Errorf("a pattern matching nothing: %v", err)
	}
	if n, err := f.w.MarkFiles("Medical/*.md", "-", "no match left"); err != nil || n != 0 {
		// both Medical files are still to do: they are marked; run again, nothing is left to mark
		if n2, err2 := f.w.MarkFiles("Medical/*.md", "-", "again"); err2 != nil || n2 != 0 {
			t.Errorf("a pattern whose files are all marked: %d, %v", n2, err2)
		}
	}
	if err := f.w.Skip("missing.md", "x"); code(err) != 422 {
		t.Errorf("a literal file not in the ledger: %v", err)
	}
	if err := f.w.Defer("Journal/2031-04-11.md", "x"); code(err) != 422 {
		t.Errorf("a done file cannot be deferred: %v", err)
	}
	if _, err := f.w.MarkFiles("*.md", "?", "x"); code(err) != 422 {
		t.Errorf("only - and > are the model's marks: %v", err)
	}
	if _, err := f.w.MarkFiles("*.md", "-", "  "); code(err) != 422 {
		t.Errorf("a mark needs a reason: %v", err)
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.png", "photo.png", true},
		{"**/*.png", "a/b/c.png", true},
		{"**/*.png", "a/b/c.PNG", false},
		{"Journal/**/*.md", "Journal/2031/03-March/2031-03-01.md", true},
		{"Journal/**/*.md", "Journal/2031-03-01.md", true},
		{"Journal/**/*.md", "Contacts/x.md", false},
		{"Drive/**", "Drive/a/b/c", true},
		{"Drive/**", "Drive", false},
		{"Drive/*", "Drive/a/b", false},
		{"*.md", "a/b.md", false},
		{"Notes/A — B.png", "Notes/A — B.png", true},
		{"[", "[", false}, // a bad pattern matches nothing
		{"**/*.{png,pdf}", "a/b.pdf", true},
		{"**/*.{png,pdf}", "a/b.md", false},
		{"{Journal,Contacts}/*.md", "Contacts/x.md", true},
		{"{Journal,Contacts}/*.md", "Medical/x.md", false},
		{"a/{b,c}/{d,e}.md", "a/c/e.md", true},
		{"a/{b.md", "a/{b.md", true}, // an unclosed brace is literal
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestStatusOrdersPassesAndCountsNotes(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	// rules approved, no plan, no ledger: the source holds Markdown files, the rules say whether they are notes
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil {
		t.Fatal(err)
	}
	if st.Notes == nil || st.Notes.Markdown != 6 || st.Notes.Files != 8 {
		t.Errorf("notes count: %+v", st.Notes)
	}
	if !strings.Contains(st.DoNow, "6 Markdown files among 8") || !strings.Contains(st.DoNow, "plan-vault") || !strings.Contains(st.DoNow, "make-ledger") {
		t.Errorf("do now offers the choice: %s", st.DoNow)
	}
	// the model chose the ledger: the choice is not asked again
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	f.metrics(t)
	if err := f.w.Defer("**/*.png", "attachments"); err != nil {
		t.Fatal(err)
	}
	st, err = f.w.Status(ctx, f.s, f.trial)
	if err != nil {
		t.Fatal(err)
	}
	if st.Notes != nil || st.Ledger["later"] != 2 || st.Ledger["to_do"] != 6 || st.Next != "Contacts/Bob Sample.md" || st.Later != "Notes/A — B.png" {
		t.Errorf("status: notes %+v ledger %v next %q later %q", st.Notes, st.Ledger, st.Next, st.Later)
	}
	if !strings.HasPrefix(st.DoNow, "Do Contacts/Bob Sample.md") {
		t.Errorf("the notes pass comes first: %s", st.DoNow)
	}
	// every other file skipped: only the later pass is left, and it is named
	if _, err := f.w.MarkFiles("**/*.md", "-", "test"); err != nil {
		t.Fatal(err)
	}
	st, err = f.w.Status(ctx, f.s, f.trial)
	if err != nil {
		t.Fatal(err)
	}
	if st.Next != "" || !strings.Contains(st.DoNow, "later pass: 2 files") || !strings.Contains(st.DoNow, "Notes/A — B.png") {
		t.Errorf("the later pass: next %q, %s", st.Next, st.DoNow)
	}
	// a later file is a file still to do for the facts
	if err := f.facts(t, "photo.png", map[string]any{"file": "photo.png", "writes": []any{}}); err != nil {
		t.Errorf("facts for a later file: %v", err)
	}
	if _, err := f.w.Apply(ctx, f.s, "photo.png"); err != nil {
		t.Errorf("apply a later file: %v", err)
	}
	if l := f.ledgerState(t, "photo.png"); l.State != "x" {
		t.Errorf("applied: %+v", l)
	}
	st, _ = f.w.Status(ctx, f.s, f.trial)
	if st.Ledger["later"] != 1 || st.Later != "Notes/A — B.png" {
		t.Errorf("after the apply: %v %q", st.Ledger, st.Later)
	}
}

func TestStatusWithoutMarkdownSaysLedger(t *testing.T) {
	f := setup(t)
	for _, p := range []string{"Journal/2031-04-11.md", "Journal/2031-04-12.md", "Contacts/Bob Sample.md", "Medical/Ferritin.md", "Recipes.md", "Medical/Twice.md"} {
		if err := os.Remove(filepath.Join(f.w.Source, filepath.FromSlash(p))); err != nil {
			t.Fatal(err)
		}
	}
	f.approveRules(t, rulesBody)
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil {
		t.Fatal(err)
	}
	if st.Notes == nil || st.Notes.Markdown != 0 || !strings.HasPrefix(st.DoNow, "Make the ledger") {
		t.Errorf("no notes: %+v, %s", st.Notes, st.DoNow)
	}
}

func TestPlanVaultRefreshKeepsFixes(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	p, err := f.w.PlanVault(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	n := len(p.Notes)
	if _, err := f.w.FixPlan(ctx, f.s, "Recipes.md", "Bread Recipes", ""); err != nil {
		t.Fatal(err)
	}
	f.addSource(t, "Journal/2031-04-13.md", "A new day.\n")
	f.addSource(t, "Recipes (1).md", "a duplicate of a note\n")
	p, err = f.w.PlanVault(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Notes) != n+2 {
		t.Fatalf("notes: %d, want %d", len(p.Notes), n+2)
	}
	byPath := map[string]Note{}
	for _, note := range p.Notes {
		byPath[note.Path] = note
	}
	if byPath["Recipes.md"].Title != "Bread Recipes" {
		t.Errorf("a fixed title was lost: %+v", byPath["Recipes.md"])
	}
	if nn := byPath["Journal/2031-04-13.md"]; nn.Day != "2031-04-13" || nn.Title != "2031-04-13" || len(nn.Problems) != 0 {
		t.Errorf("the added daily note: %+v", nn)
	}
	if nn := byPath["Recipes (1).md"]; len(nn.Problems) != 0 {
		// "Recipes (1)" is a title of its own; it is validated like the rest
		t.Errorf("the added note: %+v", nn)
	}
	// the plan on disk is the refreshed one
	saved, ok, err := f.w.LoadPlan()
	if err != nil || !ok || len(saved.Notes) != n+2 {
		t.Errorf("saved plan: %d notes, %v %v", len(saved.Notes), ok, err)
	}
}

func TestLiteralMarkKeepsASkipFinal(t *testing.T) {
	f := setup(t)
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Skip("photo.png", "a view file"); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Skip("photo.png", "a better reason"); err != nil {
		t.Errorf("a skip may be re-stated: %v", err)
	}
	if err := f.w.Defer("photo.png", "kept after all"); code(err) != 422 {
		t.Errorf("a skipped file is not held for later by the model: %v", err)
	}
	if l := f.ledgerState(t, "photo.png"); l.State != "-" || l.Note != "skip: a better reason" {
		t.Errorf("%+v", l)
	}
}
