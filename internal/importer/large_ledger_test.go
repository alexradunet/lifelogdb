package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A ledger grows with the source (one line per file) and must never be bounded like a prepared artifact: the
// evidence snapshot of a workspace hashes it as a stream (issue 0049).
func TestSelectionSnapshotHashesALargeLedger(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	mustLedger(t, f)
	before, err := f.w.selectionSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// a direct append of skipped lines: bulk setup of a 5 MiB ledger, far above the 4 MiB prepared-artifact bound
	var b strings.Builder
	for i := 0; b.Len() < 5<<20; i++ {
		b.WriteString("- [-] Photos/IMG_")
		b.WriteString(strings.Repeat("0", 8-len(itoa(i)))) // zero-padded, as a camera names them
		b.WriteString(itoa(i))
		b.WriteString(".jpg — skip: a photo library, selected later\n")
	}
	ledger := filepath.Join(f.w.Dir, "ledger.md")
	fh, err := os.OpenFile(ledger, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.WriteString(b.String()); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := f.w.selectionSnapshot(ctx)
	if err != nil {
		t.Fatalf("a large ledger is evidence like any other: %v", err)
	}
	if after == before {
		t.Error("the snapshot does not see the ledger's change")
	}
	lines, _, err := f.w.Ledger()
	if err != nil || len(lines) < 50000 {
		t.Fatalf("the ledger still reads whole: %d lines, %v", len(lines), err)
	}
	// the prepared artifact keeps its bound
	if err := os.WriteFile(filepath.Join(f.w.Dir, preparedFile), []byte("status: draft\n"+strings.Repeat("x", 4<<20+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.selectionSnapshot(ctx); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Errorf("an oversized prepared artifact is still refused: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
