package core

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"lifelog/internal/text"
)

// SessionInput records endpoint evidence, not inferred local/UTC conversions.
type SessionInput struct {
	Kind                string `json:"kind"`
	Day                 string `json:"day"`
	ImportKey           string `json:"import_key,omitempty"`
	StartAt             string `json:"start_at,omitempty"`
	StartLocal          string `json:"start_local,omitempty"`
	StartOffset         string `json:"start_offset,omitempty"`
	StartZoneUnverified string `json:"start_zone_unverified,omitempty"`
	EndAt               string `json:"end_at,omitempty"`
	EndLocal            string `json:"end_local,omitempty"`
	EndOffset           string `json:"end_offset,omitempty"`
	EndZoneUnverified   string `json:"end_zone_unverified,omitempty"`
}

type Session struct {
	SessionInput
	ID                  int64  `json:"id"`
	KindID              int64  `json:"kind_id"`
	KindDeletedAt       string `json:"kind_deleted_at,omitempty"`
	Source              string `json:"source"`
	Version             string `json:"version"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
	DeletedAt           string `json:"deleted_at,omitempty"`
	ElapsedMilliseconds *int64 `json:"elapsed_milliseconds,omitempty"`
	TimeBasis           string `json:"time_basis"`
}

const sessionSelect = `SELECT s.id,n.title,s.kind_id,coalesce(e.deleted_at,''),s.day,coalesce(s.import_key,''),coalesce(s.start_at,''),coalesce(s.start_local,''),coalesce(s.start_offset,''),coalesce(s.start_zone_unverified,''),coalesce(s.end_at,''),coalesce(s.end_local,''),coalesce(s.end_offset,''),coalesce(s.end_zone_unverified,''),s.source,CAST(s.revision AS TEXT),s.created_at,s.updated_at,coalesce(s.deleted_at,'') FROM sessions s JOIN entities e ON e.id=s.kind_id JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key `

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	s := &Session{}
	if err := row.Scan(&s.ID, &s.Kind, &s.KindID, &s.KindDeletedAt, &s.Day, &s.ImportKey, &s.StartAt, &s.StartLocal, &s.StartOffset, &s.StartZoneUnverified, &s.EndAt, &s.EndLocal, &s.EndOffset, &s.EndZoneUnverified, &s.Source, &s.Version, &s.CreatedAt, &s.UpdatedAt, &s.DeletedAt); err != nil {
		return nil, err
	}
	var err error
	s.ElapsedMilliseconds, err = sessionElapsedMillis(s.StartAt, s.EndAt)
	if err != nil {
		return nil, err
	}
	s.TimeBasis = "unresolved"
	if s.ElapsedMilliseconds != nil {
		s.TimeBasis = "known UTC interval"
	}
	if s.EndAt == "" && s.EndLocal == "" {
		s.TimeBasis = "missing end"
	}
	return s, nil
}

// CaptureSession writes a session of a kind named by its title. The endpoints and the day are the sessions CHECKs; the
// kind being a live plain non-journal page is sessions_kind_insert.
func (t *Tx) CaptureSession(in SessionInput) (id int64, existing bool, err error) {
	kind, err := t.lookup(in.Kind)
	if err != nil {
		return 0, false, err
	}
	if kind == nil {
		return 0, false, NotFound("no page titled %q for the session's kind", in.Kind)
	}
	if in.ImportKey != "" {
		have, e := scanSession(t.tx.QueryRowContext(t.ctx, sessionSelect+`WHERE s.source=? AND s.import_key=?`, t.Source, in.ImportKey))
		if e == nil {
			expected := have.SessionInput
			expected.Kind = in.Kind
			if have.DeletedAt != "" || have.KindID != kind.ID || expected != in {
				return 0, false, Conflict("session source identity has changed payload or is tombstoned")
			}
			return have.ID, true, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return 0, false, e
		}
	}
	if kind.Deleted {
		return 0, false, Conflict("session kind is tombstoned")
	}
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO sessions(kind_id,day,import_key,start_at,start_local,start_offset,start_zone_unverified,end_at,end_local,end_offset,end_zone_unverified,source,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,`+nowSQL+`,`+nowSQL+`) RETURNING id`, kind.ID, in.Day, nullIfEmpty(in.ImportKey), nullIfEmpty(in.StartAt), nullIfEmpty(in.StartLocal), nullIfEmpty(in.StartOffset), nullIfEmpty(in.StartZoneUnverified), nullIfEmpty(in.EndAt), nullIfEmpty(in.EndLocal), nullIfEmpty(in.EndOffset), nullIfEmpty(in.EndZoneUnverified), t.Source).Scan(&id)
	return id, false, err
}

