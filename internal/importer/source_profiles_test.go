package importer

import (
	"strings"
	"testing"
)

func TestSupportedSourceProfilesRetainMeaningIdentityAndUncertainty(t *testing.T) {
	daily, err := decodeSourceProfile("fit-date-csv-v1", []byte("Date,Step count,Distance (m),Calories (kcal),Average heart rate (bpm)\n2020-01-02,10000,1250.5,,60\n"), nil)
	if err != nil || len(daily.Records) != 1 || len(daily.Records[0].Quantities) != 3 {
		t.Fatalf("daily %+v %v", daily, err)
	}
	if daily.Records[0].Quantities[2] != (SourceQuantity{"mean-heart-rate", "bpm", "60"}) {
		t.Fatal("mean statistic changed")
	}
	sleep := []byte(`[{"logId":9007199254740993,"dateOfSleep":"2020-01-02","startTime":"2020-01-01T23:00:00","endTime":"2020-01-02T07:00:00","minutesAsleep":420,"timeInBed":0,"levels":{}},{"logId":"0001","dateOfSleep":"2020-01-02","startTime":"2020-01-02T09:00:00+02:00"},{"logId":18446744073709551615,"dateOfSleep":"2020-01-03","startTime":"2020-01-03T09:00:00.1"}]`)
	parsed, err := decodeSourceProfile("legacy-sleep-array-v1", sleep, nil)
	if err != nil || len(parsed.Records) != 3 {
		t.Fatalf("sleep %+v %v", parsed, err)
	}
	first := parsed.Records[0]
	if first.Key != "sleep:9007199254740993" || first.Day != "2020-01-02" || first.StartLocal != "2020-01-01T23:00:00.000" || first.StartAt != "" || len(first.Quantities) != 2 || first.Quantities[1].Value != "0" {
		t.Fatalf("sleep meaning %+v", first)
	}
	if parsed.Records[1].Key != "sleep:0001" || parsed.Records[1].StartAt != "2020-01-02T07:00:00.000Z" || parsed.Records[2].Key != "sleep:18446744073709551615" || parsed.Records[2].StartLocal != "2020-01-03T09:00:00.100" || len(parsed.Excluded) == 0 {
		t.Fatalf("identity/offset/exclusions %+v", parsed)
	}
	fit, err := decodeSourceProfile("fit-session-object-v1", []byte(`{"fitnessActivity":"running","startTime":"2020-01-02T12:00:00Z","aggregate":[{"metricName":"unknown","intValue":4000}]}`), &FitSessionBinding{Key: "reviewed-1", Day: "2020-01-03", Activity: "running"})
	if err != nil || fit.Records[0].Key != "fit-bound:reviewed-1" || fit.Records[0].Day != "2020-01-03" || len(fit.Records[0].Quantities) != 0 || len(fit.Excluded) != 1 {
		t.Fatalf("bound Fit %+v %v", fit, err)
	}
}

func TestSourceProfileRefusalsAreBoundedAndPrivate(t *testing.T) {
	for _, tc := range []struct{ profile, body string }{
		{"fit-date-csv-v1", "Start time,End time,Step count\n2020-01-01,2020-01-02,10\n"},
		{"fit-date-csv-v1", "Date,Step count\n2020-01-02,1\n2020-01-02,2\n"},
		{"fit-date-csv-v1", "Date,Date\n2020-01-02,2020-01-02\n"},
		{"fit-date-csv-v1", "Date,Step count\n2020-01-02,9007199254740993\n"},
		{"legacy-sleep-array-v1", `{"sleep":[]}`},
		{"legacy-sleep-array-v1", `[{"logId":1e3,"dateOfSleep":"2020-01-02","startTime":"2020-01-02T00:00:00"}]`},
		{"legacy-sleep-array-v1", `[{"logId":1,"logId":2}]`},
		{"legacy-sleep-array-v1", `[{"logId":1,"dateOfSleep":"2020-01-02","startTime":"2020-01-02T00:00:00-00:00"}]`},
		{"legacy-sleep-array-v1", `[{"logId":1,"dateOfSleep":"2020-01-02","startTime":"2020-01-02T24:00:00"}]`},
		{"legacy-sleep-array-v1", `[{"logId":1,"dateOfSleep":"2020-01-02","startTime":"2020-01-02T00:00:00.1234"}]`},
		{"fit-session-object-v1", `{"fitnessActivity":"running","startTime":"2020-01-02T12:00:00Z"}`},
		{"health-weight-csv-v1", "timestamp,weight grams,data source\n2020-01-02T00:00:00Z,70000,provider\n"},
	} {
		if _, err := decodeSourceProfile(tc.profile, []byte(tc.body), nil); err == nil {
			t.Errorf("accepted refusal profile %s", tc.profile)
		} else if strings.Contains(err.Error(), "9007199254740993") || strings.Contains(err.Error(), "provider") {
			t.Fatalf("private error %v", err)
		}
	}
	if _, err := decodeSourceProfile("legacy-sleep-array-v1", []byte(strings.Repeat(" ", maxProfileBytes+1)), nil); err == nil {
		t.Fatal("oversized source")
	}
}
