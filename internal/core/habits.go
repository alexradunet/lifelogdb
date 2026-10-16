package core

import (
	"context"
	"database/sql"
	"errors"

	"lifelog/internal/text"
)

// RegisterMetric makes a metric (D27): a page titled name, whose body is the note, and its metrics row with the
// unit, one id. A plain page of that title becomes the metric, as a person is promoted (D20): a ghost, or a note
// the owner wrote about it, whose text is kept — the note is written only to an empty body. Registering one that
// exists with the same unit is a no-op, and so is one with no unit where the existing metric is a scale (isScale): a
// source that names Mood does not write its range, so the request takes the metric as it is. Another unit is
// refused: a unit never changes (metrics_unit_fixed).
func (t *Tx) RegisterMetric(name, unit, note string) (added bool, err error) {
	if err := checkTitle("metric name", name); err != nil {
		return false, err
	}
	p, err := t.lookup(name)
	if err != nil {
		return false, err
	}
	switch {
	case p == nil:
		// the note is written by the insert: creation is one write, so the metric starts at revision 1
		id, _, err := t.insertPage("metric", name, text.TitleKey(name), nil, note, "")
		if err != nil {
			return false, err
		}
		if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO metrics(id, unit) VALUES (?, ?)`, id, unit); err != nil {
			return false, err
		}
		_, err = t.syncWikilinks(id, note)
		return err == nil, err
	case p.Type == "metric":
		var have string
		if err := t.tx.QueryRowContext(t.ctx, `SELECT unit FROM metrics WHERE id = ?`, p.ID).Scan(&have); err != nil {
			return false, err
		}
		if have != unit && !(isScale(have) && unit == "") {
			return false, Conflict("metric %s exists with unit %q, not %q: a unit never changes; register a new metric", p.Title, have, unit)
		}
		return false, nil
	case p.Type != "page" || p.DayPage:
		return false, Conflict("%s is taken by a %s or a day page: a metric's title must be free or a plain page (D27)", p.Title, p.Type)
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type = 'metric', deleted_at = NULL WHERE id = ?`, p.ID); err != nil {
		return false, err
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO metrics(id, unit) VALUES (?, ?)`, p.ID, unit); err != nil {
		return false, err
	}
	if note != "" && p.Body == "" { // the owner's text is never overwritten
		if _, err := t.SetBody(p.ID, note); err != nil {
			return false, err
		}
	}
	return true, nil
}

// metricID finds a live metric by its name, in any case (its title_key).
func (t *Tx) metricID(name string) (int64, error) {
	var id int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT p.id FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL
	                       WHERE p.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND p.entity_type = 'metric'`, text.TitleKey(name)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, NotFound("no metric %q: the owner registers metrics", name)
	}
	return id, err
}

// NewMetric is what CreateMetric makes in one write: the metric, and, when it is named, its category, a page
// title; it may be empty.
type NewMetric struct {
	Name, Unit, Note string
	Category         string
}

// CreateMetric registers a metric and files it in its category (made when no page has that title, as FilePages
// makes it), in one write: a refusal of either step writes nothing.
func (s *Store) CreateMetric(ctx context.Context, source string, m NewMetric) (added bool, err error) {
	err = s.Do(ctx, source, func(t *Tx) error {
		var e error
		if added, e = t.RegisterMetric(m.Name, m.Unit, m.Note); e != nil {
			return e
		}
		if m.Category != "" {
			id, e := t.metricID(m.Name)
			if e != nil {
				return e
			}
			if _, e = t.FilePages([]int64{id}, m.Category, false); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		added = false
	}
	return
}
