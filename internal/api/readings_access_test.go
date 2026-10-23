package api_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The access pass, part 1 (docs/plans/141-the-access-pass.md): the form of a reading.

var (
	selectRE = regexp.MustCompile(`(?s)<select name="([^"]+)"[^>]*>(.*?)</select>`)
	inputRE  = regexp.MustCompile(`<input type="(?:date|number)" name="([^"]+)" value="([^"]*)"`)
	optionRE = regexp.MustCompile(`<option( selected)?( value="([^"]*)")?>([^<]*)</option>`)
)

// browserSends is what a browser sends with the record form of body: formSends, and for each select the option
// that the browser has chosen (the one marked selected, else the first), as a browser does with no script.
func browserSends(t *testing.T, body string) url.Values {
	t.Helper()
	form := regexp.MustCompile(`(?s)<div id="record">.*?</form>`).FindString(body)
	if form == "" {
		t.Fatalf("no record form:\n%.800s", body)
	}
	sent := formSends(t, form, `action="/measurements"`, nil)
	for _, in := range inputRE.FindAllStringSubmatch(form, -1) { // formSends reads hidden and text inputs only
		sent.Set(in[1], html.UnescapeString(in[2]))
	}
	for _, s := range selectRE.FindAllStringSubmatch(form, -1) {
		opts := optionRE.FindAllStringSubmatch(s[2], -1)
		if len(opts) == 0 {
			t.Fatalf("select %s has no option", s[1])
		}
		pick := opts[0]
		for _, o := range opts {
			if o[1] != "" {
				pick = o
			}
		}
		val := pick[3]
		if pick[2] == "" { // no value attribute: the text is the value
			val = html.UnescapeString(pick[4])
		}
		sent.Set(s[1], val)
	}
	return sent
}

// TestALinkToTheRecordFormOfAMetricRecordsThatMetric: a metric not read for years is not among the ones read lately,
// and the link ?new=record&metric=X must still record X, never the first metric of the list.
func TestALinkToTheRecordFormOfAMetricRecordsThatMetric(t *testing.T) {
	h, do := lifeOf(t)
	for _, n := range []string{"Evening walk", "Meditate"} {
		do("/", "register-metric", map[string]string{"name": n})
	}
	do("/days/"+dayOf(-1), "record", map[string]string{"metric": "Evening walk", "value": "1"}) // the one read; Meditate is not

	body := browse(t, h, "/?new=record&metric=Meditate&value=1")
	sent := browserSends(t, body)
	if sent.Get("metric") != "Meditate" {
		t.Fatalf("the browser sends metric %q, want Meditate", sent.Get("metric"))
	}
	if rec := submit(h, "/measurements", sent, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("record: %d", rec.Code)
	}
	if got := browse(t, h, "/metrics/Meditate"); !strings.Contains(got, `/days/`+fixedToday) {
		t.Errorf("the reading is not on Meditate:\n%.1500s", got)
	}
	if got := browse(t, h, "/metrics/Evening%20walk"); strings.Contains(got, `/days/`+fixedToday) {
		t.Errorf("the reading is on Evening walk")
	}
}
