package tests

// A suite that cannot fail proves nothing. Each mutant is the docs tree with one rule broken — in schema.sql, in a
// cookbook block or in the text of a page — and the suite that owns the rule must notice: it must run to its end
// and report at least one failed expectation (stopping on the broken document is not "noticed"). Every suite named
// here must first pass on the unmutated document. A new rule gets a mutant here (tests/README.md keeps the count).

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// change is one broken rule: replace the nth occurrence of old (counted over schema.sql and then the pages in
// reading order) by new; or make one named CHECK always true; or make one trigger never fire.
type change struct {
	kind, a, b string
	nth        int
}

func edit(old, new string) change             { return change{"edit", old, new, 0} }
func editNth(old, new string, nth int) change { return change{"edit", old, new, nth} }
func nocheck(name string) change              { return change{"nocheck", name, "", 0} }
func notrigger(name string) change            { return change{"notrigger", name, "", 0} }

// apply is the files the change breaks, by their path relative to docs/.
func (ch change) apply(d *Docs) (map[string]string, error) {
	files := append([]string{"schema/schema.sql"}, d.Pages()...)
	replace := func(old, new string, nth int) (map[string]string, error) {
		seen := 0
		for _, r := range files {
			t := d.Page(r)
			for i := 0; ; {
				j := strings.Index(t[i:], old)
				if j < 0 {
					break
				}
				if seen == nth {
					at := i + j
					return map[string]string{r: t[:at] + new + t[at+len(old):]}, nil
				}
				seen++
				i += j + len(old)
			}
		}
		return nil, fmt.Errorf("mutation target found %dx: %q", seen, clip(old, 70))
	}
	ddl := d.DDL()
	switch ch.kind {
	case "nocheck": // its condition is always true; the CHECK's parenthesis is found by counting
		m := regexp.MustCompile(`CONSTRAINT ` + ch.a + `\s+CHECK\s*\(`).FindStringIndex(ddl)
		if m == nil {
			return nil, fmt.Errorf("no CHECK %s", ch.a)
		}
		i, depth := m[1], 1
		for ; depth > 0 && i < len(ddl); i++ {
			switch ddl[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		return replace(ddl[m[0]:i], "CONSTRAINT "+ch.a+" CHECK (1)", 0)
	case "notrigger": // its WHEN becomes WHEN 0, or WHEN 0 is added before BEGIN
		m := regexp.MustCompile(`(?s)CREATE TRIGGER ` + ch.a + `\b(.*?)BEGIN`).FindStringSubmatch(ddl)
		if m == nil {
			return nil, fmt.Errorf("no trigger %s", ch.a)
		}
		head := m[1]
		if w := strings.Index(head, "WHEN "); w >= 0 {
			head = head[:w]
		} else {
			head = strings.TrimRight(head, " \t\n") + " "
		}
		return replace(m[0], "CREATE TRIGGER "+ch.a+head+"WHEN 0 BEGIN", 0)
	}
	return replace(ch.a, ch.b, ch.nth)
}

var mutants = []struct {
	suite, name string
	change      change
}{
	{"dates", "a day CHECK uses = instead of IS", edit("CONSTRAINT pages_day CHECK (day IS NULL OR date(day) IS day)", "CONSTRAINT pages_day CHECK (day IS NULL OR date(day) = day)")},
	{"dates", "instants lose their milliseconds", edit("CONSTRAINT entities_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at)", "CONSTRAINT entities_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%SZ', created_at) IS created_at)")},
	{"identity", "cookbook/capture attaches the mood with last_insert_rowid()", edit("SELECT id, '2026-09-29', 4, 'ui', :page_id, strftime", "SELECT id, '2026-09-29', 4, 'ui', last_insert_rowid(), strftime")},
	{"identity", "entities.source may be NULL", edit("source      TEXT NOT NULL CONSTRAINT entities_source CHECK (length(source)", "source      TEXT CONSTRAINT entities_source CHECK (length(source)")},
	{"identity", "entities.source can change", edit("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.import_key IS NOT OLD.import_key")},
	{"identity", "entities.import_key can change", edit("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.source IS NOT OLD.source")},
	{"identity", "an entity may be imported twice", edit("CREATE UNIQUE INDEX entities_import", "CREATE INDEX entities_import")},
	{"identity", "cookbook/import-a-row-once revives a tombstoned import", edit("WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);", "WHERE source = 'import:vault' AND import_key = :import_key);")},
	{"identity", "the mirror drops the source", edit("VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NEW.source);", "VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, 'ui');")},
	{"identity", "people may be hard-deleted", edit("CREATE TRIGGER people_no_delete BEFORE DELETE ON people\nBEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;\n", "")},
	{"identity", "editing a page does not bump updated_at", edit("CREATE TRIGGER pages_touch AFTER UPDATE ON pages BEGIN\n", "CREATE TRIGGER pages_touch AFTER UPDATE ON pages WHEN 0 BEGIN\n")},
	{"identity", "the tombstone does not bump updated_at", edit("CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities\n  WHEN NEW.deleted_at IS NOT OLD.deleted_at", "CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities\n  WHEN 0")},
	{"links", "a redirect may not point at a person", edit("'page,person,place', 'old stub page", "'page',         'old stub page")},
	{"links", "located-in accepts any endpoint", edit("  ('located-in', 0, 'place',   'place',", "  ('located-in', 0, NULL,      NULL,")},
	{"named", "people hang off entities instead of their page", edit("  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),\n  CONSTRAINT people_death_day_order", "  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),\n  CONSTRAINT people_death_day_order")},
	{"named", "a promotion does not cascade to the page", edit("REFERENCES entities(id, entity_type) ON UPDATE CASCADE,", "REFERENCES entities(id, entity_type),")},
	{"named", "a day page may be promoted", edit("CHECK (date(title) IS NOT title OR entity_type = 'page')", "CHECK (1)")},
	{"named", "ghost_pages lists the page of a person", edit("WHERE p.entity_type = 'page' AND p.body = ''", "WHERE p.body = ''")},
	{"named", "days-that-name does not follow a redirect", edit("UNION SELECT r.from_id FROM links r WHERE r.to_id = :entity_id AND r.kind = 'redirect')", ")")},
	{"named", "ghost_pages ignores redirects again", edit("AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id)\n", "AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id AND l.kind <> 'redirect')\n")},
	{"named", "a wikilink cannot land on a person", edit("('wikilink', 0, 'page,person,place', 'page,person,place',", "('wikilink', 0, 'page,person,place', 'page',")},
	{"named", "a page may claim an unknown type", edit("CHECK (entity_type IN ('page','person','place')),   -- 'page', or the named entity this page is", "CHECK (1),   -- 'page', or the named entity this page is")},
	{"pages", "the title CHECK lets the zero-width space through", edit("|| char(8203) || char(8206)", "|| char(8206)")},
	{"pages", "a device name before an extension is allowed", edit("upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)", "upper(title)")},
	{"pages", "the title index is not unique", edit("CREATE UNIQUE INDEX pages_title", "CREATE INDEX pages_title")},
	{"pages", "a page may have no title", edit("  title       TEXT NOT NULL,              -- filename-safe", "  title       TEXT,                       -- filename-safe")},
	{"pages", "pages_fts_update re-indexes on every update", edit("CREATE TRIGGER pages_fts_update AFTER UPDATE OF title, body ON pages BEGIN", "CREATE TRIGGER pages_fts_update AFTER UPDATE ON pages BEGIN")},
	{"pages", "the cookbook/save-a-body resolve scans by lower(title)", edit(" WHERE p.title_key = :key;", " WHERE lower(p.title) = :key;")},
	{"renames", "cookbook/rename-a-page leaves the typed links on the stub", edit("DELETE FROM links WHERE from_id = :old_id AND kind <> 'redirect';", "DELETE FROM links WHERE from_id = :old_id AND kind = 'wikilink';")},
	{"renames", "cookbook/rename-a-page overwrites a page with text that renames into a taken title", edit(" WHERE id = :old_id AND body IN ('', (SELECT body FROM pages WHERE id = :new_id));", " WHERE id = :old_id;")},
	{"renames", "cookbook/rename-a-page links the replacement to itself", edit(" AND kind NOT IN ('wikilink', 'redirect') AND to_id <> :new_id\n", " AND kind NOT IN ('wikilink', 'redirect')\n")},
	{"renames", "cookbook/rename-a-page drops the old page's day", edit("THEN :new_title ELSE day END, body", "THEN :new_title END, body")},
	{"renames", "cookbook/rename-a-page drops the notes of the links it moves", edit("SELECT :new_id, to_id, kind, note,", "SELECT :new_id, to_id, kind, NULL,")},
	{"links", "friend is not symmetric", edit("  ('friend',   1, 'person',", "  ('friend',   0, 'person',")},
	{"links", "at accepts any endpoint", edit("  ('at',       0, 'page',      'place',", "  ('at',       0, NULL,        NULL,")},
	{"links", "link kinds may change structure", edit("  WHEN NEW.symmetric IS NOT OLD.symmetric OR NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types\n", "  WHEN 0\n")},
	{"journal", "cookbook/where-was-i lists the places of a tombstoned day", edit("  FROM pages d\n  JOIN entities de ON de.id = d.id AND de.deleted_at IS NULL\n  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'\n  JOIN pages pl   ON pl.id = l.to_id\n  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE", "  FROM pages d\n  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'\n  JOIN pages pl   ON pl.id = l.to_id\n  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE")},
	{"links", "cookbook/inside-a-place lists a tombstoned place", edit("JOIN entities epl ON epl.id = pl.id AND epl.deleted_at IS NULL", "JOIN entities epl ON epl.id = pl.id")},
	{"links", "cookbook/inside-a-place walks with UNION ALL", edit("  SELECT :place_id\n  UNION\n", "  SELECT :place_id\n  UNION ALL\n")},
	{"facts", "a correction may be of another metric", edit("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE 0;")},
	{"facts", "measurement values may be infinite", edit("CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)", "CHECK (value IS NULL OR value = value)")},
	{"facts", "measurements may be updated", edit("CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements\nBEGIN\n", "CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements WHEN 0\nBEGIN\n")},
	{"journal", "cookbook/where-was-i says I was at a tombstoned place", edit("  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE d.title_key = :day", "  JOIN entities e ON e.id = pl.id\n WHERE d.title_key = :day")},
	{"journal", "a day page may have no day", edit("CONSTRAINT pages_day_page CHECK (date(title) IS NOT title OR day IS title)", "CONSTRAINT pages_day_page CHECK (1)")},
	{"journal", "pages_day_page compares the day with = instead of IS", edit("CHECK (date(title) IS NOT title OR day IS title)", "CHECK (date(title) IS NOT title OR day = title)")},
	{"journal", "cookbook/capture looks for the day page under another key", edit("JOIN entities e ON e.id = p.id WHERE p.title_key = '2026-09-29';", "JOIN entities e ON e.id = p.id WHERE p.title_key = 'today';")},
	{"journal", "cookbook/days-that-name lists a tombstoned day", edit("  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id IN", "  JOIN entities e ON e.id = d.id\n WHERE l.to_id IN")},
	{"journal", "cookbook/days-that-name forgets the about links", edit("l.kind IN ('wikilink', 'about')", "l.kind = 'wikilink'")},
	{"journal", "cookbook/days-that-name lists pages that are not days", edit("  JOIN pages d    ON d.id = l.from_id AND d.title = d.day\n  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id", "  JOIN pages d    ON d.id = l.from_id\n  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id")},
	{"habits", "two periods of one habit may overlap", edit("   WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id\n", "   WHERE 0 AND EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id\n")},
	{"habits", "an update may make periods overlap", edit("WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id", "WHERE 0 AND EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id")},
	{"habits", "a metric with a unit may be a habit", edit("   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';", "   WHERE 0;")},
	{"habits", "habit periods may be deleted", edit("CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods\nBEGIN", "CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods WHEN 0\nBEGIN")},
	{"habits", "habit_periods.source can change", edit("CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\n  WHEN NEW.source IS NOT OLD.source", "CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\n  WHEN 0")},
	{"habits", "a period may end before it starts", edit("CONSTRAINT habit_periods_order CHECK (end_day IS NULL OR end_day >= start_day)", "CONSTRAINT habit_periods_order CHECK (1)")},
	{"habits", "start_day is checked with = instead of IS", edit("CHECK (date(start_day) IS start_day)", "CHECK (date(start_day) = start_day)")},
	{"habits", "cookbook/habits counts a day not recorded as not done", edit("sum(s.value IS 0) AS not_done", "sum(s.value IS NOT 1) AS not_done")},
	{"habits", "cookbook/habits loses the idempotent re-run", edit("A re-sent period is idempotent: insert it with\n`ON CONFLICT(metric_id, start_day) DO NOTHING`", "A re-sent period is idempotent: insert it again")},
	{"habits", "cookbook/habits re-sends a period without its end_day", edit("carrying the `end_day` it was sent with", "as it is")},
	{"habits", "cookbook/day-view lists a habit outside its periods", edit("   WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day\n  UNION ALL", "   WHERE 1\n  UNION ALL")},
	{"journal", "cookbook/day-view shows superseded readings", edit("    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n   WHERE me.day = :day", "    FROM measurements me JOIN metrics m ON m.id = me.metric_id\n   WHERE me.day = :day")},
	{"writers", "contract/connections no longer sets trusted_schema = OFF", edit("PRAGMA trusted_schema = OFF;   -- the schema may call only side-effect-free functions (all of this one's are)\n", "")},
	{"integrity", "the orphan query counts a person's page as its domain row", edit("SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages WHERE entity_type IN ('page','place') UNION", "SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages UNION")},
	{"integrity", "contract/integrity-checks loses the FTS5 integrity-check", edit("INSERT INTO pages_fts(pages_fts, rank) VALUES ('integrity-check', 1);   -- no error\n", "")},
	{"imports", "the import block loses WHERE true", edit("  FROM s.staging WHERE true\n", "  FROM s.staging\n")},
	{"imports", "contract/imports copies life.db through a read-write shell", edit("sqlite3 -readonly life.db \"VACUUM INTO", "sqlite3 life.db \"VACUUM INTO")},
	{"evolution", "one CHECK is unnamed", edit("CONSTRAINT people_death_day_order CHECK", "CHECK")},
	{"cookbook", "cookbook/inside-a-place names a column that does not exist", edit("SELECT d.day, pl.title AS place", "SELECT d.day, pl.name AS place")},
	{"document", "a 2075 answer is gone from the file", edit("  ('sqlite',    'writers need SQLite >= 3.51.3", "  ('sqlite',    'writers need SQLite >= 3.51")},
	{"document", "a lifelog_meta key answers no question", edit("  ('evolution', 'after the freeze", "  ('orphan',    'x'),\n  ('evolution', 'after the freeze")},
	{"document", "cookbook/import-a-row-once bypasses the save contract", edit("run the link sync of [save a body](save-a-body.md)", "skip")},
	{"document", "the deletes row hides the registries", edit("the registries (metrics, link_kinds, lifelog_meta) are the owner''s administrative rows", "metrics, link_kinds and lifelog_meta are the owner''s administrative rows")},
	{"document", "the schema totals drift from the DDL", edit("**+ 24 triggers.**", "**+ 25 triggers.**")},
	{"diagrams", "a foreign key is not drawn", edit("    pages    ||--o| people   : \"id\"\n", "")},
	{"diagrams", "the link map invents an edge", edit("    place -->|\"located-in\"| place\n", "    place -->|\"located-in\"| place\n    person -->|\"mentioned\"| page\n")},
	{"diagrams", "a new link kind, the map unchanged", edit("  ('friend',   1, 'person',    'person',       NULL),", "  ('mentor',   0, 'person',    'person',       NULL),\n  ('friend',   1, 'person',    'person',       NULL),")},
	{"diagrams", "the correction story shows a wrong value", edit("state \"view shows 70.8\" as V2", "state \"view shows 70.9\" as V2")},
	{"diagrams", "a non-key column is drawn", edit("    link_kinds {\n        TEXT kind PK\n", "    link_kinds {\n        TEXT kind PK\n        INTEGER symmetric\n")},
	{"named", "the ark query lists a symmetric relation in both legs", edit(" AND l.kind NOT IN (SELECT kind FROM link_kinds WHERE symmetric = 1)", "")},
	{"facts", "metric-series has no upper bound", edit("me.day <= :day", "1")},
	{"facts", "metric-series includes the 91st day", edit("me.day > date(", "me.day >= date(")},
	{"named", "a promotion does not revive a tombstoned page", edit(", deleted_at = NULL   -- cascades", "   -- cascades")},
	{"named", "a promotion takes a redirect stub", edit("\n   AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = :ghost_id AND kind = 'redirect')", "")},
	{"dates", "entities.updated_at may lack milliseconds", nocheck("entities_updated_at")},
	{"dates", "a tombstone need not be an instant", nocheck("entities_deleted_at")},
	{"dates", "links.created_at need not be an instant", nocheck("links_created_at")},
	{"dates", "measurements.created_at need not be an instant", nocheck("measurements_created_at")},
	{"dates", "people.death_day need not round-trip", nocheck("people_death_day")},
	{"dates", "habit_periods.end_day need not round-trip", nocheck("habit_periods_end_day")},
	{"named", "a death may precede the birth", nocheck("people_death_day_order")},
	{"identity", "links.source may be anything", nocheck("links_source")},
	{"identity", "habit_periods.source may be anything", nocheck("habit_periods_source")},
	{"links", "link_kinds.symmetric may be 2", nocheck("link_kinds_symmetric")},
	{"facts", "a metric name may be registered twice", edit("name  TEXT NOT NULL UNIQUE,", "name  TEXT NOT NULL,")},
	{"pages", "a title key may hold an ASCII capital", nocheck("pages_key_folded")},
	{"pages", "a title may have leading or trailing space", edit("CHECK (title = trim(title) AND length(title) >= 1", "CHECK (length(title) >= 1")},
	{"pages", "a title may hold a NUL byte", edit("         AND instr(title, char(0)) = 0\n", "")},
	{"facts", "a correction of a row that does not exist passes with foreign_keys=OFF", edit("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) <> NEW.metric_id;")},
	{"links", "an unknown endpoint id passes as a place with foreign_keys=OFF", edit("coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), '?')", "coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), 'place')")},
	{"named", "ghost_pages lists a page younger than 30 days", edit("     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')\n", "")},
	{"named", "ghost_pages lists a tombstoned page", edit("   WHERE p.entity_type = 'page' AND p.body = '' AND e.deleted_at IS NULL", "   WHERE p.entity_type = 'page' AND p.body = ''")},
	{"facts", "cookbook/mood-over-time shows superseded readings", edit("  FROM measurement_values me\n  JOIN metrics m ON m.id = me.metric_id AND m.name = 'mood'", "  FROM measurements me\n  JOIN metrics m ON m.id = me.metric_id AND m.name = 'mood'")},
	{"facts", "cookbook/metric-series shows superseded readings", edit("  FROM measurement_values me\n  JOIN metrics m ON m.id = me.metric_id AND m.name = 'weight'", "  FROM measurements me\n  JOIN metrics m ON m.id = me.metric_id AND m.name = 'weight'")},
	{"writers", "the DDL does not mark the file as Lifelog", edit("PRAGMA application_id = 0x4C494645;", "PRAGMA application_id = 0;")},
	{"pages", "a title can change", notrigger("pages_title_fixed")},
	{"identity", "entities may be hard-deleted", notrigger("entities_no_delete")},
	{"identity", "pages may be hard-deleted", notrigger("pages_no_delete")},
	{"identity", "a link's endpoints can change", notrigger("links_fixed")},
	{"identity", "editing a person does not bump updated_at", notrigger("people_touch")},
	{"identity", "measurements.source may be anything", nocheck("measurements_source")},
	{"identity", "entities.entity_type may be anything", nocheck("entities_entity_type")},
	{"facts", "measurements may be deleted", notrigger("measurements_no_delete")},
	{"facts", "a metric's unit can change", notrigger("metrics_unit_fixed")},
	{"facts", "a reading may be corrected twice", edit("CREATE UNIQUE INDEX measurements_one_correction", "CREATE INDEX measurements_one_correction")},
	{"facts", "a measurement may be imported twice", edit("CREATE UNIQUE INDEX measurements_import", "CREATE INDEX measurements_import")},
	{"facts", "a reading may supersede itself", nocheck("measurements_not_self")},
	{"facts", "a first reading may have no value", nocheck("measurements_first_has_value")},
	{"facts", "a metric name may be upper case", nocheck("metrics_name")},
	{"facts", "measurement_values shows retractions", edit("   WHERE me.value IS NOT NULL\n     AND NOT EXISTS", "   WHERE NOT EXISTS")},
	{"dates", "measurements.tz may be anything", nocheck("measurements_tz")},
	{"dates", "measurements.day is checked with = instead of IS", edit("CONSTRAINT measurements_day CHECK (date(day) IS day)", "CONSTRAINT measurements_day CHECK (date(day) = day)")},
	{"dates", "people.birth_day is checked with = instead of IS", edit("CHECK (birth_day IS NULL OR date(birth_day) IS birth_day)", "CHECK (birth_day IS NULL OR date(birth_day) = birth_day)")},
	{"imports", "measurements.taken_at may be anything", nocheck("measurements_taken_at")},
	{"pages", "a pure-ASCII title may have any key", nocheck("pages_key_ascii")},
	{"pages", "a title may be 400 bytes", edit("length(CAST(title AS BLOB)) <= 240", "length(CAST(title AS BLOB)) <= 400")},
	{"links", "deleting a symmetric link leaves its mirror", notrigger("links_mirror_delete")},
	{"links", "an unregistered kind passes the trigger", edit("   WHERE NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);", "   WHERE 0 AND NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);")},
	{"links", "a link kind may be named in upper case", nocheck("link_kinds_kind")},
	{"links", "from_types may be malformed", nocheck("link_kinds_from_types")},
	{"links", "a symmetric kind may have different endpoint types", nocheck("link_kinds_mirror_valid")},
	{"habits", "one habit may start twice on a day", edit(",\n  UNIQUE (metric_id, start_day)", "")},
	{"habits", "an update may give a habit a unit", editNth("   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';", "   WHERE 0;", 1)},
	{"habits", "a period that starts where another starts is refused as an overlap", edit("                    AND p.start_day IS NOT NEW.start_day\n", "")},
	{"writers", "the DDL does not set WAL", edit("PRAGMA journal_mode  = WAL;", "PRAGMA journal_mode  = DELETE;")},
}

var (
	baselineMu sync.Mutex
	baseline   = map[string]string{}
)

// clean runs a suite on the unmutated document once: "" when every expectation is met.
func clean(t *testing.T, suite string) string {
	baselineMu.Lock()
	defer baselineMu.Unlock()
	if r, ok := baseline[suite]; ok {
		return r
	}
	s, _ := runSuite(suite, realDocs(), t.TempDir())
	r := ""
	if s.ok != s.n || s.n == 0 {
		r = fmt.Sprintf("%d/%d: %v", s.ok, s.n, s.fails)
	}
	baseline[suite] = r
	return r
}

// TestMutants runs every mutant against the suite that owns its rule (skipped with -short).
func TestMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("mutants: skipped with -short")
	}
	for _, m := range mutants {
		if r := clean(t, m.suite); r != "" {
			t.Fatalf("%s must pass on the unmutated document first: %s", m.suite, r)
		}
	}
	for i, m := range mutants {
		t.Run(fmt.Sprintf("%03d %s: %s", i+1, m.suite, m.name), func(t *testing.T) {
			t.Parallel()
			over, err := m.change.apply(realDocs())
			if err != nil {
				t.Fatal(err)
			}
			s, stopped := runSuite(m.suite, realDocs().with(over), t.TempDir())
			failed := s.n - s.ok
			if failed == 0 || (stopped != "" && failed == 1) {
				how := "every expectation met"
				if stopped != "" {
					how = "stopped only: " + clip(stopped, 120)
				}
				t.Errorf("MISSED: %s did not notice %q (%d/%d; %s)", m.suite, m.name, s.ok, s.n, how)
			}
		})
	}
	t.Logf("mutants: %d", len(mutants))
}
