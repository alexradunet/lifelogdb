package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"lifelog/internal/text"
)

// The stream (docs/plans/130-the-explore-pass.md): the days of the record as cards, the newest first, the way the
// Calendar opens. A day is in the stream when it holds something: a live entity with that day (its day page, a page,
// a file) or a current reading. Every read after the read of the days takes the days of the page as one set
// (json_each), not a window: 30 sparse days can span decades.

// The bounds of a card: the lines of its journal, its chips (the places and the people together) and the titles of its
// pages. The rest is a count.
const (
	cardLines = 4
	cardChips = 6
	cardPages = 3
)

// CardItem is a thing a card names: a place, a person, a birthday, a page or a file (Kind).
type CardItem struct {
	Kind  string
	ID    int64
	Title string
}

// Card is one day of the stream. PageID and Words are its live day page's, 0 and "" on a slim card (a day with no
// day page). Picture is the day page's cover (0 for none). Chips are its places (the at links of the day page) then
// its people (the wikilinks of the day page to a live person), at most cardChips, and MoreChips the rest. Birthdays
// are the people born on its month and day. Pages are its pages and files, at most cardPages, and MorePages the rest.
// Readings are its current readings other than the Mood's. QuietAfter is the count of days with nothing between this
// card and the next older one; 0 for the oldest card of the record.
type Card struct {
	Day        string
	PageID     int64
	Words      string
	Picture    int64
	Mood       []float64
	Chips      []CardItem
	MoreChips  int
	Birthdays  []CardItem
	Pages      []CardItem
	MorePages  int
	Readings   int
	QuietAfter int
}

// streamLast is the newest day a stream page that starts before before may show: the day before before, and never
// after today, since a future day is not in the stream. An empty before is the top of the stream. ok is false when
// no day is left (before is 0000-01-01).
func (s *Store) streamLast(before string) (string, bool, error) {
	last := s.Today()
	if before == "" {
		return last, true, nil
	}
	t, ok := text.ParseDay(before)
	if !ok {
		return "", false, Invalid("before %q must be a day, YYYY-MM-DD", before)
	}
	prev := t.AddDate(0, 0, -1).Format(time.DateOnly)
	if !text.IsDay(prev) {
		return "", false, nil
	}
	return min(last, prev), true, nil
}

// streamDays is the query of the days of the stream up to :last, the newest first.
const streamDays = `
	SELECT day FROM entities WHERE day <= :last AND deleted_at IS NULL
	UNION
	SELECT me.day FROM measurement_values me JOIN entities m ON m.id = me.metric_id AND m.deleted_at IS NULL WHERE me.day <= :last`

// Stream is one page of the stream: the days before before ("" for the top, today first), offset days skipped, at
// most limit cards. more says that an older card exists after the page. A day that is not a day for before is a 422.
func (s *Store) Stream(ctx context.Context, before string, limit, offset int) (cards []Card, more bool, err error) {
	last, ok, err := s.streamLast(before)
	if err != nil || !ok {
		return []Card{}, false, err
	}
	rows, err := s.DB.R.QueryContext(ctx, streamDays+` ORDER BY day DESC LIMIT :n OFFSET :off`,
		sql.Named("last", last), sql.Named("n", limit+1), sql.Named("off", offset))
	days, err := list(rows, err, func(d *string) []any { return []any{d} })
	if err != nil {
		return nil, false, err
	}
	cards = make([]Card, len(days))
	for i, d := range days {
		cards[i].Day = d
		if i+1 < len(days) {
			cards[i].QuietAfter = daysBetween(days[i+1], d) - 1
		}
	}
	if more = len(cards) > limit; more {
		cards, days = cards[:limit], days[:limit]
	}
	if len(cards) == 0 {
		return cards, false, nil
	}
	return cards, more, s.fillCards(ctx, cards, days)
}

// daysBetween is the count of days from a to b, two exact days, a first.
func daysBetween(a, b string) int {
	ta, _ := text.ParseDay(a)
	tb, _ := text.ParseDay(b)
	return int(tb.Sub(ta).Hours()/24 + 0.5)
}

