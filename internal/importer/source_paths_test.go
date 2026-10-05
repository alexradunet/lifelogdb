package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/text/unicode/norm"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func writeSourceFile(t *testing.T, src, slashPath, body string) string {
	t.Helper()
	p := filepath.Join(src, filepath.FromSlash(slashPath))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func setupSourcePathWorkspace(t *testing.T, src string) (*Workspace, *core.Store) {
	t.Helper()
	w, err := Open(src + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Setup(""); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(w.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return w, &core.Store{DB: d}
}

func TestSourceFilenameIdentity(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Notebook")
	bodyNFD := "Cafe\u0301 body names [[Other]].\n"
	physicalNote := writeSourceFile(t, src, "Journe\u0301e/Cafe\u0301.md", bodyNFD)
	writeSourceFile(t, src, "Aliase\u0301/Only.md", "unique alias\n")
	composedSibling := writeSourceFile(t, src, "Sib\u00e9/Different.md", "exact directory leaf\n")
	equivalentSibling := filepath.Join(src, filepath.FromSlash("Sibe\u0301/Other.md"))
	if err := os.MkdirAll(filepath.Dir(equivalentSibling), 0o755); err != nil {
		t.Fatal(err)
	}
	siblingDirsByteDistinct := true
	if err := os.WriteFile(equivalentSibling, []byte("equivalent directory leaf\n"), 0o644); err != nil {
		if errors.Is(err, os.ErrExist) {
			siblingDirsByteDistinct = false
		} else {
			t.Fatal(err)
		}
	}
	if same(filepath.Dir(composedSibling), filepath.Dir(equivalentSibling)) {
		siblingDirsByteDistinct = false
		t.Log("filesystem treats canonically equivalent sibling directories as the same path; byte-distinct sibling case skipped")
	}

	w, s := setupSourcePathWorkspace(t, src)
	files, err := w.SourceFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Alias\u00e9/Only.md", "Journ\u00e9e/Caf\u00e9.md"} {
		if !contains(files, want) {
			t.Fatalf("inventory paths = %q, missing %q", files, want)
		}
	}
	text, err := w.ReadSource("Journ\u00e9e/Caf\u00e9.md")
	if err != nil {
		t.Fatal(err)
	}
	if text != norm.NFC.String(bodyNFD) {
		t.Fatalf("ReadSource normalisation = %q, want %q", text, norm.NFC.String(bodyNFD))
	}
	if text, err := w.ReadSource("Alias\u00e9/Only.md"); err != nil || text != "unique alias\n" {
		t.Fatalf("unique normalized alias read = %q, %v", text, err)
	}
	if siblingDirsByteDistinct {
		text, err := w.ReadSource("Sib\u00e9/Other.md")
		if err != nil || text != "equivalent directory leaf\n" {
			t.Fatalf("equivalent sibling leaf read = %q, %v", text, err)
		}
	}
	if b, err := os.ReadFile(physicalNote); err != nil || string(b) != bodyNFD {
		t.Fatalf("source bytes changed: %q, %v", b, err)
	}
	entries, err := os.ReadDir(filepath.Dir(physicalNote))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Cafe\u0301.md" {
		t.Fatalf("source filename changed: %q", entries)
	}

	f := &fixture{w: w, s: s, trial: w.TrialDB()}
	f.approveRules(t, rulesBody)
	if _, err := w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	facts := map[string]any{"file": "Journ\u00e9e/Caf\u00e9.md", "writes": []any{map[string]any{"page": map[string]any{"title": "Caf\u00e9"}, "quote": "Caf\u00e9 body"}}}
	if err := f.facts(t, "Journ\u00e9e/Caf\u00e9.md", facts); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Check(context.Background(), s, "Journ\u00e9e/Caf\u00e9.md"); err != nil {
		t.Fatal(err)
	}
	p, err := w.PlanVault(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range p.Notes {
		if n.Path == "Journ\u00e9e/Caf\u00e9.md" && n.Title == "Caf\u00e9" {
			found = true
		}
	}
	if !found {
		t.Fatalf("vault plan did not preserve logical note identity: %+v", p.Notes)
	}
	if _, err := w.ApplyVault(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	id, _ := s.PageID(context.Background(), "Caf\u00e9")
	page, _ := s.PageByID(context.Background(), id)
	if page.Body != norm.NFC.String(bodyNFD) {
		t.Fatalf("vault body = %q, want %q", page.Body, norm.NFC.String(bodyNFD))
	}
	if res, err := w.ApplyVault(context.Background(), s); err != nil || res.Created != 0 || res.Saved != 0 || res.Appended != 0 {
		t.Fatalf("repeat apply = %+v, %v", res, err)
	}

	target := filepath.Join(t.TempDir(), "life.db")
	replay, err := w.Replay(context.Background(), s, target)
	if err != nil || len(replay.Failures) != 0 || replay.Vault.Created == 0 {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	targetDB, err := db.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	targetStore := &core.Store{DB: targetDB}
	defer targetDB.Close()
	counts, err := targetStore.Counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var firstKey int64
	if err := targetStore.Do(context.Background(), "import:notebook", func(tx *core.Tx) error {
		var err error
		firstKey, err = tx.ByImportKey("Journ\u00e9e/Caf\u00e9.md")
		return err
	}); err != nil || firstKey == 0 {
		t.Fatalf("replayed import key = %d, %v", firstKey, err)
	}
	replay, err = w.Replay(context.Background(), s, target)
	if err != nil || replay.Vault.Created != 0 || replay.Vault.Saved != 0 || replay.Vault.Appended != 0 {
		t.Fatalf("repeat replay = %+v, %v", replay, err)
	}
	again, err := targetStore.Counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var secondKey int64
	if err := targetStore.Do(context.Background(), "import:notebook", func(tx *core.Tx) error {
		var err error
		secondKey, err = tx.ByImportKey("Journ\u00e9e/Caf\u00e9.md")
		return err
	}); err != nil || secondKey != firstKey || !reflect.DeepEqual(again, counts) {
		t.Fatalf("repeat replay changed key/counts: key %d->%d counts %+v->%+v err %v", firstKey, secondKey, counts, again, err)
	}

	missingTarget := filepath.Join(t.TempDir(), "life.db")
	if err := os.Rename(physicalNote, physicalNote+".gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Replay(context.Background(), s, missingTarget); err == nil || !strings.Contains(err.Error(), "no source file Journ\u00e9e/Caf\u00e9.md") {
		t.Fatalf("replay after source disappearance = %v", err)
	}
	if exists(missingTarget) {
		t.Fatal("replay with a missing source file created the target")
	}
}

func TestSourcePathMissingRecognizesTypedENOTDIR(t *testing.T) {
	missing := fmt.Errorf("wrapped: %w", &os.PathError{Op: "stat", Path: "source/file/child", Err: syscall.ENOTDIR})
	if !sourcePathMissing(missing) {
		t.Fatal("typed ENOTDIR must be treated as a missing exact path so equivalent directories can be tried")
	}
	access := fmt.Errorf("wrapped: %w", &os.PathError{Op: "stat", Path: "source/file", Err: syscall.EACCES})
	if sourcePathMissing(access) {
		t.Fatal("access errors must not be treated as missing paths")
	}
}

func TestSourcePathSkipsIncompleteCandidates(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Notebook")
	blockingFile := writeSourceFile(t, src, "Sib\u00e9", "not a directory\n")
	nfdDir := filepath.Join(src, "Sibe\u0301")
	if err := os.MkdirAll(nfdDir, 0o755); err != nil {
		t.Log("filesystem does not expose a file and canonically equivalent directory as distinct paths; incomplete-candidate case skipped")
		return
	}
	if same(blockingFile, nfdDir) {
		t.Log("filesystem does not expose a file and canonically equivalent directory as distinct paths; incomplete-candidate case skipped")
		return
	}
	if err := os.WriteFile(filepath.Join(nfdDir, "Only.md"), []byte("only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, _ := setupSourcePathWorkspace(t, src)
	got, err := w.ReadSource("Sib\u00e9/Only.md")
	if err != nil || got != "only\n" {
		t.Fatalf("resolver did not skip incomplete file candidate: %q, %v", got, err)
	}
}

func TestSourcePathExactFullPathWins(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Notebook")
	exact := writeSourceFile(t, src, "Exact\u00c5/Same.md", "exact\n")
	equiv := filepath.Join(src, filepath.FromSlash("ExactA\u030a/Same.md"))
	if err := os.MkdirAll(filepath.Dir(equiv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(equiv, []byte("equivalent\n"), 0o644); err != nil || same(exact, equiv) {
		t.Log("filesystem does not expose byte-distinct canonically equivalent full paths; exact-wins collision case skipped")
		return
	}
	w, _ := setupSourcePathWorkspace(t, src)
	got, err := w.SourcePath("Exact\u00c5/Same.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != exact {
		t.Fatalf("SourcePath chose %q, want exact physical path %q", got, exact)
	}
}

func TestSourcePathAmbiguousEquivalentRefuses(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Notebook")
	first := writeSourceFile(t, src, "AmbA\u030a/Same.md", "first\n")
	second := filepath.Join(src, filepath.FromSlash("Amb\u212b/Same.md"))
	if err := os.MkdirAll(filepath.Dir(second), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second\n"), 0o644); err != nil || same(first, second) {
		t.Log("filesystem treats canonically equivalent ambiguous paths as one path; ambiguity case skipped")
		return
	}
	w, s := setupSourcePathWorkspace(t, src)
	if _, err := w.ReadSource("Amb\u00c5/Same.md"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous fallback read error = %v", err)
	}
	if _, err := w.MakeLedger(); err == nil || !strings.Contains(err.Error(), "same logical source path") {
		t.Fatalf("MakeLedger collision error = %v", err)
	}
	if _, err := w.PlanVault(context.Background(), s); err == nil || !strings.Contains(err.Error(), "same logical source path") {
		t.Fatalf("PlanVault collision error = %v", err)
	}
}
