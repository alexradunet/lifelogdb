package tests

import (
	"path/filepath"
	"strings"
)

// These isolated SQLite probes distinguish preserving rows from preserving old SQL.
// They do not migrate any Lifelog database or define a pre-freeze migration path.
func evolutionReaderCompatibility(s *S) {
	c := s.connect(filepath.Join(s.dir, "compatibility.db"))
	c.must("PRAGMA journal_mode=WAL")
	c.must("PRAGMA foreign_keys=ON")
	c.must("PRAGMA recursive_triggers=ON")
	c.must("PRAGMA synchronous=FULL")
	c.must("PRAGMA trusted_schema=OFF")
	c.must("CREATE TABLE compatibility_probe (old_name TEXT NOT NULL) STRICT")
	c.must("INSERT INTO compatibility_probe VALUES ('retained')")
	c.must("ALTER TABLE compatibility_probe ADD COLUMN added INTEGER")
	s.K("adding a column changes SELECT star but preserves explicit projections",
		c.tab("SELECT * FROM compatibility_probe") == "retained|None" &&
			c.tab("SELECT old_name FROM compatibility_probe") == "retained")
	s.K("adding a column refuses the former positional insert",
		strings.HasPrefix(c.tryx("INSERT INTO compatibility_probe VALUES ('positional')"), "ERR") &&
			c.n("SELECT count(*) FROM compatibility_probe") == 1)
	c.must("INSERT INTO compatibility_probe(old_name) VALUES ('explicit')")
	explicitRows := c.tab("SELECT old_name, added FROM compatibility_probe ORDER BY old_name")
	s.K("explicit column insert survives an additive nullable column",
		explicitRows == "explicit|None; retained|None", explicitRows)
	c.must("ALTER TABLE compatibility_probe RENAME COLUMN old_name TO new_name")
	oldQuery := c.tryx("SELECT old_name FROM compatibility_probe")
	renamedRows := c.tab("SELECT new_name FROM compatibility_probe ORDER BY new_name")
	s.K("column rename preserves data but refuses the former projection",
		strings.HasPrefix(oldQuery, "ERR") && renamedRows == "explicit; retained", oldQuery, renamedRows)
}
