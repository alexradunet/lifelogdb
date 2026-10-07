package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSupportedPreparationUsesConfinedExactSnapshotWithoutWrites(t *testing.T) {
	f := setup(t)
	source := []byte("Date,Step count\n2020-01-02,10000\n")
	path := filepath.Join(f.w.Source, "synthetic.csv")
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.w.PrepareSupportedSource(ctx, "synthetic.csv", "fit-date-csv-v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(source)
	if result.SourceSHA256 != hex.EncodeToString(expected[:]) || result.Version != 1 || result.Records[0].Quantities[0].Value != "10000" {
		t.Fatalf("snapshot %+v", result)
	}
	after, err := f.s.Counts(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("preparation writes %+v %v", after, err)
	}
	if _, err := f.w.PrepareSupportedSource(ctx, "../outside.csv", "fit-date-csv-v1", nil); err == nil {
		t.Fatal("source escaped root")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.w.PrepareSupportedSource(canceled, "synthetic.csv", "fit-date-csv-v1", nil); err != context.Canceled {
		t.Fatalf("cancellation %v", err)
	}
	if err := os.WriteFile(path, bytes.ReplaceAll(source, []byte{10}, []byte{13, 10}), 0600); err != nil {
		t.Fatal(err)
	}
	// A source change is visible through its exact fingerprint even if all normalized quantities remain identical.
	updated, err := f.w.PrepareSupportedSource(ctx, "synthetic.csv", "fit-date-csv-v1", nil)
	if err != nil || updated.SourceSHA256 == result.SourceSHA256 {
		t.Fatal("changed source fingerprint ignored")
	}
}
