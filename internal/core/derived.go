package core

// The repairs of derived data: the owner's actions rebuild-search and resync-links. The check of each is in
// Integrity (docs/contract/integrity-checks.md); the repair is a write on the writer connection, like any other.

import (
	"context"
	"database/sql"
)

// RebuildSearch rebuilds the full-text index from the pages: the FTS5 'rebuild' command, which docs/contract/
// integrity-checks.md names as the repair of a drifted index. It is one write transaction, so a failed rebuild keeps
// the old index. It returns the number of documents in the new index. The rebuild reads every page and holds the
// writer connection until it ends.
func (s *Store) RebuildSearch(ctx context.Context) (documents int, err error) {
	err = refused(s.DB.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO entities_fts(entities_fts) VALUES ('rebuild')`); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM entity_search_content`).Scan(&documents)
	}))
	if err != nil {
		return 0, err
	}
	// A reader that read the old index reports corruption of the new one (RenewReaders says why), so the next
	// integrity check must meet readers that did not.
	s.DB.RenewReaders()
	return documents, nil
}
