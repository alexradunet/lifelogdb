package scaletest

import (
	"context"
	"os"
	"testing"
)

func TestSmallWorkloadKnownAnswersAndReproducibility(t *testing.T) {
	options := Options{Profile: "small", Seed: 2075, Samples: 1, Directory: t.TempDir()}
	first, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	m := first.Manifest
	// 14*4 roots, corrections at 0/17/34/51 and retractions at 0/31.
	// Twelve journal pages and eight notes each name the shared topic.
	if m.Roots != 56 || m.Corrections != 4 || m.Retractions != 2 || m.CurrentReadings != 54 || m.JournalPages != 12 || m.Files != 14 || m.PopularBacklinks != 20 || m.RareMatches != 2 || m.PreviewBytes != 0 {
		t.Fatalf("small known answers: %+v", m)
	}
	if _, err := os.Stat(first.Directory); !os.IsNotExist(err) {
		t.Fatalf("scratch not cleaned: %v", err)
	}
	second, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Manifest != second.Manifest {
		t.Fatalf("same seed changed logical fixture: %+v / %+v", first.Manifest, second.Manifest)
	}
	if len(first.Operations) != 10 {
		t.Fatalf("workflow measurements: %+v", first.Operations)
	}
}

func TestSmallPreviewWorkload(t *testing.T) {
	report, err := Run(context.Background(), Options{Profile: "small", Seed: 2075, Samples: 1, Previews: true, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if report.Manifest.PreviewBytes < 1<<20 || report.Manifest.Files != 14 {
		t.Fatalf("unrepresentative preview payloads: %+v", report.Manifest)
	}
	if report.Manifest.MediaMode == "metadata-only; no preview/storage realism claim" {
		t.Fatal("previews mislabeled")
	}
}
