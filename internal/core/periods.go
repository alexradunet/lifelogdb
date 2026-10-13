package core

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
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
	id, _, err := t.createNamed("period", title, body, nil, "", func(id int64) error {
		_, err := t.tx.ExecContext(t.ctx, `INSERT INTO periods(id,start_boundary,end_boundary) VALUES(?,?,?)`, id, start, end)
		return err
	})
	if err != nil {
		return 0, err
	}
	if _, err = t.syncWikilinks(id, body); err != nil {
		return 0, err
	}
	return id, nil
}

func (t *Tx) PromotePeriod(id int64, version string, start, end *string) error {
	var typ, have string
	var deleted bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT entity_type,CAST(revision AS TEXT),deleted_at IS NOT NULL FROM entities WHERE id=?`, id).Scan(&typ, &have, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no page %d", id)
	}
	if err != nil {
		return err
	}
	if typ != "page" {
		return Conflict("page %d is a %s, not a plain page", id, typ)
	}
	if deleted {
		return Conflict("page %d is tombstoned: revive it first", id)
	}
	if err := checkVersion("page", have, version); err != nil {
		return err
	}
	if _, err = t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type='period' WHERE id=?`, id); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO periods(id,start_boundary,end_boundary) VALUES(?,?,?)`, id, start, end)
	return err
}

func (t *Tx) EditPeriod(id int64, version string, start, end *string) error {
	var have string
	var deleted bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT CAST(e.revision AS TEXT),e.deleted_at IS NOT NULL FROM periods p JOIN entities e ON e.id=p.id WHERE p.id=?`, id).Scan(&have, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no period %d", id)
	}
	if err != nil {
		return err
	}
	if deleted {
		return Conflict("period %d is tombstoned: revive it first", id)
	}
	if err := checkVersion("period", have, version); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE periods SET start_boundary=?,end_boundary=? WHERE id=?`, start, end, id)
	return err
}

func (s *Store) LifePeriods(ctx context.Context, day, asOf string, includeDeleted bool) ([]LifePeriod, error) {
	if err := checkDay("day", day); day != "" && err != nil {
		return nil, err
	}
	if err := checkDay("as_of", asOf); asOf != "" && err != nil {
		return nil, err
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

// ---- the same operations alone, each its own transaction

func (s *Store) CreatePeriod(ctx context.Context, source, title, body string, start, end *string) (id int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, e = t.CreatePeriod(title, body, start, end); return })
	if err != nil {
		id = 0
	}
	return
}

func (s *Store) PromotePeriod(ctx context.Context, source string, id int64, version string, start, end *string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.PromotePeriod(id, version, start, end) })
}

func (s *Store) EditPeriod(ctx context.Context, source string, id int64, version string, start, end *string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.EditPeriod(id, version, start, end) })
}

// periodRange describes possible endpoint days, not observed dates: the read side of contract/period-boundaries.md.
// The grammar and the order of a stored boundary are the periods CHECKs.
type periodRange struct {
	min, max            string
	comparable, ongoing bool
}

func periodBoundary(value *string, end bool) (periodRange, error) {
	if value == nil {
		return periodRange{}, nil
	}
	v := *value
	if v == ".." {
		if end {
			return periodRange{ongoing: true}, nil
		}
		return periodRange{}, Invalid("ongoing marker is only an end boundary")
	}
	qualified := false
	if len(v) > 0 && strings.ContainsRune("?~%", rune(v[len(v)-1])) {
		qualified = true
		v = v[:len(v)-1]
	}
	if len(v) != 4 && len(v) != 7 && len(v) != 10 {
		return periodRange{}, Invalid("invalid period boundary")
	}
	for i := range len(v) {
		if (i == 4 || i == 7) && v[i] == '-' {
			continue
		}
		if v[i] < '0' || v[i] > '9' {
			return periodRange{}, Invalid("invalid period boundary")
		}
	}
	year, err := strconv.Atoi(v[:4])
	if err != nil {
		return periodRange{}, Invalid("invalid period year")
	}
	month := 1
	if len(v) >= 7 {
		month, err = strconv.Atoi(v[5:7])
		if err != nil || month < 1 || month > 12 {
			return periodRange{}, Invalid("invalid period month")
		}
	}
	lo, hi := v, v
	switch len(v) {
	case 4:
		lo = v + "-01-01"
		hi = v + "-12-31"
	case 7:
		lo = v + "-01"
		hi = time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	}
	if !IsDay(lo) || !IsDay(hi) {
		return periodRange{}, Invalid("invalid period calendar boundary")
	}
	if qualified {
		return periodRange{}, nil
	}
	return periodRange{min: lo, max: hi, comparable: true}, nil
}

func periodSpan(start, end *string) (periodRange, periodRange, error) {
	s, err := periodBoundary(start, false)
	if err != nil {
		return periodRange{}, periodRange{}, err
	}
	e, err := periodBoundary(end, true)
	if err != nil {
		return periodRange{}, periodRange{}, err
	}
	if s.comparable && e.comparable && s.min > e.max {
		return periodRange{}, periodRange{}, Invalid("period boundaries are definitely reversed")
	}
	return s, e, nil
}

func periodMembership(start, end *string, day, asOf string) (string, error) {
	if err := checkDay("day", day); err != nil {
		return "", err
	}
	if err := checkDay("as_of", asOf); asOf != "" && err != nil {
		return "", err
	}
	s, e, err := periodSpan(start, end)
	if err != nil {
		return "", err
	}
	if e.ongoing && !IsDay(asOf) {
		return "", Invalid("ongoing membership requires an explicit exact as_of day")
	}
	if s.comparable && day < s.min {
		return "outside", nil
	}
	if e.ongoing {
		if day > asOf {
			return "incomparable", nil
		}
		// A horizon is not an observed end and cannot eliminate possible starts.
		if !s.comparable {
			return "incomparable", nil
		}
		if day >= s.max {
			return "definite", nil
		}
		return "possible", nil
	}
	if e.comparable && day > e.max {
		return "outside", nil
	}
	if !s.comparable || !e.comparable {
		return "incomparable", nil
	}
	// Condition on start<=end, rather than counting impossible endpoint pairs.
	if min(s.max, e.max) <= day && day <= max(e.min, s.min) {
		return "definite", nil
	}
	return "possible", nil
}
