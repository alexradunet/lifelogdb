package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSelectionSnapshotsForgeryNamespaceAndStrictChoices(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "daily.csv", "Date,Distance (m)\n2020-01-02,10\n")
	mustLedger(t, f)
	b, err := f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"})
	if err != nil {
		t.Fatal(err)
	}
	review, err := f.w.Review(preparedFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Other"}); err != nil {
		t.Fatal(err)
	}
	if err = f.w.Approve(preparedFile, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), review.Hash); err == nil {
		t.Fatal("approval hash race stamped unseen interpretation")
	}
	if _, err = f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, b.Metrics); err != nil {
		t.Fatal(err)
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	body, err := f.w.artifactBody(preparedFile, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.w.Dir, preparedFile), []byte("status: draft\n{}"), 0600); err != nil {
		t.Fatal(err)
	}
	parsed, err := f.w.parsePrepared(ctx, body)
	if err != nil || parsed.Records[0].Quantities[0].Value != "10" {
		t.Fatal("verified snapshot reread another artifact")
	}
	if _, err = f.w.readPrepared(ctx, true); err == nil {
		t.Fatal("replacement artifact borrowed prior gate")
	}
	f.approveRules(t, strings.Replace(rulesBody, "import:notebook", "import:different", 1))
	if _, err = f.w.parsePrepared(ctx, body); err == nil {
		t.Fatal("approved namespace change reattributed snapshot")
	}
	f.approveRules(t, rulesBody)
	if err = os.WriteFile(filepath.Join(f.w.Source, "one.jpg"), syntheticSelectedJPEG(t), 0600); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"capture":"none","gps":"none","approved":true}`, `{"capture":"none","capture":"owner","gps":"none"}`, `{"capture":"none","gps":"none","at":"\ud800"}`} {
		if _, err = f.w.DraftSelectedPhotoJSON(ctx, "one.jpg", "", "One.jpg", "", raw); err == nil {
			t.Fatal("unsupported/lossy choices drafted")
		}
	}
}
