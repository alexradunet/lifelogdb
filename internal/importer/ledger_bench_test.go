package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// Scale of a workspace's ledger (issue 0054). Synthetic data only. Setup writes the ledger directly (bulk setup,
// labelled): its lines are the ones *ledger* and *apply facts* would write.
//
//	go test ./internal/importer -run '^$' -bench '^BenchmarkStatusLargeLedger$' -benchmem -count=5
//	go test ./internal/importer -run '^$' -bench '^BenchmarkSourceFiles$' -benchmem -count=5
const (
	benchLedgerLines = 20000 // lines of the ledger, most of them still to do
	benchDoneFiles   = 300   // done files, each with a facts file that status dry-runs
	benchTreeFiles   = 20000 // files of the source tree, 50 to a folder
)

func benchWorkspace(b *testing.B) (*Workspace, *core.Store, string) {
	b.Helper()
	src := filepath.Join(b.TempDir(), "Notes")
	if err := os.MkdirAll(filepath.Join(src, "done"), 0o755); err != nil {
		b.Fatal(err)
	}
	w, err := Open(src + ".lifelog")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := w.Setup(""); err != nil {
		b.Fatal(err)
	}
	if err := w.DraftRules("source: import:notes\n"); err != nil {
		b.Fatal(err)
	}
	if err := ownerApproves(w, "rules.md"); err != nil {
		b.Fatal(err)
	}
	lines := make([]Line, 0, benchLedgerLines)
	for i := range benchDoneFiles {
		file := fmt.Sprintf("done/n%05d.md", i)
		if err := os.WriteFile(filepath.Join(src, filepath.FromSlash(file)), []byte(fmt.Sprintf("Note %d.\n", i)), 0o644); err != nil {
			b.Fatal(err)
		}
		lines = append(lines, Line{" ", file, ""})
	}
	for i := len(lines); i < benchLedgerLines; i++ {
		lines = append(lines, Line{" ", fmt.Sprintf("todo/%03d/n%05d.md", i/100, i), ""})
	}
	if err := w.writeLedger(lines); err != nil {
		b.Fatal(err)
	}
	for i := range benchDoneFiles {
		file := lines[i].File
		if err := w.WriteFacts(file, []byte(fmt.Sprintf(`{"file":%q,"writes":[]}`, file))); err != nil {
			b.Fatal(err)
		}
		lines[i].State, lines[i].Note = "x", "nothing written"
	}
	if err := w.writeLedger(lines); err != nil {
		b.Fatal(err)
	}
	d, err := db.Open(w.TrialDB())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { d.Close() })
	return w, &core.Store{DB: d}, w.TrialDB()
}

// BenchmarkStatusLargeLedger is one *status* on a ledger of benchLedgerLines lines, benchDoneFiles of them done:
// every done file is dry-run, so the cost of reading the ledger once per file shows.
func BenchmarkStatusLargeLedger(b *testing.B) {
	w, s, trial := benchWorkspace(b)
	for b.Loop() {
		st, err := w.Status(ctx, s, trial)
		if err != nil {
			b.Fatal(err)
		}
		if len(st.Mismatches) != 0 || st.Ledger["done"] != benchDoneFiles {
			b.Fatalf("status: %d done, mismatches %v", st.Ledger["done"], st.Mismatches)
		}
	}
}

// BenchmarkSourceFiles lists a source tree of benchTreeFiles files: what *ledger* reads first.
func BenchmarkSourceFiles(b *testing.B) {
	src := filepath.Join(b.TempDir(), "Tree")
	for i := range benchTreeFiles {
		dir := filepath.Join(src, fmt.Sprintf("d%03d", i/50))
		if i%50 == 0 {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				b.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d.txt", i)), nil, 0o644); err != nil {
			b.Fatal(err)
		}
	}
	w, err := Open(src + ".lifelog")
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		files, err := w.SourceFiles()
		if err != nil {
			b.Fatal(err)
		}
		if len(files) != benchTreeFiles {
			b.Fatalf("%d files", len(files))
		}
	}
}
