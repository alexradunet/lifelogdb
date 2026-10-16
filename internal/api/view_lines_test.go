package api

import (
	"regexp"
	"strings"
	"testing"
)

// chartAxis finds the two value labels of a chart's axis: the greatest value, then the least.
var chartAxis = regexp.MustCompile(`<text x="42" y="[^"]*">([^<]*)</text><text x="42" y="[^"]*">([^<]*)</text>`)

// TestALineDrawsOnlyTheReadingsWithADayAndANumber: a reading with no day, or with no number (a retraction is null
// in the JSON), is not drawn. The least and the greatest value of a line come from the readings that are drawn,
// also when the first reading of the list is not drawn. 12 October 2026 is a Monday, 14 October a Wednesday.
func TestALineDrawsOnlyTheReadingsWithADayAndANumber(t *testing.T) {
	c := clock{today: "2026-10-16"}
	for name, first := range map[string]map[string]any{
		"no day":   {"day": "not a day", "value": 5.0},
		"null":     {"day": "2026-10-12", "value": nil},
		"no value": {"day": "2026-10-12"},
		"a text":   {"day": "2026-10-12", "value": "75"},
	} {
		readings := []any{first, map[string]any{"day": "2026-10-14", "value": 70.0}, map[string]any{"day": "2026-10-16", "value": 80.0}}

		got := string(chart("2026-10-12", "2026-10-16", readings))
		if m := chartAxis.FindStringSubmatch(got); m == nil || m[1] != "80" || m[2] != "70" {
			t.Errorf("%s first: the chart's axis is %q, want 80 and 70:\n%s", name, m, got)
		}
		if !strings.Contains(got, `aria-label="2 readings from Mon 12 Oct 2026 to Fri 16 Oct 2026"`) {
			t.Errorf("%s first: the chart does not count 2 readings:\n%s", name, got)
		}

		got = string(trendLine(c, "weight", "kg", "2026-10-16", readings))
		if want := `<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="weight from 70 kg 2 days ago · Wed 14 Oct to 80 kg today"><polyline points="98.8,24.0 116.0,4.0 "/><circle cx="116.0" cy="4.0" r="2.5"/></svg>`; got != want {
			t.Errorf("%s first: the trend line is\n%s\nwant\n%s", name, got, want)
		}

		got = string(sparkLine("weight", "kg", "2026-10-16", readings))
		if want := `<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="weight: 2 readings, from 70 kg 2 days ago · Wed 14 Oct to 80 kg today"><polyline points="4.0,24.0 116.0,4.0 "/><circle cx="116.0" cy="4.0" r="2.5"/></svg>`; got != want {
			t.Errorf("%s first: the spark line is\n%s\nwant\n%s", name, got, want)
		}
	}

	none := []any{map[string]any{"day": "not a day", "value": 1.0}, map[string]any{"day": "2026-10-14", "value": nil}}
	if got := chart("2026-10-12", "2026-10-16", none); got != "" {
		t.Errorf("a chart with no reading to draw is drawn:\n%s", got)
	}
	if got := trendLine(c, "weight", "kg", "2026-10-16", none); got != "" {
		t.Errorf("a trend line with no reading to draw is drawn:\n%s", got)
	}
	if got := sparkLine("weight", "kg", "2026-10-16", none); got != "" {
		t.Errorf("a spark line with no reading to draw is drawn:\n%s", got)
	}
}

// TestAChartSaysHowManyReadingsItDraws: the label of a chart says "1 reading" for one reading and "2 readings" for two.
func TestAChartSaysHowManyReadingsItDraws(t *testing.T) {
	one := map[string]any{"day": "2026-10-12", "value": 70.0}
	two := map[string]any{"day": "2026-10-14", "value": 71.0}
	for _, c := range []struct {
		readings []any
		want     string
	}{
		{[]any{one}, `aria-label="1 reading from Mon 12 Oct 2026 to Fri 16 Oct 2026"`},
		{[]any{one, two}, `aria-label="2 readings from Mon 12 Oct 2026 to Fri 16 Oct 2026"`},
	} {
		if got := string(chart("2026-10-12", "2026-10-16", c.readings)); !strings.Contains(got, c.want) {
			t.Errorf("the chart of %v does not say %s:\n%s", c.readings, c.want, got)
		}
	}
}

// TestAChartWritesItsNumbersInFull: the axis and the title of each point write a value as num does, as the tile and
// every other place write it: 1000000, never 1e+06, and 0.00001, never 1e-05.
func TestAChartWritesItsNumbersInFull(t *testing.T) {
	readings := []any{map[string]any{"day": "2026-10-12", "value": 0.00001}, map[string]any{"day": "2026-10-14", "value": 1000000.0}}
	got := string(chart("2026-10-12", "2026-10-16", readings))
	if m := chartAxis.FindStringSubmatch(got); m == nil || m[1] != "1000000" || m[2] != "0.00001" {
		t.Errorf("the chart's axis is %q, want 1000000 and 0.00001:\n%s", m, got)
	}
	for _, want := range []string{"<title>Mon 12 Oct 2026: 0.00001</title>", "<title>Wed 14 Oct 2026: 1000000</title>"} {
		if !strings.Contains(got, want) {
			t.Errorf("the chart has no %s:\n%s", want, got)
		}
	}
}
