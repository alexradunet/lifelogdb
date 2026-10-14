package api_test

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/core"
)

// htmlForm is a form of a page as a browser sends it: where, the button, and the hidden fields.
type htmlForm struct {
	Action, Button string
	Fields         url.Values
}

var (
	formRE   = regexp.MustCompile(`(?s)<form method="POST" action="([^"]+)"[^>]*>(.*?)</form>`)
	hiddenRE = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)
	buttonRE = regexp.MustCompile(`<button[^>]*>([^<]+)</button>`)
)

// postForms are the POST forms of a page whose fields are all hidden: the one-button forms. This application's own
// markup is regular enough to read this way; a form never nests.
func postForms(body string) []htmlForm {
	var out []htmlForm
	for _, m := range formRE.FindAllStringSubmatch(body, -1) {
		if strings.Contains(m[2], "<label>") {
			continue
		}
		f := htmlForm{Action: html.UnescapeString(m[1]), Fields: url.Values{}}
		if b := buttonRE.FindStringSubmatch(m[2]); b != nil {
			f.Button = b[1]
		}
		for _, h := range hiddenRE.FindAllStringSubmatch(m[2], -1) {
			f.Fields.Add(h[1], html.UnescapeString(h[2]))
		}
		out = append(out, f)
	}
	return out
}

// formFor is the one-button form of a page with that button whose action starts with prefix.
func formFor(t *testing.T, body, button, prefix string) htmlForm {
	t.Helper()
	for _, f := range postForms(body) {
		if f.Button == button && strings.HasPrefix(f.Action, prefix) {
			return f
		}
	}
	t.Fatalf("no %s form for %s on the page", button, prefix)
	return htmlForm{}
}

// fixedToday is the server's local day in the tests of what is due: a Wednesday.
const fixedToday = "2031-05-14"

