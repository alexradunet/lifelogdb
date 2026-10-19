package core

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// planStore is a store whose today is 2026-10-19.
func planStore(t *testing.T) *Store {
	t.Helper()
	s := fresh(t)
	s.Clock, s.Zone = func() time.Time { return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC) }, time.UTC
	return s
}

func bodyOf(t *testing.T, s *Store, id int64) string {
	t.Helper()
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Body
}

func mustPlan(t *testing.T, s *Store, title, text, on, due, every string, fromDone bool) PlanResult {
	t.Helper()
	r, err := s.Plan(ctx, "cli", title, text, on, due, every, fromDone)
	if err != nil {
		t.Fatalf("plan %q on %q: %v", text, title, err)
	}
	return r
}

func TestPlanOnADayMakesTheDayPage(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "2026-10-20", "Call Sam", "", "2026-10-22", "", false)
	p, _ := s.PageByID(ctx, r.PageID)
	if !p.IsDayPage || p.Title != "2026-10-20" {
		t.Errorf("page %+v", p)
	}
	if p.Body != "- [ ] Call Sam due:2026-10-22" || r.Written != p.Body || r.Line != 1 {
		t.Errorf("body %q, result %+v", p.Body, r)
	}
	r2 := mustPlan(t, s, "2026-10-20", "Second", "", "", "", false)
	if r2.PageID != r.PageID || r2.Line != 3 || bodyOf(t, s, r.PageID) != "- [ ] Call Sam due:2026-10-22\n\n- [ ] Second" {
		t.Errorf("second %+v, body %q", r2, bodyOf(t, s, r.PageID))
	}
}

func TestPlanOnAPeriodKeyIsCanonical(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "2026-w43", "Review", "", "", "", false)
	p, _ := s.PageByID(ctx, r.PageID)
	if p.Title != "2026-W43" || p.Period != "week" || p.Day != "" || p.IsDayPage {
		t.Errorf("page %+v", p)
	}
	if again := mustPlan(t, s, "2026-W43", "More", "", "", "", false); again.PageID != r.PageID {
		t.Errorf("a second spelling made page %d, not %d", again.PageID, r.PageID)
	}
	if id, _ := s.PageID(ctx, "2026-w43"); id != r.PageID {
		t.Errorf("the key finds page %d", id)
	}
	_, err := s.Plan(ctx, "cli", "2027-W53", "x", "", "", "", false)
	if status(err) != 422 || !strings.Contains(err.Error(), "entities_period_exists") {
		t.Errorf("2027-W53: %v", err)
	}
}

func TestPlanLinksTheWordsAndRefusesBadInput(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Trip", "Ask [[Sam]]", "2026-11-01", "", "", false)
	p, _ := s.PageByID(ctx, r.PageID)
	if titles(p.Out, "wikilink") != "Sam" {
		t.Errorf("links %v", p.Out)
	}
	for name, f := range map[string]func() error{
		"empty text":   func() error { _, e := s.Plan(ctx, "cli", "Trip", "", "", "", "", false); return e },
		"bad day":      func() error { _, e := s.Plan(ctx, "cli", "Trip", "x", "2026-2-3", "", "", false); return e },
		"bad repeat":   func() error { _, e := s.Plan(ctx, "cli", "Trip", "x", "2026-11-01", "", "fortnight", false); return e },
		"bad title":    func() error { _, e := s.Plan(ctx, "cli", "a|b", "x", "", "", "", false); return e },
		"empty title":  func() error { _, e := s.Plan(ctx, "cli", "", "x", "", "", "", false); return e },
		"every no day": func() error { _, e := s.Plan(ctx, "cli", "Trip", "x", "", "", "week", false); return e },
	} {
		if err := f(); status(err) != 422 {
			t.Errorf("%s: %v", name, err)
		}
	}
	if bodyOf(t, s, r.PageID) != "- [ ] Ask [[Sam]] on:2026-11-01" {
		t.Error("a refused plan changed the page")
	}
}

