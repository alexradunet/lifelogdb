package api

// The days: the root (today at a glance), the list of days, a day and its view, and capture.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/api/view"
	"lifelog/internal/core"
	"lifelog/internal/text"
)

// recentDays is how many of the newest day pages the root shows.
const recentDays = 5

// The title suggestions of the search box (docs/plans/130-the-explore-pass.md, part 7): the titles of the last 90
// days, at most 200 of them, about 8 KB on each page.
const (
	recentTitleDays = 90
	recentTitleMax  = 200
)

// recentTitles are the titles the search box suggests, by the clock of the store s.
func recentTitles(ctx context.Context, s *core.Store) ([]string, error) {
	since := s.Local().AddDate(0, 0, -recentTitleDays).Format(time.DateOnly)
	return s.RecentTitles(ctx, since, recentTitleMax)
}

// recentDay is a day page as the root lists it: the day, the first line of its body, its mood and the address of the
// picture of its cover. Picture hides the id of the embedded entry, which has no JSON name.
type recentDay struct {
	core.DayEntry
	Mood    []float64 `json:"mood,omitempty"`
	Picture string    `json:"picture,omitempty"`
}

// onThisDayMax is how many earlier years a day lists.
const onThisDayMax = 10

// withMoods adds to each day entry the day's moods, and the address of its picture.
func (h *server) withMoods(ctx context.Context, entries []core.DayEntry) ([]recentDay, error) {
	out := []recentDay{}
	if len(entries) == 0 {
		return out, nil
	}
	lo, hi := entries[0].Day, entries[0].Day
	for _, d := range entries {
		lo, hi = min(lo, d.Day), max(hi, d.Day)
	}
	moods, err := h.s.Moods(ctx, lo, hi)
	if err != nil {
		return nil, err
	}
	for _, d := range entries {
		out = append(out, recentDay{DayEntry: d, Mood: moods[d.Day], Picture: pictureHref(d.Picture)})
	}
	return out, nil
}

// onThisDay are the day pages of earlier years on the month and day of a day, the newest first.
func (h *server) onThisDay(ctx context.Context, day string) ([]recentDay, error) {
	entries, err := h.s.OnThisDay(ctx, day, onThisDayMax)
	if err != nil {
		return nil, err
	}
	return h.withMoods(ctx, entries)
}

// recentDaysUpTo are the newest day pages up to today, the newest first, with their moods.
func (h *server) recentDaysUpTo(ctx context.Context, today string) ([]recentDay, error) {
	entries, err := h.s.DayEntries(ctx, "0000-01-01", today, true, recentDays)
	if err != nil {
		return nil, err
	}
	return h.withMoods(ctx, entries)
}

// The root is the entry point of the API: the newest day pages, and every action that starts something. A browser's
// Home is the page of today instead (docs/plans/129-the-ux-pass.md, part 10), which carries the same lists.
func (h *server) root(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	today := h.s.Today()
	if wantsHTML(r) {
		return nil, h.redirectKeepingFeedback(r, siren.DayHref(today))
	}
	recent, err := h.recentDaysUpTo(ctx, today)
	if err != nil {
		return nil, err
	}
	onThisDay, err := h.onThisDay(ctx, today)
	if err != nil {
		return nil, err
	}
	record, err := h.recordAction(ctx, "", today)
	if err != nil {
		return nil, err
	}
	titles, err := recentTitles(ctx, h.s)
	if err != nil {
		return nil, err
	}
	actions := []siren.Action{
		action("capture", map[string]string{"day": today}, nil), action("search", nil, nil), action("find", nil, nil), action("open-day", nil, nil),
		action("create-page", nil, nil), action("create-person", nil, nil), action("create-place", nil, nil), action("add-file", nil, nil),
		record, action("register-metric", nil, nil), action("snapshot", nil, nil)}
	return &siren.Entity{
		Class: []string{"root"}, Title: "Lifelog",
		Properties: map[string]any{"today": today, "recent": recent, "on_this_day": onThisDay, "recent_titles": titles},
		Links: []siren.Link{link("self", "/", "Lifelog"), link("today", siren.DayHref(today), "Today"), link("days", "/days", "Days"),
			link("browse", siren.BrowseHref(nil), "Browse"), link("categories", siren.BrowseHref(nil), "Categories"), link("people", siren.BrowseKindHref("person"), "People"),
			link("places", siren.BrowseKindHref("place"), "Places"), link("media", siren.BrowseKindHref("media"), "Media"), link("files", siren.BrowseKindHref("document"), "Documents"),
			link("metrics", "/metrics", "Metrics"), link("ghosts", siren.GhostsHref(), "Ghost pages"), link("actions", "/actions", "All actions"),
			link("calendar", "/months/current", "Calendar"),
			link("week", "/weeks/current", "This week"), link("year", "/years/current", "This year")},
		Actions: actions,
	}, nil
}