// dayOf is the local day n days from fixedToday.
func dayOf(n int) string {
	t, _ := time.Parse(time.DateOnly, fixedToday)
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// TestABrowserFormReturnsToThePageThatSentIt: a form that names a path on this server returns there with the
// feedback; any other value is ignored and the browser goes to what it changed, as before; a JSON client never
// follows back.
func TestABrowserFormReturnsToThePageThatSentIt(t *testing.T) {
	c, h := fresh(t)
	for _, tc := range []struct{ back, want string }{
		{"/deadlines?from=2026-10-01&through=2026-10-31", "/deadlines?from=2026-10-01&through=2026-10-31"},
		{"/", "/"},
		{"/days/2026-09-29?x=%2F%2F", "/days/2026-09-29?x=%2F%2F"},
		{"", "/days/2026-09-29"},
		{"//evil.example/", "/days/2026-09-29"},
		{"/\\evil.example/", "/days/2026-09-29"},
		{"https://evil.example/", "/days/2026-09-29"},
		{"javascript:alert(1)", "/days/2026-09-29"},
		{"evil", "/days/2026-09-29"},
		{"/a\r\nSet-Cookie: x=1", "/days/2026-09-29"},
		{"/page#part", "/days/2026-09-29"},
		{"/" + strings.Repeat("a", 2048), "/days/2026-09-29"},
	} {
		rec := submit(h, "/days/2026-09-29/capture", url.Values{"text": {"a line"}, "back": {tc.back}}, "")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("back %q: %d", tc.back, rec.Code)
		}
		if got := withoutReceipt(t, rec.Header().Get("Location")); got != tc.want {
			t.Errorf("back %q: Location %q, want %q", tc.back, got, tc.want)
		}
	}
	// The feedback of a write that has some goes to the page returned to, with its receipt.
	rec := submit(h, "/pages", url.Values{"title": {"Notes"}, "body": {"[[bad/name]]"}, "back": {"/tasks"}}, "")
	if loc := rec.Header().Get("Location"); !feedbackLocation(loc, "/tasks") || !strings.Contains(browse(t, h, loc), "bad/name") {
		t.Errorf("feedback did not follow the return path: %q", loc)
	}
	// A JSON client: the answer is the resource, whatever back says.
	req := httptest.NewRequest("POST", "/days/2026-09-29/capture", strings.NewReader(url.Values{"text": {"json"}, "back": {"/tasks"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusOK || out.Header().Get("Location") != "/days/2026-09-29" {
		t.Errorf("a JSON client followed back: %d %q", out.Code, out.Header().Get("Location"))
	}
	if n := len(must(c.Get(t.Context(), "/days/2026-09-29")).Properties.(map[string]any)["view"].([]any)); n == 0 {
		t.Error("the writes were not done")
	}
}

// TestDueOccurrencesOfferTheirOutcome builds a small plan around today and follows the browser's own forms:
// the overdue, today's and the next week's occurrences offer Done and Skip, a later one does not, and each
// button writes the state it names and returns to the page.
func TestDueOccurrencesOfferTheirOutcome(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	create := func(v map[string]string) string {
		e := must(c.Do(t.Context(), find(root, "create-task"), v))
		return strings.SplitN(hrefOf(e, "self"), "?", 2)[0]
	}
	late := create(map[string]string{"label": "Book the dentist", "due_day": dayOf(-10)})
	water := create(map[string]string{"label": "Water the plants", "repeat_unit": "day", "repeat_every": "5", "anchor_day": dayOf(-11)})
	later := create(map[string]string{"label": "Renew the passport", "due_day": dayOf(30)})
	undated := create(map[string]string{"label": "Read about compost"})
	gone := create(map[string]string{"label": "Old plan", "due_day": dayOf(1)})
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), gone)), "tombstone-task"), nil))

	// The JSON: one outcome for each occurrence due within the week after today, and none for a later one.
	window := "/deadlines?from=" + dayOf(-20) + "&through=" + dayOf(40)
	due := must(c.Get(t.Context(), window))
	if props(due)["today"] != fixedToday {
		t.Fatalf("today = %v", props(due)["today"])
	}
	var hrefs []string
	for _, a := range due.Actions[2:] {
		hrefs = append(hrefs, a.Name+" "+a.Href+" "+fieldOf(a, "key"))
	}
	// The series is due every 5 days from 11 days back: -11, -6, -1 and 4 are in the week after today or before it,
	// and 9 is the first later one.
	want := []string{
		"capture-occurrence " + water + "/occurrences " + dayOf(-11),
		"edit-occurrence " + late + "/occurrences/once/edit ",
		"capture-occurrence " + water + "/occurrences " + dayOf(-6),
		"capture-occurrence " + water + "/occurrences " + dayOf(-1),
		"capture-occurrence " + water + "/occurrences " + dayOf(4),
	}
	if strings.Join(hrefs, "\n") != strings.Join(want, "\n") {
		t.Fatalf("outcomes offered:\n%s\nwant\n%s", strings.Join(hrefs, "\n"), strings.Join(want, "\n"))
	}

	// The page: the groups, and a Done for the overdue one-off that returns here.
	body := browse(t, h, window)
	for _, w := range []string{"<h2>Overdue</h2>", "<h2>Next 7 days</h2>", "<h2>Later</h2>", "Renew the passport", "and 6 more"} {
		if !strings.Contains(body, w) {
			t.Errorf("the deadlines page lacks %q", w)
		}
	}
	if strings.Contains(body, "Old plan") || strings.Contains(body, "Read about compost") {
		t.Error("the deadlines page shows a tombstoned or undated task")
	}
	done := formFor(t, body, "Done", late+"/occurrences/once/edit")
	if done.Fields.Get("back") != window {
		t.Errorf("the Done form returns to %q", done.Fields.Get("back"))
	}
	for _, f := range postForms(body) {
		if strings.HasPrefix(f.Action, later) {
			t.Errorf("a later occurrence offers %s", f.Button)
		}
	}
	rec := submit(h, done.Action, done.Fields, "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != window {
		t.Fatalf("Done: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	o := props(must(c.Get(t.Context(), late+"/occurrences/once")))
	if o["state"] != "done" || !core.IsInstant(o["completed_at"].(string)) {
		t.Fatalf("after Done: %v", o)
	}

	// Skip a virtual slot: it is written skipped, with no completion.
	slot := water + "/occurrences/" + dayOf(-1)
	skip := formFor(t, browse(t, h, window), "Skip", water+"/occurrences")
	if skip.Fields.Get("key") != dayOf(-11) {
		t.Fatalf("the first Skip of the series is for %s", skip.Fields.Get("key"))
	}
	// The deadlines fold the overdue slots of a series under its first one; the task's own page has every slot.
	for _, f := range postForms(browse(t, h, window)) {
		if f.Button == "Skip" && f.Fields.Get("key") == dayOf(-1) {
			t.Error("the deadlines page shows a folded overdue slot")
		}
	}
	for _, f := range postForms(browse(t, h, water)) {
		if f.Button == "Skip" && f.Fields.Get("key") == dayOf(-1) {
			skip = f
		}
	}
	if skip.Fields.Get("key") != dayOf(-1) {
		t.Fatalf("the task page offers no Skip for %s", dayOf(-1))
	}
	if rec := submit(h, skip.Action, skip.Fields, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("Skip: %d %s", rec.Code, rec.Body.String())
	}
	if o := props(must(c.Get(t.Context(), slot))); o["state"] != "skipped" || o["virtual"] != false || o["completed_at"] != nil {
		t.Fatalf("after Skip: %v", o)
	}

	// Reopen from the occurrence's own page: open again, and the completion cleared.
	reopen := formFor(t, browse(t, h, late+"/occurrences/once"), "Reopen", late+"/occurrences/once/edit")
	if rec := submit(h, reopen.Action, reopen.Fields, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("Reopen: %d %s", rec.Code, rec.Body.String())
	}
	if o := props(must(c.Get(t.Context(), late+"/occurrences/once"))); o["state"] != "open" || o["completed_at"] != nil {
		t.Fatalf("after Reopen: %v", o)
	}

	// A form drawn before the task changed is refused, and writes nothing.
	stale := formFor(t, browse(t, h, window), "Done", water+"/occurrences")
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), water)), "edit-task"), map[string]string{"label": "Water the garden"}))
	if rec := submit(h, stale.Action, stale.Fields, ""); rec.Code != http.StatusConflict {
		t.Fatalf("a stale Done: %d", rec.Code)
	}
	if o := props(must(c.Get(t.Context(), water+"/occurrences/"+stale.Fields.Get("key")))); o["virtual"] != true || o["state"] != "open" {
		t.Fatalf("a stale Done wrote: %v", o)
	}

	// An undated one-off: its task page shows its one occurrence, with Done.
	page := browse(t, h, undated)
	if props(must(c.Get(t.Context(), undated)))["once"] == nil || formFor(t, page, "Done", undated+"/occurrences/once/edit").Fields.Get("back") != undated {
		t.Error("an undated one-off does not offer its outcome on its page")
	}
	// A tombstoned task offers no outcome anywhere.
	if names(must(c.Get(t.Context(), gone))) != "revive-task" || len(postForms(browse(t, h, gone+"/occurrences/once"))) != 0 {
		t.Error("a tombstoned task offers an outcome")
	}
}