func TestPlannedListsOpenLinesInOrder(t *testing.T) {
	s := planStore(t)
	a := mustPlan(t, s, "Beta", "b1", "2026-10-18", "", "", false)
	mustPlan(t, s, "Alpha", "a1", "", "2026-10-18", "", false)
	mustPlan(t, s, "Alpha", "future", "2026-10-25", "", "", false)
	mustPlan(t, s, "Alpha", "no day", "", "", "", false)
	mustPlan(t, s, "2026-10-10", "old", "2026-10-10", "", "", false)
	if err := setBody(t, s, a.PageID, "- [ ] b1 on:2026-10-18\n\n- [ ] bad on:2026-10-18 on:2026-10-19\n\n- [x] fin on:2026-10-18 done:2026-10-18"); err != nil {
		t.Fatal(err)
	}
	items, more, err := s.Planned(ctx, "2026-10-19", 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Title+":"+it.Text)
	}
	want := "2026-10-10:old,Alpha:a1,Beta:b1"
	if strings.Join(got, ",") != want || more != 0 {
		t.Errorf("got %v more %d, want %s", got, more, want)
	}
	if items[0].PageID == 0 || !items[0].IsDayPage || items[0].Line != 1 {
		t.Errorf("first item %+v", items[0])
	}
	items, more, _ = s.Planned(ctx, "2026-10-19", 2)
	if len(items) != 2 || more != 1 {
		t.Errorf("limit 2: %d items, more %d", len(items), more)
	}
	for _, bad := range []func() error{
		func() error { _, _, e := s.Planned(ctx, "2026-2-3", 5); return e },
		func() error { _, _, e := s.Planned(ctx, "2026-10-19", 0); return e },
	} {
		if status(bad()) != 422 {
			t.Error("a bad day or limit is not 422")
		}
	}
}

func TestPlanLinesOfAWindow(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "2026-W43", "plan", "2026-10-20", "", "", false)
	mustPlan(t, s, "Other", "outside", "2026-11-20", "", "", false)
	mustPlan(t, s, "Other", "done in", "2026-10-01", "", "", false)
	mustPlan(t, s, "Other", "malformed", "2026-10-21", "", "", false)
	o, _ := s.PageID(ctx, "Other")
	body := bodyOf(t, s, o)
	body = strings.Replace(body, "- [ ] done in on:2026-10-01", "- [x] done in on:2026-10-01 done:2026-10-22", 1)
	body = strings.Replace(body, "malformed on:2026-10-21", "malformed on:2026-10-21 on:2026-10-22", 1)
	if err := setBody(t, s, o, body); err != nil {
		t.Fatal(err)
	}
	items, err := s.PlanLines(ctx, "2026-10-19", "2026-10-25")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Text)
	}
	if strings.Join(got, ",") != "done in,plan" { // sorted by planned day: 2026-10-01 before 2026-10-20
		t.Errorf("got %v", got)
	}
	if items[1].Period != "week" || items[1].PageID != r.PageID {
		t.Errorf("item %+v", items[1])
	}
	if _, err := s.PlanLines(ctx, "2026-01-01", "2027-01-02"); status(err) != 422 {
		t.Errorf("367 days: %v", err)
	}
	if _, err := s.PlanLines(ctx, "2026-01-01", "2027-01-01"); err != nil {
		t.Errorf("366 days: %v", err)
	}
	if _, err := s.PlanLines(ctx, "2026-10-25", "2026-10-19"); status(err) != 422 {
		t.Errorf("reversed: %v", err)
	}
}

func TestTickDoneWithARepeat(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Chores", "Pay rent", "", "2026-01-31", "month", false)
	res, err := s.Tick(ctx, "cli", r.PageID, 1, "Pay rent", "done", "2026-01-31")
	if err != nil {
		t.Fatal(err)
	}
	want := "- [x] Pay rent due:2026-01-31 every:month done:2026-01-31\n\n- [ ] Pay rent due:2026-02-28 every:month-31"
	if got := bodyOf(t, s, r.PageID); got != want {
		t.Errorf("body %q", got)
	}
	if res.Line != 1 || res.AppendedLine != 3 || res.Appended != "- [ ] Pay rent due:2026-02-28 every:month-31" {
		t.Errorf("result %+v", res)
	}
}

func TestTickDefaultsTheDoneDayToToday(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Chores", "Wash", "", "", "", false)
	if _, err := s.Tick(ctx, "cli", r.PageID, 1, "Wash", "done", ""); err != nil {
		t.Fatal(err)
	}
	if got := bodyOf(t, s, r.PageID); got != "- [x] Wash done:2026-10-19" {
		t.Errorf("body %q", got)
	}
}