func (t *Tx) EditSession(id int64, version string, in SessionInput) error {
	have, err := scanSession(t.tx.QueryRowContext(t.ctx, sessionSelect+`WHERE s.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no session %d", id)
	}
	if err != nil {
		return err
	}
	if have.DeletedAt != "" {
		return Conflict("session %d is tombstoned: revive it first", id)
	}
	if err := checkVersion("session", have.Version, version); err != nil {
		return err
	}
	if in.ImportKey != "" && in.ImportKey != have.ImportKey {
		return Invalid("session import identity cannot be changed")
	}
	kind, err := t.lookup(in.Kind)
	if err != nil {
		return err
	}
	if kind == nil {
		return NotFound("no page titled %q for the session's kind", in.Kind)
	}
	if kind.Deleted && kind.ID != have.KindID {
		return Conflict("session kind %s is tombstoned: revive it first", kind.Title)
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE sessions SET kind_id=?,day=?,start_at=?,start_local=?,start_offset=?,start_zone_unverified=?,end_at=?,end_local=?,end_offset=?,end_zone_unverified=? WHERE id=?`, kind.ID, in.Day, nullIfEmpty(in.StartAt), nullIfEmpty(in.StartLocal), nullIfEmpty(in.StartOffset), nullIfEmpty(in.StartZoneUnverified), nullIfEmpty(in.EndAt), nullIfEmpty(in.EndLocal), nullIfEmpty(in.EndOffset), nullIfEmpty(in.EndZoneUnverified), id)
	return err
}

func (t *Tx) SessionLifecycle(id int64, version string, deleted bool) error {
	var have string
	var tombstoned bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT CAST(revision AS TEXT),deleted_at IS NOT NULL FROM sessions WHERE id=?`, id).Scan(&have, &tombstoned)
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no session %d", id)
	}
	if err != nil {
		return err
	}
	if err := checkVersion("session", have, version); err != nil {
		return err
	}
	if deleted == tombstoned {
		return nil
	}
	return t.setDeletedAt("sessions", id, deleted)
}

func (s *Store) Session(ctx context.Context, id int64) (*Session, error) {
	result, err := scanSession(s.DB.R.QueryRowContext(ctx, sessionSelect+`WHERE s.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no session %d", id)
	}
	return result, err
}

func (s *Store) Sessions(ctx context.Context, day, kind string, includeDeleted bool) ([]Session, error) {
	if err := checkDay("day", day); day != "" && err != nil {
		return nil, err
	}
	query := sessionSelect + `WHERE (? OR s.deleted_at IS NULL)`
	args := []any{includeDeleted}
	if day != "" {
		query += ` AND s.day=?`
		args = append(args, day)
	}
	if kind != "" {
		query += ` AND s.kind_id=(SELECT entity_id FROM entity_names WHERE name_key=?)`
		args = append(args, text.TitleKey(kind))
	}
	rows, err := s.DB.R.QueryContext(ctx, query+` ORDER BY s.day,s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		row, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// ---- the same operations alone, each its own transaction

func (s *Store) CaptureSession(ctx context.Context, source string, in SessionInput) (id int64, existing bool, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, existing, e = t.CaptureSession(in); return })
	if err != nil {
		id, existing = 0, false
	}
	return
}

func (s *Store) EditSession(ctx context.Context, source string, id int64, version string, in SessionInput) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.EditSession(id, version, in) })
}

func (s *Store) SessionLifecycle(ctx context.Context, source string, id int64, version string, deleted bool) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.SessionLifecycle(id, version, deleted) })
}

// sessionElapsedMillis is exact across years0000–9999; time.Duration cannot
// represent that span. Local clocks and mixed endpoints never establish elapsed. The endpoints come from a row, so
// they are instants in order (sessions_start_at, sessions_end_at, sessions_utc_order).
func sessionElapsedMillis(startAt, endAt string) (*int64, error) {
	if startAt == "" || endAt == "" {
		return nil, nil
	}
	start, err := time.Parse(instantLayout, startAt)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse(instantLayout, endAt)
	if err != nil {
		return nil, err
	}
	result := end.UnixMilli() - start.UnixMilli()
	return &result, nil
}