// withoutReceipt is a Location without its one-use feedback receipt (a capture always reports what it linked).
func withoutReceipt(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Location %q: %v", location, err)
	}
	q := u.Query()
	if len(q["feedback"]) != 1 {
		t.Errorf("Location %q carries no receipt", location)
	}
	q.Del("feedback")
	u.RawQuery = q.Encode()
	return u.String()
}

// fieldOf is the value an action filled in for a field, "" when it filled none.
func fieldOf(a api.Action, name string) string {
	for _, f := range a.Fields {
		if f.Name == name && f.Value != nil {
			return fmt.Sprint(f.Value)
		}
	}
	return ""
}

// TestAnOverdueRowSaysSo: an open occurrence due before today says "overdue" in a word, not by its date alone; one
// due today does not, and neither does an overdue one once it is done (docs/plans/095-the-leaf-and-its-margin.md).
func TestAnOverdueRowSaysSo(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	create := func(v map[string]string) string {
		e := must(c.Do(t.Context(), find(root, "create-task"), v))
		return strings.SplitN(hrefOf(e, "self"), "?", 2)[0]
	}
	create(map[string]string{"label": "Book the dentist", "due_day": dayOf(-3)})
	create(map[string]string{"label": "Call the plumber", "due_day": fixedToday})
	paid := create(map[string]string{"label": "Pay the rent", "due_day": dayOf(-2)})
	window := "/deadlines?from=" + dayOf(-7) + "&through=" + dayOf(7)
	done := formFor(t, browse(t, h, window), "Done", paid+"/occurrences/once/edit")
	if rec := submit(h, done.Action, done.Fields, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("Done: %d", rec.Code)
	}
	rowRE := regexp.MustCompile(`(?s)<li[^>]*><div class="occ">.*?</div></li>`)
	for _, path := range []string{window, "/"} {
		rows := map[string]bool{}
		for _, row := range rowRE.FindAllString(browse(t, h, path), -1) {
			for _, label := range []string{"Book the dentist", "Call the plumber", "Pay the rent"} {
				if strings.Contains(row, ">"+label+"</a>") {
					rows[label] = strings.Contains(row, `<span class="tag overdue">overdue</span>`)
				}
			}
		}
		if overdue, ok := rows["Book the dentist"]; !ok || !overdue {
			t.Errorf("%s: the occurrence due 3 days ago does not say overdue (found %v)", path, ok)
		}
		if rows["Call the plumber"] {
			t.Errorf("%s: the occurrence due today says overdue", path)
		}
		if rows["Pay the rent"] {
			t.Errorf("%s: the done occurrence says overdue", path)
		}
	}
}

