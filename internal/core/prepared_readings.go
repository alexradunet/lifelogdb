package core

import (
	"database/sql"
	"errors"
)

// RecordPrepared checks the original source interpretation across all metrics, including scope,
// before allowing an idempotent import. Corrections do not change the imported root.
func (t *Tx) RecordPrepared(in Reading) (int64, error) {
	metric, err := t.prepareReading(in)
	if err != nil {
		return 0, err
	}
	var count int
	if err = t.tx.QueryRow(`SELECT count(*) FROM measurements WHERE source=? AND import_key=?`, t.Source, in.Key).Scan(&count); err != nil {
		return 0, err
	}
	if count > 1 {
		return 0, conflict("ambiguous prepared source identity")
	}
	var id, haveMetric, session int64
	var day string
	var value sql.NullFloat64
	var at, tz sql.NullString
	var supersedes, with sql.NullInt64
	err = t.tx.QueryRow(`SELECT id,metric_id,day,value,coalesce(session_id,0),taken_at,tz,supersedes_id,captured_with_id FROM measurements WHERE source=? AND import_key=?`, t.Source, in.Key).Scan(&id, &haveMetric, &day, &value, &session, &at, &tz, &supersedes, &with)
	if err == nil {
		if haveMetric != metric || day != in.Day || !value.Valid || value.Float64 != in.Value || session != in.SessionID || at.String != in.TakenAt || tz.String != in.TZ || supersedes.Valid || with.Int64 != in.CapturedWith {
			return 0, conflict("prepared source root interpretation changed")
		}
		return 0, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return t.Record(in)
}