func TestTickDroppedAppendsNothing(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Chores", "Wash", "2026-10-20", "", "week", false)
	res, err := s.Tick(ctx, "cli", r.PageID, 1, "Wash", "dropped", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := bodyOf(t, s, r.PageID); got != "- [-] Wash on:2026-10-20 every:week" || res.Appended != "" || res.AppendedLine != 0 {
		t.Errorf("body %q, result %+v", got, res)
	}
}

func TestTickRefusals(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Chores", "Wash", "", "", "", false)
	before := bodyOf(t, s, r.PageID)
	_, err := s.Tick(ctx, "cli", r.PageID, 1, "Dry", "done", "")
	var e *Error
	if status(err) != 409 || !errors.As(err, &e) || e.Code != CodeStaleVersion {
		t.Errorf("changed text: %v", err)
	}
	if bodyOf(t, s, r.PageID) != before {
		t.Error("a stale tick changed the body")
	}
	for name, f := range map[string]func() error{
		"outcome":    func() error { _, e := s.Tick(ctx, "cli", r.PageID, 1, "Wash", "later", ""); return e },
		"done day":   func() error { _, e := s.Tick(ctx, "cli", r.PageID, 1, "Wash", "done", "yesterday"); return e },
		"empty text": func() error { _, e := s.Tick(ctx, "cli", r.PageID, 1, "", "done", ""); return e },
	} {
		if status(f()) != 422 {
			t.Errorf("%s is not 422", name)
		}
	}
	if status(func() error { _, e := s.Tick(ctx, "cli", 99999, 1, "Wash", "done", ""); return e }()) != 404 {
		t.Error("missing page is not 404")
	}
	// a malformed line
	m := mustPlan(t, s, "Bad", "x", "", "", "", false)
	if err := setBody(t, s, m.PageID, "- [ ] x on:2026-10-20 on:2026-10-21"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tick(ctx, "cli", m.PageID, 1, "x", "done", ""); status(err) != 422 {
		t.Errorf("malformed: %v", err)
	}
	// a line in a block quote
	q := mustPlan(t, s, "Quote", "x", "", "", "", false)
	if err := setBody(t, s, q.PageID, "> - [ ] x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tick(ctx, "cli", q.PageID, 1, "x", "done", ""); status(err) != 409 {
		t.Errorf("quoted: %v", err)
	}
	// a tombstoned page
	if err := s.Tombstone(ctx, "cli", r.PageID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tick(ctx, "cli", r.PageID, 1, "Wash", "done", ""); status(err) != 409 {
		t.Errorf("tombstoned: %v", err)
	}
}

func TestCarryToADayPageSetsOn(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Chores", "Wash", "2026-10-19", "2026-10-30", "", false)
	res, err := s.Carry(ctx, "cli", r.PageID, 1, "Wash", "2026-10-21")
	if err != nil {
		t.Fatal(err)
	}
	if got := bodyOf(t, s, r.PageID); got != "- [>] Wash on:2026-10-19 due:2026-10-30" {
		t.Errorf("source %q", got)
	}
	if got := bodyOf(t, s, res.ToID); got != "- [ ] Wash on:2026-10-21 due:2026-10-30" || res.ToLine != 1 || res.Line != 1 || res.PageID != r.PageID {
		t.Errorf("target %q, result %+v", got, res)
	}
	p, _ := s.PageByID(ctx, res.ToID)
	if !p.IsDayPage {
		t.Error("the target is not a day page")
	}
}

func TestCarryToAPlainPageKeepsOn(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Chores", "Wash", "2026-10-19", "", "", false)
	res, err := s.Carry(ctx, "cli", r.PageID, 1, "Wash", "Later")
	if err != nil {
		t.Fatal(err)
	}
	if got := bodyOf(t, s, res.ToID); got != "- [ ] Wash on:2026-10-19" {
		t.Errorf("target %q", got)
	}
}

func TestCarryRefusals(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Chores", "Wash", "2026-10-19", "", "", false)
	before := bodyOf(t, s, r.PageID)
	if _, err := s.Carry(ctx, "cli", r.PageID, 1, "Wash", "chores"); status(err) != 422 {
		t.Errorf("to itself: %v", err)
	}
	if _, err := s.Carry(ctx, "cli", r.PageID, 1, "Wash", "2027-W53"); status(err) != 422 || !strings.Contains(err.Error(), "entities_period_exists") {
		t.Errorf("2027-W53: %v", err)
	}
	if bodyOf(t, s, r.PageID) != before {
		t.Error("a refused carry changed the source")
	}
	if id, _ := s.PageID(ctx, "2027-W53"); id != 0 {
		t.Error("a refused carry made the target")
	}
	if _, err := s.Carry(ctx, "cli", r.PageID, 1, "Dry", "Later"); status(err) != 409 {
		t.Errorf("changed text: %v", err)
	}
}

func TestAPlanWriteAdvancesTheRevision(t *testing.T) {
	s := planStore(t)
	r := mustPlan(t, s, "Chores", "Wash", "", "", "", false)
	p, _ := s.PageByID(ctx, r.PageID)
	mustPlan(t, s, "Chores", "Dry", "", "", "", false)
	if _, err := s.SaveBody(ctx, "cli", r.PageID, "other", p.Version); status(err) != 409 {
		t.Errorf("save with the old version: %v", err)
	}
	p2, _ := s.PageByID(ctx, r.PageID)
	if _, err := s.Tick(ctx, "cli", r.PageID, 1, "Wash", "done", "2026-10-19"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", r.PageID, "other", p2.Version); status(err) != 409 {
		t.Errorf("save after a tick: %v", err)
	}
}

// setBody sets a body directly, as deliberate setup of a line that the writer would not write.
func setBody(t *testing.T, s *Store, id int64, body string) error {
	t.Helper()
	return s.Do(ctx, "cli", func(tx *Tx) error { _, err := tx.SetBody(id, body); return err })
}