// TestTheMarginFoldsTheOverdueSlotsOfASeries: a daily series three days behind is one overdue row in the margin of
// Home and of today, with "and 2 more" that opens the task, beside today's slot; the task's own page keeps every slot.
func TestTheMarginFoldsTheOverdueSlotsOfASeries(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	e := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "create-task"),
		map[string]string{"label": "Water the plants", "repeat_unit": "day", "repeat_every": "1", "anchor_day": dayOf(-3)}))
	water := strings.SplitN(hrefOf(e, "self"), "?", 2)[0]
	rows := regexp.MustCompile(`<a href="` + regexp.QuoteMeta(water) + `/occurrences/([0-9-]+)">Water the plants</a>`)
	for _, path := range []string{"/", "/days/" + fixedToday} {
		body := browse(t, h, path)
		var keys []string
		for _, m := range rows.FindAllStringSubmatch(body, -1) {
			keys = append(keys, m[1])
		}
		if want := dayOf(-3) + " " + fixedToday; strings.Join(keys, " ") != want {
			t.Errorf("%s: the rows of the series are %v, want %s", path, keys, want)
		}
		if !strings.Contains(body, `<a href="`+water+`">and 2 more</a>`) {
			t.Errorf("%s: the overdue row does not say that it stands for 2 more", path)
		}
	}
	if n := len(rows.FindAllString(browse(t, h, water), -1)); n != 0 {
		t.Errorf("the task page names its rows by the task: %d rows named so", n)
	}
	page := browse(t, h, water)
	for _, k := range []string{dayOf(-3), dayOf(-2), dayOf(-1), fixedToday} {
		if !strings.Contains(page, water+"/occurrences/"+k+`"`) {
			t.Errorf("the task page lacks the slot %s", k)
		}
	}
}
