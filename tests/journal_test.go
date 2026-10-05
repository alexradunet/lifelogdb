package tests

import (
	"strings"

	"lifelog/internal/text"
)

// journal: the journal (D5, D15, D16, D22, D23) — the day page and its CHECK, capture that appends to it
// (cookbook/capture), the cookbook/day-view day view, the days that name someone or somewhere
// (cookbook/days-that-name), where the owner was (cookbook/where-was-i), and what stands in for recurrence,
// events and tasks.
func journal(s *S) {
	// ---- the day page (pages_day_page)
	c := s.fresh()
	add := func(title string, day any) int64 { return c.addPage(title, day, "", text.TitleKey(title)) }
	s.K("a page titled with a day, whose day is its title, is accepted", add("2026-09-29", "2026-09-29") != 0)
	s.K("a page titled with a day but no day is refused (pages_day_page)", add("2026-09-28", nil) == 0)
	s.K("...and one whose day is another day", add("2026-09-27", "2026-09-26") == 0)
	s.K("a second page for the same day is refused (the title is unique)", add("2026-09-29", "2026-09-29") == 0)
	s.K("a title that only looks like a day is an ordinary page (2026-02-30, 2026-9-3)", add("2026-02-30", nil) != 0 && add("2026-9-3", nil) != 0)
	s.K("an ordinary page may have a day of its own", add("Trip report", "2026-09-29") != 0)
	cols := c.col("select name from pragma_table_info('pages')")
	s.K("pages have no kind and no inbox column (D5)", !contains(cols, "kind") && !contains(cols, "triaged_at"))
	s.K("there is no events table (D22)", c.n("select count(*) from sqlite_schema where name='events'") == 0)

	// ---- cookbook/capture: append to the day page, create it on the first write
	c = s.fresh()
	c.metric("weight", "kg")
	steps, ok := s.saveSteps()
	if !ok {
		stop("cookbook/save-a-body does not have one statement of each step: %v", steps.counts)
	}
	p := P{}
	sync := func(st string) { // the cookbook/save-a-body link sync, inside cookbook/capture's transaction
		if strings.HasPrefix(strings.ToUpper(st), "UPDATE PAGES SET BODY") {
			id := p["page_id"].(int64)
			c.docSave(steps, id, c.str("select body from pages where id=?", id), c.str("select title_key from pages where id=?", id), true)
		}
	}
	_, e := c.runBlock(s.d.Block("capture"), p, sync)
	row := c.tab("select title, day, body from pages where id=?", p["page_id"])
	s.K("cookbook/capture run literally on a new day creates the day page with the entry", e == nil && row == "2026-09-29|2026-09-29|Shipped the schema doc. Review pending. [[Lifelog]]", e, row)
	s.K("...links it to what the entry names, and attaches the mood to it",
		c.n("select count(*) from links l join pages p on p.id=l.to_id where l.from_id=? and p.title='Lifelog'", p["page_id"]) == 1 &&
			c.tab("select captured_with_id from measurements") == val(p["page_id"]))
	var moodInsert string
	for _, st := range statements(s.d.Block("capture")) {
		if strings.HasPrefix(strings.ToUpper(code(st)), "INSERT INTO MEASUREMENTS") {
			moodInsert = st
		}
	}
	mood := c.n("select id from pages where title_key='mood'")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", mood)
	c.must(moodInsert, p)
	s.K("cookbook/capture does not write tombstoned Mood", c.n("select count(*) from measurements") == 1)
	c.must("UPDATE entities SET deleted_at=NULL WHERE id=?", mood)
	c.must(moodInsert, p)
	s.K("cookbook/capture writes revived Mood", c.n("select count(*) from measurements") == 2)

	var sel, upd []string
	for _, st := range statements(s.d.Block("capture")) {
		switch u := strings.ToUpper(code(st)); {
		case strings.HasPrefix(u, "SELECT"):
			sel = append(sel, st)
		case strings.HasPrefix(u, "UPDATE PAGES"):
			upd = append(upd, st)
		}
	}
	s.K("cookbook/capture finds the day page by its key, the day itself", len(sel) > 0 && c.tabOrErr(sel[0]) == val(p["page_id"])+"|None")
	plan := ""
	if len(sel) > 0 {
		plan = c.plan(sel[0])
	}
	s.K("...a search on pages_title", strings.Contains(plan, "pages_title"), plan)
	if len(upd) == 0 {
		stop("cookbook/capture has no UPDATE pages")
	}
	c.must(upd[0], P{"page_id": p["page_id"]})
	body := c.str("select body from pages where id=?", p["page_id"])
	s.K("a second capture that day appends after a blank line, in the same page", strings.Count(body, "Review pending.") == 2 &&
		strings.Contains(body, "[[Lifelog]]\n\nShipped") && c.n("select count(*) from pages where day=?", "2026-09-29") == 1)
	c = s.fresh()
	a := c.capture("first", "2026-09-30")
	b := c.capture("met [[Ana]]", "2026-09-30")
	d := c.capture("next day", "2026-10-01")
	s.K("capture: one page per day, entries in order", a == b && b != d && c.str("select body from pages where id=?", a) == "first\n\nmet [[Ana]]")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", d)
	e2 := c.capture("again", "2026-10-01")
	s.K("capture on a tombstoned day page revives it, never a second page for the day", e2 == d && c.str("select deleted_at from entities where id=?", d) == "None")
	c.savePage("see [[2026-12-02]]", "")
	g := c.n("select id from pages where title='2026-12-02'")
	s.K("a link that names a day before anything was written makes that day's page, with its day", c.tab("select day, body from pages where id=?", g) == "2026-12-02|")
	s.K("...and the first capture of that day writes into it", c.capture("it came", "2026-12-02") == g && c.str("select body from pages where id=?", g) == "it came")

	// ---- cookbook/day-view
	DV := s.d.Block("day-view")
	c = s.fresh()
	d29, d28 := c.dayPage("2026-09-29", "a day of work"), c.dayPage("2026-09-28", "yesterday")
	office, home := c.named("place", "Office"), c.named("place", "Home")
	c.link(d29, office, "at")
	c.link(d28, home, "at")
	c.pageW("Essay", "2026-09-29", "text")
	c.page("Link target")
	c.pageW("Yesterday essay", "2026-09-28", "x")
	c.named("person", "Sam")
	wt := c.metric("weight", "kg")
	c.measure(wt, "2026-09-29", 71.2, M{"taken_at": "2026-09-29T06:00:00.000Z"})
	c.measure(wt, "2026-09-29", 70.0, M{"supersedes_id": 1})
	rows := c.rows(DV, P{"day": "2026-09-29"})
	var got, kinds, items []string
	for _, r := range rows {
		got = append(got, val(r[0])+"|"+val(r[2]))
		kinds = append(kinds, val(r[0]))
		items = append(items, val(r[2]))
	}
	s.K("cookbook/day-view shows the day page, the page written that day, where I was and the corrected reading",
		contains(got, "day page|a day of work") && contains(got, "page|Essay") && contains(got, "at|Office") && contains(got, "weight|70.0 kg"), tab(rows))
	nDay := 0
	for _, k := range kinds {
		if k == "day page" {
			nDay++
		}
	}
	s.K("...the day page once, as the day page and not again as a page written that day", nDay == 1 && !contains(got, "page|2026-09-29"), tab(rows))
	none := true
	for _, x := range []string{"yesterday", "Home", "Link target", "Sam", "Yesterday essay", "71.2 kg"} {
		none = none && !contains(items, x)
	}
	s.K("...not another day's page or place, a link target, a person's page, a page of another day or the superseded reading", none, tab(rows))
	order := len(rows) > 0 && rows[0][0] == "day page"
	seenDated := false
	for _, r := range rows {
		if r[1] != nil {
			seenDated = true
		} else if seenDated {
			order = false
		}
	}
	s.K("...undated items first, the day page leading", order, tab(rows))
	pid := c.n("select id from pages where title='Essay'")
	c.must("UPDATE entities SET created_at='2026-09-29T08:00:00.000Z' WHERE id=?", pid)
	c.must("UPDATE pages SET body='text 2' WHERE id=?", pid)
	edited := false
	for _, r := range c.rows(DV, P{"day": "2026-09-29"}) {
		edited = edited || (r[0] == "page (edited)" && r[2] == "Essay")
	}
	s.K(`an edited page is flagged "(edited)"`, edited)

	// ---- cookbook/days-that-name: the days that name someone or somewhere
	c = s.fresh()
	ana := c.named("person", "Ana", M{"name": "Ana"})
	par := c.named("place", "Lakeside")
	d1 := c.capture("with [[Ana]] at [[Lakeside]]", "2026-07-31")
	d2 := c.capture("called [[Ana]]", "2026-08-02")
	c.capture("alone", "2026-08-03")
	c.savePage("an essay about [[Ana]]", "Friends")
	Q := s.d.Block("days-that-name")
	s.K("cookbook/days-that-name lists the day pages that link Ana, newest first, and not an essay that names her", eq(c.col(Q, P{"entity_id": ana}), []string{"2026-08-02", "2026-07-31"}))
	d4 := c.capture("cina cu Ana", "2026-08-04")
	c.link(d4, ana, "about")
	c.link(d1, ana, "about")
	s.K("...also a day that names her without brackets, by an about link; a day with both is listed once",
		eq(c.col(Q, P{"entity_id": ana}), []string{"2026-08-04", "2026-08-02", "2026-07-31"}))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", d4)
	s.K("cookbook/days-that-name lists the days at a place the same way", eq(c.col(Q, P{"entity_id": par}), []string{"2026-07-31"}))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", d2)
	s.K("...and drops a tombstoned day", eq(c.col(Q, P{"entity_id": ana}), []string{"2026-07-31"}))
	plan = c.plan(Q, P{"entity_id": ana})
	s.K("cookbook/days-that-name is served by links_to", strings.Contains(plan, "links_to"), plan)

	// ---- cookbook/where-was-i: at links from the day page
	c = s.fresh()
	par, cor, spa := c.named("place", "Lakeside"), c.named("place", "Northgate"), c.named("place", "Southpark")
	d7 := c.capture("am fost in northgate, apoi la southpark", "2026-08-07")
	d31 := c.capture("seara la lakeside", "2026-07-31")
	st := statements(s.d.Block("where-was-i"))
	if len(st) < 3 {
		stop("cookbook/where-was-i has %d statements, not 3", len(st))
	}
	if _, e := c.runBlock(st[0], P{"day_page_id": d31, "place_id": par}, nil); e != nil {
		stop("where-was-i: %v", e)
	}
	s.K("cookbook/where-was-i records an at link from the day page to the place, with its note", c.str("select note from links where from_id=? and to_id=? and kind='at'", d31, par) == "evening")
	s.K("...and again is a no-op (ON CONFLICT DO NOTHING)", c.tryx(st[0], P{"day_page_id": d31, "place_id": par}) == "OK" && c.n("select count(*) from links where kind='at'") == 1)
	c.link(d7, cor, "at")
	c.link(d7, spa, "at")
	d8 := c.capture("iar la lakeside", "2026-08-08")
	c.link(d8, par, "at")
	s.K("cookbook/where-was-i where was I on 2026-08-07: both places, by title", eq(c.col(st[1], P{"day": "2026-08-07"}), []string{"Northgate", "Southpark"}))
	s.K("cookbook/where-was-i the days at Lakeside, newest first", eq(c.col(st[2], P{"place_id": par}), []string{"2026-08-08", "2026-07-31"}))
	s.K("an at link to a person is refused: at points at a place", strings.HasPrefix(c.link(d7, c.named("person", "Ana"), "at"), "ERR"))
	s.K("an at link from a person is refused: at comes from a page", strings.HasPrefix(c.link(c.named("person", "Ion"), par, "at"), "ERR"))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", spa)
	s.K("...and a tombstoned place is not where I was", eq(c.col(st[1], P{"day": "2026-08-07"}), []string{"Northgate"}))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", d31)
	s.K("cookbook/where-was-i where was I on a tombstoned day: nowhere", len(c.col(st[1], P{"day": "2026-07-31"})) == 0)
	atRows := 0
	for _, r := range c.rows(DV, P{"day": "2026-07-31"}) {
		if r[0] == "at" {
			atRows++
		}
	}
	s.K("cookbook/day-view a tombstoned day lists none of its places", atRows == 0)

	// ---- what stands in for recurrence (D15), events (D22) and tasks (D23)
	c = s.fresh()
	s.K("there is no tasks table and no task link kind (D23)", c.n("select count(*) from sqlite_schema where name='tasks'") == 0 &&
		c.n("select count(*) from link_kinds where kind in ('spawned','subtask')") == 0)
	mid := c.metric("rent_paid", "")
	for _, x := range [][2]any{{"2026-01-31", 1}, {"2026-02-28", 0}, {"2026-03-31", 1}} {
		c.measure(mid, x[0].(string), x[1])
	}
	s.K(`"did I do it each month" is a 0/1 habit metric`, c.str("select group_concat(value) from (select value from measurement_values where metric_id=? order by day)", mid) == "1.0,0.0,1.0")
	c.named("person", "Ada", M{"birth_day": "1815-12-10"})
	s.K("birthdays are a query over people.birth_day", c.tab("SELECT p.title FROM people pe JOIN pages p USING(id) WHERE strftime('%m-%d', pe.birth_day) = '12-10'") == "Ada")
	s.K("no event kinds are registered: attended and is-a are gone with events (D22)", c.n("select count(*) from link_kinds where kind in ('attended','is-a')") == 0)
}
