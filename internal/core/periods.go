package core

import (
	"context"
	"database/sql"
	"errors"
)

// LifePeriod is a named recorded span; its ordinary page holds body, names and lifecycle.
type LifePeriod struct {
	ID         int64   `json:"id"`
	Title      string  `json:"title"`
	Start      *string `json:"start_boundary"`
	End        *string `json:"end_boundary"`
	Version    string  `json:"version"`
	DeletedAt  string  `json:"deleted_at,omitempty"`
	Membership string  `json:"membership,omitempty"`
}

func (t *Tx) CreatePeriod(title, body string, start, end *string) (int64, error) {
	if _, _, err := periodSpan(start, end); err != nil {
		return 0, err
	}
	id, _, err := t.createNamed("period", title, "", func(id int64) error {
		_, err := t.tx.ExecContext(t.ctx, `INSERT INTO periods(id,start_boundary,end_boundary) VALUES(?,?,?)`, id, start, end)
		return err
	})
	if err != nil {
		return 0, err
	}
	if _, err = t.SetBody(id, body); err != nil {
		return 0, err
	}
	return id, nil
}

func (t *Tx) PromotePeriod(id int64, version string, start, end *string) error {
	if _, _, err := periodSpan(start, end); err != nil {
		return err
	}
	var typ, have string
	var deleted bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT entity_type,CAST(revision AS TEXT),deleted_at IS NOT NULL FROM entities WHERE id=?`, id).Scan(&typ, &have, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound("no page %d", id)
	}
	if err != nil {
		return err
	}
	if typ != "page" || deleted || have != version {
		return conflict("plain live page and current version required")
	}
	if _, err = t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type='period' WHERE id=?`, id); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO periods(id,start_boundary,end_boundary) VALUES(?,?,?)`, id, start, end)
	return err
}

func (t *Tx) EditPeriod(id int64, version string, start, end *string) error {
	if _, _, err := periodSpan(start, end); err != nil {
		return err
	}
	var have string
	var deleted bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT CAST(e.revision AS TEXT),e.deleted_at IS NOT NULL FROM periods p JOIN entities e ON e.id=p.id WHERE p.id=?`, id).Scan(&have, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound("no period %d", id)
	}
	if err != nil {
		return err
	}
	if deleted || version != have {
		return conflict("live period and current version required")
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE periods SET start_boundary=?,end_boundary=? WHERE id=?`, start, end, id)
	return err
}

func (s *Store) LifePeriods(ctx context.Context, day, asOf string, includeDeleted bool) ([]LifePeriod, error) {
	if day != "" && !IsDay(day) {
		return nil, invalid("membership day must be exact")
	}
	if asOf != "" && !IsDay(asOf) {
		return nil, invalid("as_of must be exact")
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT p.id,n.title,p.start_boundary,p.end_boundary,CAST(e.revision AS TEXT),coalesce(e.deleted_at,'') FROM periods p JOIN entities e ON e.id=p.id JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key WHERE (? OR e.deleted_at IS NULL) ORDER BY e.preferred_name_key,e.id`, includeDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LifePeriod{}
	for rows.Next() {
		var p LifePeriod
		if err := rows.Scan(&p.ID, &p.Title, &p.Start, &p.End, &p.Version, &p.DeletedAt); err != nil {
			return nil, err
		}
		if day != "" {
			p.Membership, err = periodMembership(p.Start, p.End, day, asOf)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
