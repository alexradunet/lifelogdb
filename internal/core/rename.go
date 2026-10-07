package core

import (
	"context"
	"lifelog/internal/text"
)

// Rename selects an owned preferred name, retaining previous names and the entity's identity.
// See contract/titles-and-wikilinks.md and cookbook/rename-a-page.md.
func (t *Tx) Rename(id int64, newTitle string) (int64, error) {
	old, err := t.pageByID(id)
	if err != nil {
		return 0, err
	}
	if old.Deleted {
		return 0, conflict("page %d is deleted: revive it first", id)
	}
	if old.DayPage {
		return 0, conflict("a day page's canonical date is not renamed")
	}
	if !text.ValidTitle(newTitle) {
		return 0, invalid("title %q is not a valid reference name", newTitle)
	}
	if IsDay(newTitle) {
		return 0, conflict("a calendar date is reserved for its journal day")
	}
	key := text.TitleKey(newTitle)
	target, err := t.lookupKey(key)
	if err != nil {
		return 0, err
	}
	if target != nil && target.ID != id {
		return 0, &ExistsError{target.ID, target.Title}
	}
	if target == nil {
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, newTitle, key); err != nil {
			return 0, err
		}
	} else {
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE entity_names SET title = ? WHERE entity_id = ? AND name_key = ? AND title IS NOT ?`, newTitle, id, key, newTitle); err != nil {
			return 0, err
		}
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET preferred_name_key = ? WHERE id = ? AND preferred_name_key IS NOT ?`, key, id, key); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) Rename(ctx context.Context, source string, id int64, newTitle string) (to int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { to, e = t.Rename(id, newTitle); return })
	if err != nil {
		to = 0
	}
	return
}
