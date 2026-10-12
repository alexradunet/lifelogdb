package api_test

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestTheMoodIsAChoiceOfFive: capture's mood offers the options 1 to 5 (docs/plans/092-the-app-shell.md), on the
// day's action and in the catalog; the values the API takes do not change, and the browser draws the field as a row
// of radio buttons that works with no script.
func TestTheMoodIsAChoiceOfFive(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	day := must(c.Get("/days/" + fixedToday))
	for _, a := range []string{"the day's", "the catalog's"} {
		capture := find(day, "capture")
		if a == "the catalog's" {
			capture = find(must(c.Get("/actions")), "capture")
		}
		var mood string
		for _, f := range capture.Fields {
			if f.Name == "mood" {
				mood = fmt.Sprintf("%s %v %q", f.Type, f.Required, f.Options)
			}
		}
		if mood != `number false ["1" "2" "3" "4" "5"]` {
			t.Errorf("%s capture's mood: %s", a, mood)
		}
	}

	capture := find(day, "capture")
	must(c.Do(capture, map[string]string{"text": "No mood today."}))
	must(c.Do(capture, map[string]string{"mood": "4"}))
	for _, bad := range []string{"6", "0", "2.5"} {
		if _, err := c.Do(capture, map[string]string{"mood": bad}); !clientStatus(err, 422) {
			t.Errorf("mood %s: %v, want 422", bad, err)
		}
	}
	if got := fmt.Sprint(props(must(c.Get("/days/" + fixedToday)))["mood"]); got != "[4]" {
		t.Errorf("the day's mood after the captures: %s", got)
	}

	body := browse(t, h, "/days/"+fixedToday)
	form := captureForm(t, body, "/days/"+fixedToday+"/capture")
	for _, want := range []string{`<fieldset class="segmented"><legend>Mood 1-5</legend>`, `<input type="radio" name="mood" value="1">`,
		`<input type="radio" name="mood" value="5">`, `<input type="hidden" name="back" value="/days/` + fixedToday + `">`} {
		if !strings.Contains(form, want) {
			t.Errorf("the day's Capture lacks %s:\n%s", want, form)
		}
	}
	if strings.Contains(form, `<select name="mood"`) || strings.Contains(form, " required") {
		t.Errorf("the mood is a list, or is required:\n%s", form)
	}
	rec := submit(h, "/days/"+fixedToday+"/capture", url.Values{"text": {"From the browser."}, "mood": {"3"}, "back": {"/days/" + fixedToday}}, "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/days/"+fixedToday) {
		t.Fatalf("Capture from the browser: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := fmt.Sprint(props(must(c.Get("/days/" + fixedToday)))["mood"]); got != "[4 3]" {
		t.Errorf("the day's mood after the browser's capture: %s", got)
	}
}

// TestCaptureReturnsToThePageThatSentIt: Home's Capture returns to Home, a day's to the day.
func TestCaptureReturnsToThePageThatSentIt(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	for page, want := range map[string]string{"/": "/", "/days/" + dayOf(-3): "/days/" + dayOf(-3)} {
		action := "/days/" + fixedToday + "/capture"
		if page != "/" {
			action = page + "/capture"
		}
		if form := captureForm(t, browse(t, h, page), action); !strings.Contains(form, `name="back" value="`+want+`"`) {
			t.Errorf("Capture on %s does not return to %s:\n%s", page, want, form)
		}
	}
}

// captureForm is the form of a page that posts to action.
func captureForm(t *testing.T, body, action string) string {
	t.Helper()
	for _, m := range regexp.MustCompile(`(?s)<form method="POST" action="([^"]+)"[^>]*>.*?</form>`).FindAllStringSubmatch(body, -1) {
		if html.UnescapeString(m[1]) == action {
			return m[0]
		}
	}
	t.Fatalf("no form posts to %s", action)
	return ""
}