func (h *server) today(*http.Request) (*siren.Entity, error) {
	return nil, redirect{siren.DayHref(h.s.Today())}
}

// dayNamed is the day that a path or a field names. The word "today" is today: the local day of the store's clock, the
// day that GET /days/today redirects to. Every field of the catalog whose type is date is read through it, so the word
// has one meaning in the API (TestEveryDayFieldReadsTheWordToday). Any other value comes back as it is: the schema's
// CHECK refuses a write of a value that is not a day, and open-day checks its own.
func (h *server) dayNamed(day string) string {
	if day == "today" {
		return h.s.Today()
	}
	return day
}

// neighbour is the page next to this one in time: the day its address stands for (the first day of a month or of
// a week), its address and its title.
type neighbour struct {
	day         time.Time
	href, title string
}

// neighbourLinks are the links prev and next of a page of the calendar. A link leads only to a page that answers: a
// neighbour whose day is not a day (before 0000-01-01, after 9999-12-31) has no link.
func neighbourLinks(prev, next neighbour) []siren.Link {
	var out []siren.Link
	if text.IsDay(prev.day.Format(time.DateOnly)) {
		out = append(out, link("prev", prev.href, prev.title))
	}
	if text.IsDay(next.day.Format(time.DateOnly)) {
		out = append(out, link("next", next.href, next.title))
	}
	return out
}

// The stream (docs/plans/130-the-explore-pass.md): /days is the days of the record as cards, the newest first. A page
// is the days before before, offset days skipped, at most limit of them; next is the older days, prev the newer.

// streamLimit is the count of the cards of a page of the stream when the request names none.
const streamLimit = 30

// cardChip is a place, a person or a birthday of a card, with the address of its page.
type cardChip struct {
	Title string `json:"title"`
	Href  string `json:"href"`
}

// cardPage is a page or a file of a card (Kind), with its address.
type cardPage struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Href  string `json:"href"`
}

// dayCard is a card of the stream as the API shows it (core.Card). A slim card, a day with no day page, has no page_id
// and no words.
type dayCard struct {
	Day        string     `json:"day"`
	PageID     int64      `json:"page_id,omitempty"`
	Words      string     `json:"words,omitempty"`
	Mood       []float64  `json:"mood,omitempty"`
	Picture    string     `json:"picture,omitempty"`
	Places     []cardChip `json:"places,omitempty"`
	People     []cardChip `json:"people,omitempty"`
	MoreChips  int        `json:"more_chips,omitempty"`
	Birthdays  []cardChip `json:"birthdays,omitempty"`
	Pages      []cardPage `json:"pages,omitempty"`
	MorePages  int        `json:"more_pages,omitempty"`
	Readings   int        `json:"readings,omitempty"`
	QuietAfter int        `json:"quiet_after,omitempty"`
}

// cardOf is a card of the store as the API shows it.
func cardOf(c core.Card) dayCard {
	chip := func(it core.CardItem) cardChip { return cardChip{Title: it.Title, Href: siren.PageHref(it.ID)} }
	d := dayCard{Day: c.Day, PageID: c.PageID, Words: c.Words, Mood: c.Mood, Picture: pictureHref(c.Picture),
		MoreChips: c.MoreChips, MorePages: c.MorePages, Readings: c.Readings, QuietAfter: c.QuietAfter}
	for _, it := range c.Chips {
		if it.Kind == "place" {
			d.Places = append(d.Places, chip(it))
		} else {
			d.People = append(d.People, chip(it))
		}
	}
	for _, it := range c.Birthdays {
		d.Birthdays = append(d.Birthdays, chip(it))
	}
	for _, it := range c.Pages {
		d.Pages = append(d.Pages, cardPage{Kind: it.Kind, Title: it.Title, Href: siren.PageHref(it.ID)})
	}
	return d
}

