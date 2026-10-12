package core

import (
	"database/sql"
	"errors"

	"lifelog/internal/text"
)

// byImportKey is the entity a sender's key names under this transaction's source; 0 when none.
func (t *Tx) byImportKey(importKey string) (int64, error) {
	if importKey == "" {
		return 0, nil
	}
	var id int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT id FROM entities WHERE source = ? AND import_key = ?`, t.Source, importKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// metricUnit is a registered metric's unit; found is false when no metric has the name.
func (t *Tx) metricUnit(name string) (unit string, found bool, err error) {
	err = t.tx.QueryRowContext(t.ctx, `SELECT m.unit FROM metrics m JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, text.TitleKey(name)).Scan(&unit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return unit, err == nil, err
}
