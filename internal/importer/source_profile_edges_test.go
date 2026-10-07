package importer

import (
	"strconv"
	"strings"
	"testing"
)

func TestSourceProfileRejectsQuantityUnderflow(t *testing.T) {
	// Positive 10^-324 cannot be represented as a nonzero binary64 quantity.
	// A missing value and a literal zero are different admitted source claims.
	tiny := "0." + strings.Repeat("0", 323) + "1"
	if n, err := strconv.ParseFloat(tiny, 64); err != nil || n != 0 {
		t.Fatalf("underflow setup: n=%g err=%v", n, err)
	}
	for _, value := range []string{"0", "0.0001"} {
		got, err := decodeSourceProfile("fit-date-csv-v1", []byte("Date,Distance (m)\n2020-01-02,"+value+"\n"), nil)
		if err != nil || len(got.Records) != 1 || len(got.Records[0].Quantities) != 1 || got.Records[0].Quantities[0].Value != value {
			t.Fatalf("valid quantity control: got=%+v err=%v", got, err)
		}
	}
	if got, err := decodeSourceProfile("fit-date-csv-v1", []byte("Date,Distance (m)\n2020-01-02,"+tiny+"\n"), nil); err == nil {
		t.Fatalf("accepted positive source quantity that binary64 turns into zero: records=%+v", got.Records)
	}
}

func TestSourceActivityBindingIsLossless(t *testing.T) {
	binding := &FitSessionBinding{Key: "reviewed-1", Day: "2020-01-02", Activity: "run\ufffd"}
	valid := []byte(`{"fitnessActivity":"run\ufffd","startTime":"2020-01-02T12:00:00Z"}`)
	got, err := decodeSourceProfile("fit-session-object-v1", valid, binding)
	if err != nil || len(got.Records) != 1 || got.Records[0].Activity != binding.Activity {
		t.Fatalf("literal replacement-character control: got=%+v err=%v", got, err)
	}
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"invalid UTF-8", append(append([]byte(`{"fitnessActivity":"run`), 0xff), []byte(`","startTime":"2020-01-02T12:00:00Z"}`)...)},
		{"unpaired surrogate", []byte(`{"fitnessActivity":"run\ud800","startTime":"2020-01-02T12:00:00Z"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := decodeSourceProfile("fit-session-object-v1", tc.raw, binding); err == nil {
				t.Fatalf("accepted lossy activity-identifier decoding as a match to an owner binding: got=%q", got.Records[0].Activity)
			}
		})
	}
}