// streamHref is the address of a page of the stream: the days before before ("" for the top), with limit and offset
// when they are not the defaults.
func streamHref(before string, limit, offset int) string {
	v := url.Values{}
	if before != "" {
		v.Set("before", before)
	}
	if limit != streamLimit {
		v.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	if len(v) == 0 {
		return "/days"
	}
	return "/days?" + v.Encode()
}

// yearJump and monthJump are a year and a month of the scrubber: its name and the page of the stream that starts
// after it.
type (
	yearJump struct {
		Year string `json:"year"`
		Href string `json:"href"`
	}
	monthJump struct {
		Month string `json:"month"`
		Href  string `json:"href"`
	}
)

func (h *server) days(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	limit, offset, err := pageOf(r, streamLimit)
	if err != nil {
		return nil, err
	}
	before, today := r.URL.Query().Get("before"), h.s.Today()
	cards, more, err := h.s.Stream(ctx, before, limit, offset)
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"days"}, Title: "Days",
		Links:   []siren.Link{link("self", streamHref(before, limit, offset), "Days"), link("index", "/", "Home")},
		Actions: []siren.Action{action("open-day", nil, nil), action("random-day", nil, nil)}}
	if offset > 0 {
		e.Links = append(e.Links, link("prev", streamHref(before, limit, max(offset-limit, 0)), "Newer days"))
	} else if newer, ok, err := h.s.NewerBefore(ctx, before, limit); err != nil {
		return nil, err
	} else if ok {
		e.Links = append(e.Links, link("prev", streamHref(newer, limit, 0), "Newer days"))
	}
	if more {
		e.Links = append(e.Links, link("next", streamHref(cards[len(cards)-1].Day, limit, 0), "Older days"))
	}
	// The zoom is anchored on the top card's day, so Week, Month and Year open the period the page shows.
	anchor := today
	if len(cards) > 0 {
		anchor = cards[0].Day
	}
	at, _ := text.ParseDay(anchor)
	e.Links = append(e.Links, zoomLinks(at, "/days")...)
	shown := []dayCard{}
	for _, c := range cards {
		shown = append(shown, cardOf(c))
		e.Entities = append(e.Entities, link("item", siren.DayHref(c.Day), c.Day))
	}
	years, months, err := h.scrubber(ctx, at, today)
	if err != nil {
		return nil, err
	}
	props := map[string]any{"today": today, "before": before, "limit": limit, "offset": offset, "cards": shown,
		"years": years, "months": months}
	if before == "" && offset == 0 { // On this day is on the first page only
		onThisDay, err := h.onThisDay(ctx, today)
		if err != nil {
			return nil, err
		}
		props["on_this_day"] = onThisDay
	}
	e.Properties = props
	return e, nil
}

// scrubber are the jumps of the stream: each year from the first dated year to today's, the newest first, and the
// twelve months of the year of at. A jump leads to the page of the days before the first day after it, so a year with
// nothing in it shows the next older card.
func (h *server) scrubber(ctx context.Context, at time.Time, today string) ([]yearJump, []monthJump, error) {
	years, months := []yearJump{}, []monthJump{}
	first, err := h.s.FirstDay(ctx)
	if err != nil || first == "" {
		return years, months, err
	}
	from, _ := text.ParseDay(first)
	to, _ := text.ParseDay(today)
	jump := func(t time.Time) string { // the page before t; the top for a day that is not one
		if day := t.Format(time.DateOnly); text.IsDay(day) {
			return streamHref(day, streamLimit, 0)
		}
		return "/days"
	}
	for y := to.Year(); y >= from.Year(); y-- {
		years = append(years, yearJump{Year: fmt.Sprintf("%04d", y), Href: jump(time.Date(y+1, time.January, 1, 0, 0, 0, 0, time.UTC))})
	}
	for m := time.December; m >= time.January; m-- {
		t := time.Date(at.Year(), m, 1, 0, 0, 0, 0, time.UTC)
		months = append(months, monthJump{Month: t.Format("2006-01"), Href: jump(t.AddDate(0, 1, 0))})
	}
	return years, months, nil
}

// dayView is a day as the API shows it: the cookbook's day view, and beside it the day's mood and the day the
// server read as today
// (docs/plans/091-the-browser-for-daily-use.md).
type dayView struct {
	core.Day
	Mood   []float64 `json:"mood"`
	Trends []trend   `json:"trends"`
	// OnThisDay are the day pages of earlier years on this month and day.
	OnThisDay []recentDay `json:"on_this_day"`
	Today     string      `json:"today"`
	// Recent are the newest day pages, on the day of today only: Home is the page of today.
	Recent []recentDay `json:"recent,omitempty"`
	// Cover is the cover of the day page, which the journal draws above it; absent when it has none.
	Cover *core.Cover `json:"cover,omitempty"`
	// Pictures are the other pictures of the day page, as on a page.
	Pictures []core.Cover `json:"pictures,omitempty"`
}