// fillCards reads what the cards hold, each read for all of the days at once.
func (s *Store) fillCards(ctx context.Context, cards []Card, days []string) error {
	set, err := json.Marshal(days)
	if err != nil {
		return err
	}
	at := make(map[string]*Card, len(cards))
	for i := range cards {
		at[cards[i].Day] = &cards[i]
	}
	inSet := `IN (SELECT value FROM json_each(:days))`
	arg := sql.Named("days", string(set))

	entries, err := s.dayEntries(ctx, `p.day IN (SELECT value FROM json_each(?))`, "DESC", len(days), string(set))
	if err != nil {
		return err
	}
	for _, e := range entries {
		c := at[e.Day]
		c.PageID, c.Words, c.Picture = e.PageID, plainLines(e.body, cardLines, firstLineRunes), e.Picture
	}
	moods, err := s.moods(ctx, `me.day `+inSet, arg)
	if err != nil {
		return err
	}
	readings, err := s.readingsBy(ctx, `me.day `+inSet, arg)
	if err != nil {
		return err
	}
	for _, c := range at {
		c.Mood, c.Readings = moods[c.Day], readings[c.Day]-len(moods[c.Day])
	}
	dated, err := s.dated(ctx, inSet, cardPages, arg)
	if err != nil {
		return err
	}
	for _, d := range dated {
		c := at[d.Day]
		if d.Kind == "place" {
			c.addChip(CardItem{d.Kind, d.ID, d.Title})
			continue
		}
		c.Pages = append(c.Pages, CardItem{d.Kind, d.ID, d.Title})
		c.MorePages = max(d.Pages-cardPages, 0)
	}
	people, err := s.cardPeople(ctx, arg)
	if err != nil {
		return err
	}
	for _, p := range people {
		at[p.day].addChip(p.CardItem)
	}
	anniversaries, err := s.anniversariesOn(ctx, days)
	if err != nil {
		return err
	}
	for _, a := range anniversaries {
		if a.Kind == "birthday" {
			at[a.Day].Birthdays = append(at[a.Day].Birthdays, CardItem{"person", a.ID, a.Title})
		}
	}
	return nil
}

// addChip adds a place or a person to a card, or counts it when the card has cardChips already.
func (c *Card) addChip(it CardItem) {
	if len(c.Chips) < cardChips {
		c.Chips = append(c.Chips, it)
		return
	}
	c.MoreChips++
}

// dayPerson is a live person a live day page of the set links with a wikilink.
type dayPerson struct {
	day string
	CardItem
}

// cardPeople are the live people the live day pages of the days :days link with a wikilink, by day, then in the
// order of their titles. A place a day page names with a wikilink is not one: its at link is its chip.
func (s *Store) cardPeople(ctx context.Context, days sql.NamedArg) ([]dayPerson, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT d.day, pe.id, n.title
		  FROM entities d
		  JOIN links l ON l.from_id = d.id AND l.kind = 'wikilink'
		  JOIN entities pe ON pe.id = l.to_id AND pe.entity_type = 'person' AND pe.deleted_at IS NULL
		  JOIN entity_names n ON n.entity_id = pe.id AND n.name_key = pe.preferred_name_key
		 WHERE d.is_journal AND d.deleted_at IS NULL AND d.day IN (SELECT value FROM json_each(:days))
		 ORDER BY d.day, pe.preferred_name_key, pe.id`, days)
	return list(rows, err, func(p *dayPerson) []any {
		p.Kind = "person"
		return []any{&p.day, &p.ID, &p.Title}
	})
}

// NewerBefore is the before of the page of the stream that is newer than the page that starts before before, for a
// page of limit cards: "" when that page is the top of the stream. ok is false when no newer day exists: the page is
// the top.
func (s *Store) NewerBefore(ctx context.Context, before string, limit int) (newer string, ok bool, err error) {
	if before == "" {
		return "", false, nil
	}
	if _, _, err := s.streamLast(before); err != nil {
		return "", false, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT day FROM (`+streamDays+`) WHERE day >= :before ORDER BY day LIMIT :n`,
		sql.Named("last", s.Today()), sql.Named("before", before), sql.Named("n", limit+1))
	days, err := list(rows, err, func(d *string) []any { return []any{d} })
	switch {
	case err != nil:
		return "", false, err
	case len(days) == 0:
		return "", false, nil
	case len(days) <= limit:
		return "", true, nil
	}
	return days[limit], true, nil
}

// FirstDay is the oldest day of the stream, "" when the record holds no day up to today. The UNION of streamDays is a
// merge of its two indexes in day order, so the first row ends the read; a min() would walk every row.
func (s *Store) FirstDay(ctx context.Context) (string, error) {
	var day string
	err := s.DB.R.QueryRowContext(ctx, streamDays+` ORDER BY day LIMIT 1`, sql.Named("last", s.Today())).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return day, err
}
