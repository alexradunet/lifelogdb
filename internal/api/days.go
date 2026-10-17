package api

// The days: the root (today at a glance), the list of days, a day and its view, and capture.

import (
	"context"
	"net/http"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/api/view"
	"lifelog/internal/core"
	"lifelog/internal/text"
)

// recentDays is how many of the newest day pages the root shows.
const recentDays = 5

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
	actions := []siren.Action{
		action("capture", map[string]string{"day": today}, nil), action("search", nil, nil), action("find", nil, nil), action("open-day", nil, nil),
		action("create-page", nil, nil), action("create-person", nil, nil), action("create-place", nil, nil), action("add-file", nil, nil),
		record, action("register-metric", nil, nil), action("snapshot", nil, nil)}
	return &siren.Entity{
		Class: []string{"root"}, Title: "Lifelog",
		Properties: map[string]any{"today": today, "recent": recent, "on_this_day": onThisDay},
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

func (h *server) days(r *http.Request) (*siren.Entity, error) {
	limit, offset, err := pageOf(r, 60)
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"days"}, Title: "Days", Links: []siren.Link{link("self", "/days", "Days"), link("index", "/", "Home")}}
	days, props, err := readPage(e, "/days", nil, limit, offset, func(limit, offset int) ([]core.Edge, error) {
		return h.s.RecentDays(r.Context(), limit, offset)
	})
	if err != nil {
		return nil, err
	}
	e.Properties = props
	for _, d := range days {
		e.Entities = append(e.Entities, link("item", siren.DayHref(d.Title), d.Title))
	}
	return e, nil
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