func (h *server) day(r *http.Request) (*siren.Entity, error) {
	return h.dayEntity(r.Context(), r.PathValue("day"))
}

// dayEntity is the resource /days/{day} serves.
func (h *server) dayEntity(ctx context.Context, day string) (*siren.Entity, error) {
	d, err := h.s.Day(ctx, day)
	if err != nil {
		return nil, err
	}
	today := h.s.Today()
	moods, err := h.s.Moods(ctx, day, day)
	if err != nil {
		return nil, err
	}
	trends, err := h.trends(ctx, day, d.Readings)
	if err != nil {
		return nil, err
	}
	onThisDay, err := h.onThisDay(ctx, day)
	if err != nil {
		return nil, err
	}
	mood := moods[day]
	if mood == nil {
		mood = []float64{}
	}
	var recent []recentDay
	if day == today {
		if recent, err = h.recentDaysUpTo(ctx, today); err != nil {
			return nil, err
		}
	}
	record, err := h.recordAction(ctx, "", day)
	if err != nil {
		return nil, err
	}
	t, _ := text.ParseDay(day)
	e := &siren.Entity{
		Class: []string{"day"}, Title: day, Properties: dayView{Day: *d, Mood: mood, Trends: trends, OnThisDay: onThisDay, Today: today, Recent: recent},
		Links:   []siren.Link{link("self", siren.DayHref(day), day)},
		Actions: []siren.Action{action("capture", map[string]string{"day": day}, nil), record, action("open-day", nil, nil)},
	}
	// 0000-01-01 has no day before it, and 9999-12-31 none after it.
	before, after := t.AddDate(0, 0, -1), t.AddDate(0, 0, 1)
	e.Links = append(e.Links, neighbourLinks(
		neighbour{before, siren.DayHref(before.Format(time.DateOnly)), "Previous day"},
		neighbour{after, siren.DayHref(after.Format(time.DateOnly)), "Next day"})...)
	e.Links = append(e.Links, link("metrics", "/metrics", "Metrics"), link("index", "/", "Home"), link("month", siren.MonthHref(t), t.Format(layoutMonth)))
	if d.PageID != 0 {
		e.Links = append(e.Links, link("page", siren.PageHref(d.PageID), "The day page"))
		p, err := h.s.PageByID(ctx, d.PageID)
		if err != nil {
			return nil, err
		}
		e.Actions = append(e.Actions, saveBodyAction(d.PageID, p.Body, p.Version))
		e.Links = append(e.Links, coverLinks(p)...)
		e.Actions = append(e.Actions, action("set-cover", idParam(d.PageID), nil))
		dv := e.Properties.(dayView)
		dv.Cover, dv.Pictures = p.Cover, p.Pictures
		e.Properties = dv
	}
	for _, m := range d.Readings {
		e.Entities = append(e.Entities, link("reading", siren.MeasurementHref(m.ID), m.Metric+" "+view.Num(m.Value)))
	}
	return e, nil
}

func (h *server) capture(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	mood, err := number(v, "mood")
	if err != nil {
		return nil, err
	}
	day := h.dayNamed(r.PathValue("day")) // the share target of the manifest cannot know the day: it sends the word
	_, sync, already, err := h.s.CaptureWithKey(r.Context(), src, v.Get("import_key"), day, v.Get("text"), mood)
	if err != nil {
		return nil, err
	}
	if already { // a re-send: the first one stands (docs/decisions/D28-capture-keys.md), and the answer links its day
		if day, err = h.s.CapturedDay(r.Context(), src, v.Get("import_key")); err != nil {
			return nil, err
		}
		return &siren.Entity{Class: []string{"result"}, Title: "Already added",
			Properties: map[string]any{"message": "a capture with this import_key was added before; nothing changed"},
			Result:     map[string]any{"already": true},
			Links:      []siren.Link{link("self", siren.DayHref(day), day), link("day", siren.DayHref(day), day), link("index", "/", "Home")}}, nil
	}
	return withResult(h.dayEntity(r.Context(), day))(sync)
}
