package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"lifelog/internal/text"
)

type ScopeRelocation struct {
	RetractionID  int64 `json:"retraction_id"`
	ReplacementID int64 `json:"replacement_id"`
}

func (t *Tx) RelocateReading(wrong int64, replacement Reading) (ScopeRelocation, error) {
	return t.relocateReading(wrong, replacement, nil)
}

// afterRetraction is a narrow fault boundary for testing cancellation of the two-insert operation.
func (t *Tx) relocateReading(wrong int64, replacement Reading, afterRetraction func(int64) error) (ScopeRelocation, error) {
	if strings.HasPrefix(t.Source, "agent:") || strings.HasPrefix(t.Source, "import:") {
		return ScopeRelocation{}, invalid("scope relocation is an owner operation, unavailable to agents/imports")
	}
	if replacement.Key != "" {
		return ScopeRelocation{}, invalid("scope relocation cannot create a keyed/import-backed root")
	}
	var metric, scope int64
	var current bool
	var value sql.NullFloat64
	err := t.tx.QueryRowContext(t.ctx, `SELECT metric_id,coalesce(session_id,0),value,NOT EXISTS(SELECT 1 FROM measurements x WHERE x.supersedes_id=me.id) FROM measurements me WHERE id=?`, wrong).Scan(&metric, &scope, &value, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return ScopeRelocation{}, notFound("no measurement %d", wrong)
	}
	if err != nil {
		return ScopeRelocation{}, err
	}
	if !current || !value.Valid {
		return ScopeRelocation{}, conflict("relocation requires current non-retracted leaf")
	}
	if scope == replacement.SessionID {
		return ScopeRelocation{}, invalid("same-scope request is not a relocation")
	}
	var imported bool
	err = t.tx.QueryRowContext(t.ctx, `WITH RECURSIVE ancestors(id,source,import_key,supersedes_id) AS (SELECT id,source,import_key,supersedes_id FROM measurements WHERE id=? UNION SELECT m.id,m.source,m.import_key,m.supersedes_id FROM measurements m JOIN ancestors a ON a.supersedes_id=m.id) SELECT EXISTS(SELECT 1 FROM ancestors WHERE import_key IS NOT NULL OR source LIKE 'import:%')`, wrong).Scan(&imported)
	if err != nil {
		return ScopeRelocation{}, err
	}
	if imported {
		return ScopeRelocation{}, invalid("imported/keyed chain scope relocation is unavailable")
	}
	target, err := t.prepareReading(replacement)
	if err != nil {
		return ScopeRelocation{}, err
	}
	if target != metric {
		return ScopeRelocation{}, invalid("relocation must retain metric identity")
	}
	if replacement.SessionID != 0 {
		var live bool
		err = t.tx.QueryRowContext(t.ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=? AND deleted_at IS NULL)`, replacement.SessionID).Scan(&live)
		if err != nil {
			return ScopeRelocation{}, err
		}
		if !live {
			return ScopeRelocation{}, invalid("replacement session must be live")
		}
	}
	if replacement.CapturedWith != 0 {
		var exists bool
		err = t.tx.QueryRowContext(t.ctx, `SELECT EXISTS(SELECT 1 FROM entities WHERE id=?)`, replacement.CapturedWith).Scan(&exists)
		if err != nil {
			return ScopeRelocation{}, err
		}
		if !exists {
			return ScopeRelocation{}, invalid("replacement capture provenance does not exist")
		}
	}

	retract, err := t.Correct(wrong, nil)
	if err != nil {
		return ScopeRelocation{}, err
	}
	if afterRetraction != nil {
		if err := afterRetraction(retract); err != nil {
			return ScopeRelocation{}, err
		}
	}
	root, err := t.Record(replacement)
	if err != nil {
		return ScopeRelocation{}, err
	}
	return ScopeRelocation{RetractionID: retract, ReplacementID: root}, nil
}

func (s *Store) RelocateReading(ctx context.Context, source string, wrong int64, replacement Reading) (ScopeRelocation, error) {
	var result ScopeRelocation
	err := s.Do(ctx, source, func(t *Tx) error { var e error; result, e = t.RelocateReading(wrong, replacement); return e })
	if err != nil {
		return ScopeRelocation{}, err
	}
	return result, nil
}

// SeriesScope selects unassociated, one session, or explicitly labeled all scopes.
func (s *Store) SeriesScope(ctx context.Context, metric, from, to, scope string, sessionID int64, includeDeleted bool) ([]Reading, error) {
	return s.seriesScope(ctx, metric, from, to, scope, sessionID, includeDeleted, false)
}

// SeriesHistory has no lower day bound; to remains inclusive and scope is unchanged.
func (s *Store) SeriesHistory(ctx context.Context, metric, to, scope string, sessionID int64, includeDeleted bool) ([]Reading, error) {
	return s.seriesScope(ctx, metric, "", to, scope, sessionID, includeDeleted, true)
}

func (s *Store) seriesScope(ctx context.Context, metric, from, to, scope string, sessionID int64, includeDeleted, allHistory bool) ([]Reading, error) {
	if !IsDay(to) || !allHistory && !IsDay(from) {
		return nil, invalid("series bounds must be exact days")
	}
	predicate := `WHERE m.id=(SELECT entity_id FROM entity_names WHERE name_key=?) AND me.day<=?`
	args := []any{text.TitleKey(metric), to}
	if !allHistory {
		predicate += ` AND me.day>?`
		args = append(args, from)
	}
	switch scope {
	case "", "unassociated":
		if sessionID != 0 {
			return nil, invalid("session id requires session scope")
		}
		predicate += ` AND me.session_id IS NULL`
	case "session":
		if sessionID == 0 {
			return nil, invalid("session scope requires session id")
		}
		predicate += ` AND me.session_id=?`
		args = append(args, sessionID)
	case "all":
		if sessionID != 0 {
			return nil, invalid("all scope does not select one session")
		}
	default:
		return nil, invalid("scope must be unassociated, session or all")
	}
	return s.readingsWithLifecycle(ctx, includeDeleted, predicate+` ORDER BY me.day,me.taken_at,me.id`, args...)
}

func (s *Store) RelocationAllowed(ctx context.Context, id int64) (bool, error) {
	var allowed bool
	err := s.DB.R.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id,source,import_key,supersedes_id) AS (SELECT id,source,import_key,supersedes_id FROM measurements WHERE id=? UNION SELECT m.id,m.source,m.import_key,m.supersedes_id FROM measurements m JOIN ancestors a ON a.supersedes_id=m.id) SELECT EXISTS(SELECT 1 FROM measurement_values WHERE id=?) AND NOT EXISTS(SELECT 1 FROM ancestors WHERE import_key IS NOT NULL OR source LIKE 'import:%')`, id, id).Scan(&allowed)
	return allowed, err
}
