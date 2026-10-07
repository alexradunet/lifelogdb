package importer

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"lifelog/internal/core"
)

func TestSelectedPhotoOffsetMustBeOffset(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	if err := os.WriteFile(filepath.Join(f.w.Source, "offset.jpg"), syntheticSelectedJPEG(t), 0600); err != nil {
		t.Fatal(err)
	}
	writeSource(t, f, "offset.json", `{"photoTakenTime":{"timestamp":"1577920200"}}`)
	mustLedger(t, f)
	for _, offset := range []string{"", "+00:00", "+02:00", "+23:59", "-23:59"} {
		if _, err := f.w.DraftSelectedPhoto(ctx, "offset.jpg", "offset.json", "Offset.jpg", "", PhotoChoices{Capture: "sidecar", GPS: "none", Offset: offset}); err != nil {
			t.Fatalf("valid offset %q refused: %v", offset, err)
		}
	}
	for _, offset := range []string{".000", ".1", ".000Z", "+00:60", "-00:00"} {
		t.Run(offset, func(t *testing.T) {
			before, err := os.ReadFile(filepath.Join(f.w.Dir, selectedPhotoFile))
			if err != nil {
				t.Fatal(err)
			}
			var callErr error
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				_, callErr = f.w.DraftSelectedPhoto(ctx, "offset.jpg", "offset.json", "Offset.jpg", "", PhotoChoices{Capture: "sidecar", GPS: "none", Offset: offset})
			}()
			if panicked != nil {
				t.Errorf("offset %q panicked instead of refusing: %v", offset, panicked)
			} else if callErr == nil {
				t.Errorf("accepted %q as an offset; want ordinary validation refusal", offset)
			}
			after, err := os.ReadFile(filepath.Join(f.w.Dir, selectedPhotoFile))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("invalid offset changed the prior draft")
			}
		})
	}
}

func TestPreparedAndPhotoRespectLedgerAdmission(t *testing.T) {
	for _, kind := range []string{"prepared", "photo"} {
		for _, state := range []string{"skipped", "absent"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				f := setup(t)
				f.approveRules(t, rulesBody)
				if state == "absent" {
					mustLedger(t, f)
				}
				file := "admission.csv"
				artifact := preparedFile
				if kind == "prepared" {
					writeSource(t, f, file, "Date,Distance (m)\n2020-01-02,10.5\n")
					if _, err := f.s.RegisterMetric(ctx, "cli", "Distance", "m", "Source distance"); err != nil {
						t.Fatal(err)
					}
					if _, err := f.w.DraftPrepared(ctx, file, "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); err != nil {
						t.Fatal(err)
					}
				} else {
					file, artifact = "admission.jpg", selectedPhotoFile
					if err := os.WriteFile(filepath.Join(f.w.Source, file), syntheticSelectedJPEG(t), 0600); err != nil {
						t.Fatal(err)
					}
					if _, err := f.w.DraftSelectedPhoto(ctx, file, "", "Admission.jpg", "Synthetic caption", PhotoChoices{Capture: "none", GPS: "none"}); err != nil {
						t.Fatal(err)
					}
				}
				if state == "skipped" {
					mustLedger(t, f)
					if err := f.w.Skip(file, "not selected for application"); err != nil {
						t.Fatal(err)
					}
				}
				if err := ownerApproves(f.w, artifact); err != nil {
					t.Fatal(err)
				}
				before, err := f.s.Counts(ctx)
				if err != nil {
					t.Fatal(err)
				}
				ledger, ok, err := f.w.Ledger()
				if err != nil || !ok {
					t.Fatalf("ledger setup: %v %v", ok, err)
				}
				var callErr error
				if kind == "prepared" {
					_, callErr = f.w.Prepared(ctx, f.s, false)
				} else {
					_, callErr = f.w.SelectedPhoto(ctx, f.s, false)
				}
				if callErr == nil {
					t.Error("non-admitted source applied without refusal")
				}
				after, err := f.s.Counts(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Errorf("non-admitted source changed persisted state: before=%+v after=%+v err=%v", before, after, callErr)
				}
				later, _, err := f.w.Ledger()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(ledger, later) {
					t.Error("non-admitted source changed the ledger")
				}
			})
		}
	}
}

func TestSelectedPhotoPromotionPreservesConflictingAttribution(t *testing.T) {
	for _, day := range []string{"2012-03-04", "2020-01-02"} {
		t.Run(day, func(t *testing.T) {
			f := setup(t)
			f.approveRules(t, rulesBody)
			if err := os.WriteFile(filepath.Join(f.w.Source, "promotion.jpg"), syntheticSelectedJPEG(t), 0600); err != nil {
				t.Fatal(err)
			}
			var id int64
			if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error {
				var e error
				id, _, _, e = tx.CreatePage("Promotion.jpg", "Retained owner text", "2012-03-04", "")
				return e
			}); err != nil {
				t.Fatal(err)
			}
			mustLedger(t, f)
			// Leave Body empty so the existing text-conflict guard cannot mask day handling.
			if _, err := f.w.DraftSelectedPhoto(ctx, "promotion.jpg", "", "Promotion.jpg", "", PhotoChoices{Capture: "owner", Day: day, GPS: "none"}); err != nil {
				t.Fatal(err)
			}
			if err := ownerApproves(f.w, selectedPhotoFile); err != nil {
				t.Fatal(err)
			}
			before, err := f.s.PageByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			counts, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			kept, callErr := f.w.SelectedPhoto(ctx, f.s, false)
			after, err := f.s.PageByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if day == "2012-03-04" {
				if callErr != nil || !kept.Promoted || after.Day != day || after.Body != before.Body {
					t.Fatalf("matching-day promotion control: kept=%+v page=%+v err=%v", kept, after, callErr)
				}
				return
			}
			if callErr == nil {
				t.Errorf("conflicting promotion applied: %+v", kept)
			}
			later, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(counts, later) {
				t.Errorf("conflicting attribution changed owner entity or relationships: prior=%+v current=%+v counts=%+v => %+v", before, after, counts, later)
			}
		})
	}
}
