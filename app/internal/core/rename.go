package core

import (
	"context"
	"strings"

	"lifelog/internal/text"
)

// Rename gives a plain page a new title the only way titles allow (docs/contract/titles-and-wikilinks.md,
// "Renames"): the page titled newTitle gets the text, the old page becomes a one-line #REDIRECT stub, and a
// redirect link joins them. Into a title that is free, the body moves to a new page; into an existing page only
// when the old page is empty (a typo's ghost). The old page's typed links (about, related, ...) move with it;
// that is this writer's choice, the contract leaves it open. Returns the new page's id.
func (t *Tx) Rename(id int64, newTitle string) (int64, error) {
	old, err := t.pageByID(id)
	if err != nil {
		return 0, err
	}
	switch {
	case old.Deleted:
		return 0, conflict("page %d is deleted: revive it first", id)
	case old.Type != "page":
		return 0, conflict("a %s's title is its permanent handle and is not renamed (D20)", old.Type)
	case old.DayPage:
		return 0, conflict("a day page's title is its day and is not renamed (D5)")
	case old.Stub:
		return 0, conflict("page %d is already a redirect stub", id)
	case !text.ValidTitle(newTitle):
		return 0, invalid("title %q is not a valid title (docs/contract/titles-and-wikilinks.md)", newTitle)
	case text.TitleKey(newTitle) == text.TitleKey(old.Title):
		return 0, invalid("%q and %q are the same title: a title never changes, not even its case", newTitle, old.Title)
	}
	target, err := t.Lookup(newTitle)
	if err != nil {
		return 0, err
	}
	var to int64
	if target == nil {
		var day string
		t.tx.QueryRow(`SELECT coalesce(day, '') FROM pages WHERE id = ?`, id).Scan(&day)
		if to, _, _, err = t.CreatePage(newTitle, old.Body, day, ""); err != nil {
			return 0, err
		}
	} else {
		switch {
		case strings.TrimSpace(old.Body) != "":
			return 0, &ExistsError{target.ID, target.Title}
		case target.Deleted:
			return 0, conflict("%s is deleted: revive it first", target.Title)
		case target.Stub:
			return 0, conflict("%s is itself a redirect stub: rename into its target", target.Title)
		}
		to = target.ID
	}
	if _, err := t.SetBody(id, "#REDIRECT [["+titleOf(target, newTitle)+"]]"); err != nil {
		return 0, err
	}
	if _, err := t.tx.Exec(`INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, 'redirect', `+Now+`, ?)`,
		id, to, t.Source); err != nil {
		return 0, err
	}
	rows, err := t.tx.Query(`SELECT to_id, kind, coalesce(note, '') FROM links
	                          WHERE from_id = ? AND kind NOT IN ('wikilink', 'redirect')`, id)
	if err != nil {
		return 0, err
	}
	type edge struct {
		to         int64
		kind, note string
	}
	var moved []edge
	for rows.Next() {
		var e edge
		if err := rows.Scan(&e.to, &e.kind, &e.note); err != nil {
			rows.Close()
			return 0, err
		}
		moved = append(moved, e)
	}
	rows.Close()
	for _, e := range moved {
		if err := t.Unlink(id, e.to, e.kind); err != nil {
			return 0, err
		}
		if e.to == to {
			continue // a link to the page itself would point at itself
		}
		if _, err := t.Link(to, e.to, e.kind, e.note); err != nil {
			return 0, err
		}
	}
	return to, nil
}

// titleOf is the spelling the stub names: the existing page's own title, else the new one.
func titleOf(p *PageRef, title string) string {
	if p != nil {
		return p.Title
	}
	return title
}

func (s *Store) Rename(ctx context.Context, source string, id int64, newTitle string) (to int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { to, e = t.Rename(id, newTitle); return })
	if err != nil {
		to = 0
	}
	return
}
