package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

// The calendar's reads (docs/plans/101-the-calendar-holds-every-dated-thing.md): what names a day, over a window of
// days. Each read is of rows the schema holds; the anniversaries are the application's reading of a person's days.

// maxCalendarDays bounds the window of a calendar read: a year, so a read never walks an unbounded run of days.
const maxCalendarDays = 366

// checkCalendarWindow refuses a window that is not from..through, or that is longer than a year.
func checkCalendarWindow(from, through string) error {
	if err := checkWindow(from, through); err != nil {
		return err
	}
	if len(calendarDays(from, through)) > maxCalendarDays {
		return Invalid("a calendar window is at most %d days", maxCalendarDays)
	}
	return nil
}

// calendarDays are the days from from through through; nil for a window that is not one. It stops one day after the
// bound, so the caller can see the window is too long without walking it.
func calendarDays(from, through string) []time.Time {
	first, ok1 := ParseDay(from)
	last, ok2 := ParseDay(through)
	if !ok1 || !ok2 {
		return nil
	}
	var out []time.Time
	for t := first; !t.After(last) && len(out) <= maxCalendarDays; t = t.AddDate(0, 0, 1) {
		out = append(out, t)
	}
	return out
}

// Dated is a row the calendar shows on its day: a page that is not a day page and has the day (kind page, or file
// for a file), or a live place the live day page links with at (place). ID is the page's or the place's. Pages is
// the count of the day's pages and files, on a page or a file.
type Dated struct {
	Day   string
	Kind  string
	ID    int64
	Title string
	Pages int
}

// DatedIn are the dated rows from from through through, by day: the places and the pages in the order of their
// titles, at most pagesPerDay pages and files a day.
func (s *Store) DatedIn(ctx context.Context, from, through string, pagesPerDay int) ([]Dated, error) {
	if err := checkCalendarWindow(from, through); err != nil {
		return nil, err
	}
	if pagesPerDay < 1 {
		return nil, Invalid("the dated rows need a limit of pages of 1 or more")
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT day, kind, id, title, n FROM (
		  SELECT p.day AS day, CASE p.entity_type WHEN 'file' THEN 'file' ELSE 'page' END AS kind, p.id AS id,
		         p_name.title AS title, 2 AS rank, p.preferred_name_key AS sortkey,
		         row_number() OVER (PARTITION BY p.day ORDER BY p.preferred_name_key, p.id) AS r,
		         count(*) OVER (PARTITION BY p.day) AS n
		    FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		   WHERE p.day BETWEEN :from AND :through AND NOT p.is_journal AND p.deleted_at IS NULL
		  UNION ALL
		  SELECT d.day, 'place', pl.id, pl_name.title, 1, pl.preferred_name_key, 0, 0
		    FROM entities d
		    JOIN links l ON l.from_id = d.id AND l.kind = 'at'
		    JOIN entities pl ON pl.id = l.to_id AND pl.deleted_at IS NULL
		    JOIN entity_names pl_name ON pl_name.entity_id = pl.id AND pl_name.name_key = pl.preferred_name_key
		   WHERE d.is_journal AND d.day BETWEEN :from AND :through AND d.deleted_at IS NULL
		) WHERE r <= :cap
		ORDER BY day, rank, sortkey, id`,
		sql.Named("from", from), sql.Named("through", through), sql.Named("cap", pagesPerDay))
	return list(rows, err, func(d *Dated) []any { return []any{&d.Day, &d.Kind, &d.ID, &d.Title, &d.Pages} })
}

// Anniversary is a live person's birth day or death day (Kind birthday or death), or its day in a later year, on Day
// inside a window. Years are the whole years since the day the person's row holds; 0 on that day itself.
type Anniversary struct {
	Day   string
	Kind  string
	ID    int64
	Title string
	Years int
}

// Anniversaries are the anniversaries from from through through, by day, a birthday before a death, then in the
// order of the people's titles. A day of 29 February comes back on 28 February in a common year.
func (s *Store) Anniversaries(ctx context.Context, from, through string) ([]Anniversary, error) {
	if err := checkCalendarWindow(from, through); err != nil {
		return nil, err
	}
	// the days of the window by their month and day ("05-14"), with 29 February on 28 February of a common year
	on := map[string][]string{}
	for _, t := range calendarDays(from, through) {
		day := t.Format(time.DateOnly)
		on[day[5:]] = append(on[day[5:]], day)
		if day[5:] == "02-28" && t.AddDate(0, 0, 1).Day() == 1 {
			on["02-29"] = append(on["02-29"], day)
		}
	}
	keys := make([]string, 0, len(on))
	for k := range on {
		keys = append(keys, k)
	}
	set, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT e.id, n.title, coalesce(p.birth_day, ''), coalesce(p.death_day, '')
		  FROM people p
		  JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL
		  JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
		 WHERE substr(p.birth_day, 6) IN (SELECT value FROM json_each(:set))
		    OR substr(p.death_day, 6) IN (SELECT value FROM json_each(:set))
		 ORDER BY e.preferred_name_key, e.id`, sql.Named("set", string(set)))
	out := []Anniversary{}
	var id int64
	var title, birth, death string
	if err := each(rows, err, []any{&id, &title, &birth, &death}, func() {
		for _, k := range []struct{ kind, date string }{{"birthday", birth}, {"death", death}} {
			if k.date == "" {
				continue
			}
			for _, day := range on[k.date[5:]] {
				if day < k.date {
					continue // before the person's day: not yet an anniversary
				}
				y1, _ := strconv.Atoi(day[:4])
				y0, _ := strconv.Atoi(k.date[:4])
				out = append(out, Anniversary{Day: day, Kind: k.kind, ID: id, Title: title, Years: y1 - y0})
			}
		}
	}); err != nil {
		return nil, err
	}
	// by day, a birthday before a death; the order of the titles stays inside each
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		return out[i].Kind == "birthday" && out[j].Kind == "death"
	})
	return out, nil
}
