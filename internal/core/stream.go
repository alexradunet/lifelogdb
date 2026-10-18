package core

import (
	"context"
	"database/sql"
	"encoding/json"
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
// Readings are its current readings other than the Mood's.
type Card struct {
	Day       string
	PageID    int64
	Words     string
	Picture   int64
	Mood      []float64
	Chips     []CardItem
	MoreChips int
	Birthdays []CardItem
	Pages     []CardItem
	MorePages int
	Readings  int
}

// streamDays is the query of the days of the stream up to :last, the newest first.
const streamDays = `
	SELECT day FROM entities WHERE day <= :last AND deleted_at IS NULL
	UNION
	SELECT me.day FROM measurement_values me JOIN entities m ON m.id = me.metric_id AND m.deleted_at IS NULL WHERE me.day <= :last`

// Stream is one page of the stream: the days up to today, the newest first, offset days skipped, at most limit
// cards; a future day is not in the stream. more says that an older card exists after the page.
func (s *Store) Stream(ctx context.Context, limit, offset int) (cards []Card, more bool, err error) {
	rows, err := s.DB.R.QueryContext(ctx, streamDays+` ORDER BY day DESC LIMIT :n OFFSET :off`,
		sql.Named("last", s.Today()), sql.Named("n", limit+1), sql.Named("off", offset))
	days, err := list(rows, err, func(d *string) []any { return []any{d} })
	if err != nil {
		return nil, false, err
	}
	cards = make([]Card, len(days))
	for i, d := range days {
		cards[i].Day = d
	}
	if more = len(cards) > limit; more {
		cards, days = cards[:limit], days[:limit]
	}
	if len(cards) == 0 {
		return cards, false, nil
	}
	return cards, more, s.fillCards(ctx, cards, days)
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
