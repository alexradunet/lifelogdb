package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// the guide's "rules.md" and "ledger.md": a `## Folders` line whose words start with `skip:` or `later:` marks the
// files *ledger* adds, once the owner has approved the rules (issue 0054)

const markingRules = "source: import:notebook\n\n## Folders\n" +
	"- `Journal/*.md` — one note per day\n" +
	"- `Medical/Ferritin.md` — readings: one metric, values from its table\n" +
	"- `Medical/**` — skip: results kept elsewhere\n" +
	"- `**/*.png`, `**/*.canvas` — later: an attachment, kept with its text\n"

func TestLedgerMarksWhatTheApprovedRulesSkipOrHold(t *testing.T) {
	f := setup(t)
	f.approveRules(t, markingRules)
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	lines, _, err := f.w.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"Journal/2031-04-11.md":  {" ", ""},
		"Medical/Ferritin.md":    {" ", ""}, // the first matching line decides: readings, not the skip below it
		"Medical/Twice.md":       {"-", "skip: results kept elsewhere"},
		"photo.png":              {">", "later: an attachment, kept with its text"},
		"Notes/A — B.png":        {">", "later: an attachment, kept with its text"},
		"Contacts/Bob Sample.md": {" ", ""}, // no line matches: the model asks
	}
	for file, w := range want {
		if ledgerState(lines, file) != w[0] || ledgerNote(lines, file) != w[1] {
			t.Errorf("%s: [%s] %q, want [%s] %q", file, ledgerState(lines, file), ledgerNote(lines, file), w[0], w[1])
		}
	}

	// a file added later is marked by the same rules; a line written before stays as it is
	if err := os.WriteFile(filepath.Join(f.w.Source, "Medical", "New.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.w.Source, "Board.canvas"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Defer("Contacts/Bob Sample.md", "a later pass"); err != nil {
		t.Fatal(err)
	}
	r, err := f.w.RefreshLedger()
	if err != nil || r.Added != 2 || r.Skipped != 1 || r.Later != 1 {
		t.Fatalf("refresh: %+v, %v", r, err)
	}
	lines, _, _ = f.w.Ledger()
	if ledgerState(lines, "Medical/New.md") != "-" || ledgerState(lines, "Board.canvas") != ">" || ledgerNote(lines, "Contacts/Bob Sample.md") != "later: a later pass" {
		t.Errorf("after the refresh: %+v", lines)
	}
}

func TestADraftRulesFileMarksNothing(t *testing.T) {
	f := setup(t)
	if err := f.w.DraftRules(markingRules); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	lines, _, _ := f.w.Ledger()
	for _, l := range lines {
		if l.State != " " {
			t.Errorf("a draft rule marked %s [%s] %s", l.File, l.State, l.Note)
		}
	}
}

func TestFolderLineForms(t *testing.T) {
	for _, c := range []struct {
		line, mark, note string
		patterns         []string
	}{
		{"- `Journal/*.md` — one note per day", "", "", []string{"Journal/*.md"}},
		{"- `**/*.png`, `**/*.pdf` — skip: kept as files", "-", "skip: kept as files", []string{"**/*.png", "**/*.pdf"}},
		{"- `Notes/A — B.png` — later: a picture", ">", "later: a picture", []string{"Notes/A — B.png"}},
		{"- Drive/**, **/*.{png,pdf} — Skip: not a life log", "-", "skip: not a life log", []string{"Drive/**", "**/*.{png,pdf}"}},
		{"- Keep/** - later:  convert first", ">", "later: convert first", []string{"Keep/**"}},
		{"- Fit/** — readings, skip: the sessions", "", "", []string{"Fit/**"}},
	} {
		r, ok := parseFolderLine(c.line)
		if !ok || r.mark != c.mark || r.note != c.note || strings.Join(r.patterns, "|") != strings.Join(c.patterns, "|") {
			t.Errorf("%q: %+v, %v", c.line, r, ok)
		}
	}
	if _, ok := parseFolderLine("Every file falls under one line."); ok {
		t.Error("a sentence is a folder line")
	}
}
