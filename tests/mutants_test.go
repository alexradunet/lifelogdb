package tests

// A suite that cannot fail proves nothing. Each mutant is the docs tree with one rule broken — in schema.sql, in a
// cookbook block or in the text of a page — and the suite that owns the rule must notice: it must run to its end
// and fail its explicitly named rule witness (stopping on the broken document is not "noticed"). Every suite named
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
	suite, name, witness string
	change               change
}{
	{"dates", "a day CHECK uses = instead of IS", "pages.day rejects \"2026-9-3\"", edit("CONSTRAINT pages_day CHECK (day IS NULL OR date(day) IS day)", "CONSTRAINT pages_day CHECK (day IS NULL OR date(day) = day)")},
	{"dates", "instants lose their milliseconds", "measurements.created_at accepts a UTC instant with milliseconds", edit("CONSTRAINT measurements_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at)", "CONSTRAINT measurements_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%SZ', created_at) IS created_at)")},
	{"identity", "cookbook/capture attaches the mood with last_insert_rowid()", "cookbook/capture run literally, with a link sync inside: the mood reading points at the day page the RETURNING gave", edit("SELECT p.id, '2026-09-29', 4, 'ui', :page_id, strftime", "SELECT p.id, '2026-09-29', 4, 'ui', last_insert_rowid(), strftime")},
	{"identity", "entities.source may be NULL", "entities.source is TEXT NOT NULL with no default", edit("source      TEXT NOT NULL CONSTRAINT entities_source CHECK (length(source)", "source      TEXT CONSTRAINT entities_source CHECK (length(source)")},
	{"identity", "entities.source can change", "entities.source cannot change", edit("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.import_key IS NOT OLD.import_key")},
	{"identity", "entities.import_key can change", "entities.import_key cannot change", edit("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.source IS NOT OLD.source")},
	{"identity", "an entity may be imported twice", "a second entity with the same (source, import_key) is refused", edit("CREATE UNIQUE INDEX entities_import", "CREATE INDEX entities_import")},
	{"identity", "cookbook/import-a-row-once revives a tombstoned import", "cookbook/import-a-row-once: a tombstoned import is neither updated nor inserted again", edit("WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);", "WHERE source = 'import:vault' AND import_key = :import_key);")},
	{"identity", "the mirror drops the source", "the mirror of a symmetric link copies its source", edit("VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NEW.source);", "VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, 'ui');")},
	{"identity", "people may be hard-deleted", "DELETE FROM people is refused", edit("CREATE TRIGGER people_no_delete BEFORE DELETE ON people\nBEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;\n", "")},
	{"identity", "editing a page does not bump updated_at", "updating a page bumps entities.updated_at", edit("CREATE TRIGGER pages_touch AFTER UPDATE ON pages BEGIN\n", "CREATE TRIGGER pages_touch AFTER UPDATE ON pages WHEN 0 BEGIN\n")},
	{"identity", "the tombstone does not bump updated_at", "a tombstone bumps updated_at to the tombstone instant, and the trigger does not re-fire itself", edit("CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities\n  WHEN NEW.deleted_at IS NOT OLD.deleted_at", "CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities\n  WHEN 0")},
	{"links", "a redirect may not point at a person", "link redirect page->person (the replacement may be named): OK", edit("'page,person,place,metric,file', 'old stub page", "'page',         'old stub page")},
	{"links", "located-in accepts any endpoint", "link located-in place->person: ERR", edit("  ('located-in', 0, 'place',   'place',", "  ('located-in', 0, NULL,      NULL,")},
	{"named", "people hang off entities instead of their page", "a person without a page is refused (its FK points at pages)", edit("  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),\n  CONSTRAINT people_death_day_order", "  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),\n  CONSTRAINT people_death_day_order")},
	{"named", "a promotion does not cascade to the page", "a plain page is promoted: UPDATE entities.entity_type cascades to pages.entity_type, then the people row", edit("REFERENCES entities(id, entity_type) ON UPDATE CASCADE,", "REFERENCES entities(id, entity_type),")},
	{"named", "a day page may be promoted", "a day page cannot become a person or a place: the cascade meets pages_day_page_plain", edit("CHECK (date(title) IS NOT title OR entity_type = 'page')", "CHECK (1)")},
	{"named", "ghost_pages lists the page of a person", "ghost_pages lists the real ghost and none of the person or place pages, and not a page a rename points at", edit("WHERE p.entity_type = 'page' AND p.body = ''", "WHERE p.body = ''")},
	{"named", "days-that-name does not follow a redirect", "...and a day that wrote a misspelt name now redirected to him (one hop)", edit("UNION SELECT r.from_id FROM links r WHERE r.to_id = :entity_id AND r.kind = 'redirect')", ")")},
	{"named", "ghost_pages ignores redirects again", "ghost_pages lists the real ghost and none of the person or place pages, and not a page a rename points at", edit("AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id)\n", "AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id AND l.kind <> 'redirect')\n")},
	{"named", "a wikilink cannot land on a person", "the day page links to the person's own id, and no new page was made", edit("('wikilink', 0, 'page,person,place,metric,file', 'page,person,place,metric,file',", "('wikilink', 0, 'page,person,place,metric,file', 'page',")},
	{"named", "a page may claim an unknown type", "pages.entity_type is checked even on a connection without foreign keys (pages_entity_type)", edit("CHECK (entity_type IN ('page','person','place','metric','file')),   -- 'page', or the named entity this page is", "CHECK (1),   -- 'page', or the named entity this page is")},
	{"pages", "the title CHECK lets the zero-width space through", "U+200B in a title is rejected by the DB and by the writer", edit("|| char(8203) || char(8206)", "|| char(8206)")},
	{"pages", "a device name before an extension is allowed", "unsafe or device title \"con.txt\" rejected", edit("upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)", "upper(title)")},
	{"pages", "the title index is not unique", "DIET without a day collides with Diet with a day", edit("CREATE UNIQUE INDEX pages_title", "CREATE INDEX pages_title")},
	{"pages", "a page may have no title", "a page with a key but no title rejected", edit("  title       TEXT NOT NULL,              -- filename-safe", "  title       TEXT,                       -- filename-safe")},
	{"pages", "pages_fts_update re-indexes on every update", "a new day touches pages and entities only, not the index (pages_fts_update watches title and body)", edit("CREATE TRIGGER pages_fts_update AFTER UPDATE OF title, body ON pages BEGIN", "CREATE TRIGGER pages_fts_update AFTER UPDATE ON pages BEGIN")},
	{"pages", "the cookbook/save-a-body resolve scans by lower(title)", "...and is a SEARCH on pages_title", edit(" WHERE p.title_key = :key;", " WHERE lower(p.title) = :key;")},
	{"renames", "cookbook/rename-a-page leaves the typed links on the stub", "...the stub holds the one-line body and its redirect alone", edit("DELETE FROM links WHERE from_id = :old_id AND kind <> 'redirect';", "DELETE FROM links WHERE from_id = :old_id AND kind = 'wikilink';")},
	{"renames", "cookbook/rename-a-page overwrites a page with text that renames into a taken title", "...and the stub step alone overwrites no text: it changes no row of a page with text whose text is not on the target", edit(" WHERE id = :old_id AND body IN ('', (SELECT body FROM pages WHERE id = :new_id));", " WHERE id = :old_id;")},
	{"renames", "cookbook/rename-a-page links the replacement to itself", "...the ghost's typed links move to the person, and the one to the person itself goes, its mirror with it", edit(" AND kind NOT IN ('wikilink', 'redirect') AND to_id <> :new_id\n", " AND kind NOT IN ('wikilink', 'redirect')\n")},
	{"renames", "cookbook/rename-a-page drops the old page's day", "...the new page holds the old page's text and its day", edit("THEN :new_title ELSE day END, body", "THEN :new_title END, body")},
	{"renames", "cookbook/rename-a-page drops the notes of the links it moves", "...the new page starts the wikilinks of its text and the typed links, notes kept", edit("SELECT :new_id, to_id, kind, note,", "SELECT :new_id, to_id, kind, NULL,")},
	{"links", "friend is not symmetric", "a symmetric kind is stored in both directions", edit("  ('friend',   1, 'person',", "  ('friend',   0, 'person',")},
	{"links", "at accepts any endpoint", "link at person->place (at comes from a page): ERR", edit("  ('at',       0, 'page',      'place',", "  ('at',       0, NULL,        NULL,")},
	{"links", "link kinds may change structure", "link_kinds: symmetric is fixed (by the trigger: located-in could be symmetric by its CHECK)", edit("  WHEN NEW.symmetric IS NOT OLD.symmetric OR NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types\n", "  WHEN 0\n")},
	{"journal", "cookbook/where-was-i lists the places of a tombstoned day", "cookbook/where-was-i where was I on a tombstoned day: nowhere", edit("  FROM pages d\n  JOIN entities de ON de.id = d.id AND de.deleted_at IS NULL\n  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'\n  JOIN pages pl   ON pl.id = l.to_id\n  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE", "  FROM pages d\n  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'\n  JOIN pages pl   ON pl.id = l.to_id\n  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE")},
	{"links", "cookbook/inside-a-place lists a tombstoned place", "cookbook/inside-a-place asked about a tombstoned place itself lists nothing", edit("JOIN entities epl ON epl.id = pl.id AND epl.deleted_at IS NULL", "JOIN entities epl ON epl.id = pl.id")},
	{"links", "cookbook/inside-a-place walks with UNION ALL", "cookbook/inside-a-place terminates on a cycle (UNION)", edit("  SELECT :place_id\n  UNION\n", "  SELECT :place_id\n  UNION ALL\n")},
	{"facts", "a correction may be of another metric", "a correction of another metric is refused", edit("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE 0;")},
	{"facts", "measurement values may be infinite", "+Infinity refused", edit("CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)", "CHECK (value IS NULL OR value = value)")},
	{"facts", "measurements may be updated", "UPDATE of a value refused", edit("CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements\nBEGIN\n", "CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements WHEN 0\nBEGIN\n")},
	{"journal", "cookbook/where-was-i says I was at a tombstoned place", "...and a tombstoned place is not where I was", edit("  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE d.title_key = :day", "  JOIN entities e ON e.id = pl.id\n WHERE d.title_key = :day")},
	{"journal", "a day page may have no day", "a page titled with a day but no day is refused (pages_day_page)", edit("CONSTRAINT pages_day_page CHECK (date(title) IS NOT title OR day IS title)", "CONSTRAINT pages_day_page CHECK (1)")},
	{"journal", "pages_day_page compares the day with = instead of IS", "a page titled with a day but no day is refused (pages_day_page)", edit("CHECK (date(title) IS NOT title OR day IS title)", "CHECK (date(title) IS NOT title OR day = title)")},
	{"journal", "cookbook/capture looks for the day page under another key", "cookbook/capture finds the day page by its key, the day itself", edit("JOIN entities e ON e.id = p.id WHERE p.title_key = '2026-09-29';", "JOIN entities e ON e.id = p.id WHERE p.title_key = 'today';")},
	{"journal", "cookbook/days-that-name lists a tombstoned day", "...and drops a tombstoned day", edit("  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id IN", "  JOIN entities e ON e.id = d.id\n WHERE l.to_id IN")},
	{"journal", "cookbook/days-that-name forgets the about links", "...also a day that names her without brackets, by an about link; a day with both is listed once", edit("l.kind IN ('wikilink', 'about')", "l.kind = 'wikilink'")},
	{"journal", "cookbook/days-that-name lists pages that are not days", "cookbook/days-that-name lists the day pages that link Ana, newest first, and not an essay that names her", edit("  JOIN pages d    ON d.id = l.from_id AND d.title = d.day\n  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id", "  JOIN pages d    ON d.id = l.from_id\n  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id")},
	{"habits", "two periods of one habit may overlap", "an overlapping period refused", edit("   WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id\n", "   WHERE 0 AND EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id\n")},
	{"habits", "an update may make periods overlap", "moving a period onto another refused (update)", edit("WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id", "WHERE 0 AND EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id")},
	{"habits", "a metric with a unit may be a habit", "a period on a metric with a unit refused: a habit is 0/1", edit("   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';", "   WHERE 0;")},
	{"habits", "habit periods may be deleted", "a period is never deleted", edit("CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods\nBEGIN", "CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods WHEN 0\nBEGIN")},
	{"habits", "habit_periods.source can change", "a period's source never changes", edit("CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\n  WHEN NEW.source IS NOT OLD.source", "CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\n  WHEN 0")},
	{"habits", "a period may end before it starts", "an end before the start refused (habit_periods_order)", edit("CONSTRAINT habit_periods_order CHECK (end_day IS NULL OR end_day >= start_day)", "CONSTRAINT habit_periods_order CHECK (1)")},
	{"habits", "start_day is checked with = instead of IS", "start_day \"2026-9-3\" refused", edit("CHECK (date(start_day) IS start_day)", "CHECK (date(start_day) = start_day)")},
	{"habits", "cookbook/habits daily ignores tombstones", "cookbook/habits hides tombstoned daily rows", edit("FROM habit_periods h JOIN pages m ON m.id = h.metric_id\n  JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL", "FROM habit_periods h JOIN pages m ON m.id = h.metric_id\n  JOIN entities e ON e.id = m.id")},
	{"habits", "cookbook/habits completion ignores tombstones", "cookbook/habits hides tombstoned completion rows", edit("JOIN pages m ON m.id = a.metric_id\n  JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL", "JOIN pages m ON m.id = a.metric_id\n  JOIN entities e ON e.id = m.id")},
	{"habits", "cookbook/habits start ignores tombstones", "cookbook/habits start does not add a tombstoned metric", edit("SELECT p.id, :day, 'ui' FROM pages p JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL", "SELECT p.id, :day, 'ui' FROM pages p JOIN entities e ON e.id = p.id")},
	{"habits", "cookbook/habits stop ignores tombstones", "cookbook/habits stop does not change a tombstoned metric", edit("SELECT p.id FROM pages p JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL", "SELECT p.id FROM pages p JOIN entities e ON e.id = p.id")},
	{"journal", "cookbook/capture writes tombstoned Mood", "cookbook/capture does not write tombstoned Mood", edit(`FROM pages p JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL
 WHERE p.title_key = 'mood'`, `FROM pages p JOIN entities e ON e.id = p.id
 WHERE p.title_key = 'mood'`)},
	{"habits", "cookbook/habits counts a day not recorded as not done", "cookbook/habits completion counts only active days: done, not done, not recorded", edit("sum(s.value IS 0) AS not_done", "sum(s.value IS NOT 1) AS not_done")},
	{"habits", "cookbook/habits loses the idempotent re-run", "cookbook/habits documents the idempotent re-run", edit("A re-sent period is idempotent: insert it with\n`ON CONFLICT(metric_id, start_day) DO NOTHING`", "A re-sent period is idempotent: insert it again")},
	{"habits", "cookbook/habits re-sends a period without its end_day", "cookbook/habits says a re-sent period carries its end_day", edit("carrying the `end_day` it was sent with", "as it is")},
	{"habits", "cookbook/day-view lists a habit outside its periods", "...outside every period a 0/1 reading is just a reading, and no habit is listed", edit("   WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day\n  UNION ALL", "   WHERE 1\n  UNION ALL")},
	{"journal", "cookbook/day-view shows superseded readings", "...not another day's page or place, a link target, a person's page, a page of another day or the superseded reading", edit("    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id JOIN pages p ON p.id = m.id\n   WHERE me.day = :day", "    FROM measurements me JOIN metrics m ON m.id = me.metric_id JOIN pages p ON p.id = m.id\n   WHERE me.day = :day")},
	{"writers", "contract/connections no longer sets trusted_schema = OFF", "the contract/connections block sets every pragma the contract names", edit("PRAGMA trusted_schema = OFF;   -- the schema may call only side-effect-free functions (all of this one's are)\n", "")},
	{"integrity", "the orphan query counts a person's page as its domain row", "a person with a page but no people row: only the orphan query sees it", edit("SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages WHERE entity_type IN ('page','place') UNION", "SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages UNION")},
	{"integrity", "contract/integrity-checks loses the FTS5 integrity-check", "contract/integrity-checks has exactly the four checks: integrity_check, foreign_key_check, the orphan query, the FTS5 integrity-check", edit("INSERT INTO pages_fts(pages_fts, rank) VALUES ('integrity-check', 1);   -- no error\n", "")},
	{"imports", "the import block loses WHERE true", "the document's block loads 1 000 rows", edit("  FROM s.staging WHERE true\n", "  FROM s.staging\n")},
	{"imports", "contract/imports copies life.db through a read-write shell", "contract/imports step 1: the documented command opens the file read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO '/tmp/trial.db'", "sqlite3 life.db \"VACUUM INTO '/tmp/trial.db'")},
	{"imports", "contract/imports copies life.db through a reader that trusts the schema", "contract/imports step 1: the documented command opens the file read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO '/tmp/trial.db'", "sqlite3 -readonly life.db \"VACUUM INTO '/tmp/trial.db'")},
	{"snapshots", "the shell form of the snapshot trusts the schema", "the shell form of the snapshot opens life.db read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO 'life-", "sqlite3 -readonly life.db \"VACUUM INTO 'life-")},
	{"snapshots", "cookbook/take-a-snapshot takes it on the writer's connection", "the snapshot block runs on a connection that opened life.db read-only (mode=ro)", edit("-- on a connection to life.db opened read-only (mode=ro), while", "-- on the writer's connection to life.db, while")},
	{"snapshots", "the restore check opens the snapshot read-only, where the FTS5 check is refused", "the restore check opens the snapshot with the writer's connection settings", edit("open it with the writer's connection settings", "open it read-only (mode=ro)")},
	{"snapshots", "the restore leaves life.db in rollback-journal mode", "the restored life.db is in WAL mode again", edit("PRAGMA journal_mode = WAL;   -- once, on the restored life.db", "PRAGMA journal_mode = DELETE;   -- once, on the restored life.db")},
	{"evolution", "one CHECK is unnamed", "every CHECK in schema is named", edit("CONSTRAINT people_death_day_order CHECK", "CHECK")},
	{"cookbook", "cookbook/inside-a-place names a column that does not exist", "plain: all statements of the cookbook prepare", edit("SELECT d.day, pl.title AS place", "SELECT d.day, pl.name AS place")},
	{"document", "a 2075 answer is gone from the file", "Q18: the answer says [3.51.3 3.53]", edit("  ('sqlite',    'writers need SQLite >= 3.51.3", "  ('sqlite',    'writers need SQLite >= 3.51")},
	{"document", "a lifelog_meta key answers no question", "every lifelog_meta key answers some question (no rule without a question)", edit("  ('evolution', 'after the freeze", "  ('orphan',    'x'),\n  ('evolution', 'after the freeze")},
	{"document", "cookbook/import-a-row-once bypasses the save contract", "import-a-row-once sends an imported body through the save contract (D19)", edit("run the link sync of [save a body](save-a-body.md)", "skip")},
	{"document", "the deletes row hides the registries", "Q6: the answer says [tombstone links registries]", edit("the registries (link_kinds, lifelog_meta) are the owner''s administrative rows", "link_kinds and lifelog_meta are the owner''s administrative rows")},
	{"document", "the schema totals drift from the DDL", "the totals in schema/README.md match the DDL (tables, views, triggers)", edit("**+ 29 triggers.**", "**+ 30 triggers.**")},
	{"diagrams", "a foreign key is not drawn", "every foreign key is drawn as `parent --- child : first FK column`", edit("    pages    ||--o| people   : \"id\"\n", "")},
	{"diagrams", "the link map invents an edge", "the link map has the same edges as link_kinds", edit("    place -->|\"located-in\"| place\n", "    place -->|\"located-in\"| place\n    person -->|\"mentioned\"| page\n")},
	{"diagrams", "a new link kind, the map unchanged", "the link map has the same edges as link_kinds", edit("  ('friend',   1, 'person',    'person',       NULL),", "  ('mentor',   0, 'person',    'person',       NULL),\n  ('friend',   1, 'person',    'person',       NULL),")},
	{"diagrams", "the correction story shows a wrong value", "executed, the view shows what each state of the diagram says", edit("state \"view shows 70.8\" as V2", "state \"view shows 70.9\" as V2")},
	{"diagrams", "a non-key column is drawn", "er-core.link_kinds.symmetric is a key column (the diagrams draw keys only)", edit("    link_kinds {\n        TEXT kind PK\n", "    link_kinds {\n        TEXT kind PK\n        INTEGER symmetric\n")},
	{"named", "the ark query lists a symmetric relation in both legs", "...and a friend once, not twice", edit(" AND l.kind NOT IN (SELECT kind FROM link_kinds WHERE symmetric = 1)", "")},
	{"facts", "metric-series has no upper bound", "cookbook/metric-series: the 90 days ending on :day, none after it", edit("me.day <= :day", "1")},
	{"facts", "metric-series includes the 91st day", "cookbook/metric-series: the 90 days ending on :day, none after it", edit("me.day > date(", "me.day >= date(")},
	{"named", "a promotion does not revive a tombstoned page", "a tombstoned ghost is promoted and revived", edit(", deleted_at = NULL   -- cascades", "   -- cascades")},
	{"named", "a promotion takes a redirect stub", "a redirect stub is not promoted: the UPDATE changes no row and the people insert fails", edit("\n   AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = :ghost_id AND kind = 'redirect')", "")},
	{"dates", "entities.updated_at may lack milliseconds", "entities.updated_at without milliseconds refused", nocheck("entities_updated_at")},
	{"dates", "a tombstone need not be an instant", "a tombstone that is not an instant refused", nocheck("entities_deleted_at")},
	{"dates", "links.created_at need not be an instant", "links.created_at must be an instant", nocheck("links_created_at")},
	{"dates", "measurements.created_at need not be an instant", "measurements.created_at must be an instant", nocheck("measurements_created_at")},
	{"dates", "people.death_day need not round-trip", "people.death_day must round-trip", nocheck("people_death_day")},
	{"dates", "habit_periods.end_day need not round-trip", "habit_periods.end_day must round-trip", nocheck("habit_periods_end_day")},
	{"named", "a death may precede the birth", "a death before the birth is refused", nocheck("people_death_day_order")},
	{"identity", "links.source may be anything", "links.source takes the same GLOB as entities.source", nocheck("links_source")},
	{"links", "link_kinds.symmetric may be 2", "link_kinds.symmetric is 0 or 1", nocheck("link_kinds_symmetric")},
	{"identity", "habit_periods.source may be anything", "habit_periods.source takes the same GLOB as entities.source", nocheck("habit_periods_source")},
	{"pages", "a title key may hold an ASCII capital", "a non-ASCII title's key may not hold an ASCII capital", nocheck("pages_key_folded")},
	{"pages", "a title may have leading or trailing space", "a title with leading space refused (a key the writer folded to match)", edit("CHECK (title = trim(title) AND length(title) >= 1", "CHECK (length(title) >= 1")},
	{"pages", "a title may hold a NUL byte", "a title with a NUL byte refused", edit("         AND instr(title, char(0)) = 0\n", "")},
	{"facts", "a correction of a row that does not exist passes with foreign_keys=OFF", "with foreign_keys=OFF, a correction of a row that does not exist is refused by the trigger", edit("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) <> NEW.metric_id;")},
	{"links", "an unknown endpoint id passes as a place with foreign_keys=OFF", "with foreign_keys=OFF, a typed link to an id that does not exist is refused", edit("coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), '?')", "coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), 'place')")},
	{"named", "ghost_pages lists a page younger than 30 days", "ghost_pages leaves a page younger than 30 days alone", edit("     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')\n", "")},
	{"named", "ghost_pages lists a tombstoned page", "ghost_pages leaves a tombstoned page alone", edit("   WHERE p.entity_type = 'page' AND p.body = '' AND e.deleted_at IS NULL", "   WHERE p.entity_type = 'page' AND p.body = ''")},
	{"facts", "cookbook/mood-over-time shows superseded readings", "cookbook/mood-over-time skips superseded and retracted readings", edit("  FROM measurement_values me\n  JOIN pages m ON m.id = me.metric_id AND m.title_key = 'mood'", "  FROM measurements me\n  JOIN pages m ON m.id = me.metric_id AND m.title_key = 'mood'")},
	{"facts", "cookbook/metric-series shows superseded readings", "cookbook/metric-series skips superseded and retracted readings", edit("  FROM measurement_values me\n  JOIN pages m ON m.id = me.metric_id AND m.title_key = 'weight'", "  FROM measurements me\n  JOIN pages m ON m.id = me.metric_id AND m.title_key = 'weight'")},
	{"facts", "measurements may be deleted", "DELETE refused", notrigger("measurements_no_delete")},
	{"writers", "the DDL does not mark the file as Lifelog", "the DDL marks the file as Lifelog: application_id 0x4C494645 and user_version 1", edit("PRAGMA application_id = 0x4C494645;", "PRAGMA application_id = 0;")},
	{"pages", "a title can change", "a title cannot change", notrigger("pages_title_fixed")},
	{"identity", "entities may be hard-deleted", "an orphan entities row cannot be deleted either (only the entities trigger can stop it)", notrigger("entities_no_delete")},
	{"identity", "pages may be hard-deleted", "DELETE FROM pages is refused", notrigger("pages_no_delete")},
	{"identity", "a link's endpoints can change", "a link's endpoints cannot change", notrigger("links_fixed")},
	{"identity", "editing a person does not bump updated_at", "updating a person bumps entities.updated_at", notrigger("people_touch")},
	{"identity", "measurements.source may be anything", "source  rejected on entities and measurements", nocheck("measurements_source")},
	{"identity", "entities.entity_type may be anything", "entities.entity_type rejects an unknown type", nocheck("entities_entity_type")},
	{"facts", "a metrics row may name a page that is not a metric", "...and cannot claim another type to hang off a person's page", nocheck("metrics_entity_type")},
	{"facts", "a metric's unit can change", "metrics.unit cannot change", notrigger("metrics_unit_fixed")},
	{"facts", "a reading may be corrected twice", "a second correction of the same row is refused", edit("CREATE UNIQUE INDEX measurements_one_correction", "CREATE INDEX measurements_one_correction")},
	{"facts", "a measurement may be imported twice", "a duplicate measurement import is refused by its unique index", edit("CREATE UNIQUE INDEX measurements_import", "CREATE INDEX measurements_import")},
	{"facts", "a reading may supersede itself", "a row cannot supersede itself", nocheck("measurements_not_self")},
	{"facts", "a first reading may have no value", "a first reading with NULL value is refused", nocheck("measurements_first_has_value")},
	{"facts", "a metrics row may hang off a page that is not a metric", "a metrics row needs a page of type metric", edit("  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type)\n) STRICT;\nCREATE TRIGGER metrics_unit_fixed", "  FOREIGN KEY (id) REFERENCES pages(id)\n) STRICT;\nCREATE TRIGGER metrics_unit_fixed")},
	{"identity", "metrics may be hard-deleted", "DELETE FROM metrics is refused", notrigger("metrics_no_delete")},
	{"integrity", "the orphan query counts a metric's page as its domain row", "a metric with a page but no metrics row: only the orphan query sees it", edit("UNION SELECT id FROM people UNION SELECT id FROM metrics UNION SELECT id FROM files);", "UNION SELECT id FROM people UNION SELECT id FROM pages WHERE entity_type = 'metric' UNION SELECT id FROM files);")},
	{"facts", "a category may be a person", "...but a category is a plain page: never a person or a metric", edit("  ('part-of',  0, NULL,        'page',", "  ('part-of',  0, NULL,        'page,person',")},
	{"facts", "cookbook/metrics-by-category walks no deeper than the category itself", "cookbook/metrics-by-category: a category holds the metrics of every category under it", edit("  SELECT l.from_id FROM links l JOIN tree t ON l.to_id = t.id AND l.kind = 'part-of'\n", "  SELECT l.from_id FROM links l JOIN tree t ON l.to_id = t.id AND l.kind = 'part-of' AND 0\n")},
	{"facts", "cookbook/metrics-by-category files metrics in a deleted category", "cookbook/metrics-by-category: a deleted category's metrics read as filed nowhere", edit("               JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL\n              WHERE l.kind = 'part-of') c", "               JOIN entities e ON e.id = p.id\n              WHERE l.kind = 'part-of') c")},
	{"renames", "a rename leaves the typed links that end at the old page", "E: a category's page is renamed; its metrics are filed in the new page", edit("DELETE FROM links WHERE to_id = :old_id AND kind NOT IN ('wikilink', 'redirect');", "DELETE FROM links WHERE 0;")},
	{"facts", "measurement_values shows retractions", "the tap and its retraction are hidden", edit("   WHERE me.value IS NOT NULL\n     AND NOT EXISTS", "   WHERE NOT EXISTS")},
	{"dates", "measurements.tz may be anything", "measurements.tz \"\" rejected", nocheck("measurements_tz")},
	{"dates", "measurements.day is checked with = instead of IS", "measurements.day rejects \"2026-9-3\"", edit("CONSTRAINT measurements_day CHECK (date(day) IS day)", "CONSTRAINT measurements_day CHECK (date(day) = day)")},
	{"dates", "people.birth_day is checked with = instead of IS", "people.birth_day rejects \"2026-9-3\"", edit("CHECK (birth_day IS NULL OR date(birth_day) IS birth_day)", "CHECK (birth_day IS NULL OR date(birth_day) = birth_day)")},
	{"imports", "measurements.taken_at may be anything", "without NULLIF an empty taken_at ('' is not NULL) fails its CHECK", nocheck("measurements_taken_at")},
	{"pages", "a pure-ASCII title may have any key", "ASCII title with a wrong key rejected", nocheck("pages_key_ascii")},
	{"pages", "a title may be 400 bytes", "241 bytes rejected (é × 121 = 242 bytes)", edit("length(CAST(title AS BLOB)) <= 240", "length(CAST(title AS BLOB)) <= 400")},
	{"links", "deleting a symmetric link leaves its mirror", "deleting one side of a symmetric edge deletes its mirror", notrigger("links_mirror_delete")},
	{"links", "an unregistered kind passes the trigger", "with foreign_keys=OFF an unregistered kind is still refused (the trigger, in autocommit)", edit("   WHERE NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);", "   WHERE 0 AND NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);")},
	{"links", "a link kind may be named in upper case", "a kind name in upper case is refused", nocheck("link_kinds_kind")},
	{"links", "from_types may be malformed", "a malformed type list is refused", nocheck("link_kinds_from_types")},
	{"links", "a symmetric kind may have different endpoint types", "a symmetric kind with different endpoint types is refused", nocheck("link_kinds_mirror_valid")},
	{"habits", "one habit may start twice on a day", "the same start twice is refused by UNIQUE", edit(",\n  UNIQUE (metric_id, start_day)", "")},
	{"habits", "an update may give a habit a unit", "moving a period to a metric with a unit refused (update)", editNth("   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';", "   WHERE 0;", 1)},
	{"habits", "a period that starts where another starts is refused as an overlap", "the same start twice is refused by UNIQUE", edit("                    AND p.start_day IS NOT NEW.start_day\n", "")},
	{"writers", "the DDL does not set WAL", "journal_mode=WAL is stored in the file by the DDL", edit("PRAGMA journal_mode  = WAL;", "PRAGMA journal_mode  = DELETE;")},
	{"writers", "contract/connections lets readers keep trusted_schema on", "contract/connections: readers are read-only and set trusted_schema = OFF", edit("Readers must be **read-only** and set **`PRAGMA trusted_schema = OFF`** per connection, as writers do.", "Readers must be **read-only**.")},
	{"pages", "the Cn rule names a Unicode version the writer does not pin", "the Cn rule names one Unicode version, the one the writer pins", edit("**Unicode 15.0** has not", "**Unicode 16.0** has not")},
	{"pages", "the Cn rule accepts a code point Unicode 15.0 has not assigned", "the Cn rule lists code points a writer refuses and accepts", edit("and `U+1FAE9` (16.0) in a title, and accepts `U+1FAE8` (15.0)", "in a title, and accepts `U+1FAE8` (15.0) and `U+1FAE9` (16.0)")},
	{"files", "files.sha256 may be anything", "sha256 AAAAAAAAAAAA… refused", nocheck("files_sha256")},
	{"files", "files.mime may be anything", "mime Image/JPEG refused", nocheck("files_mime")},
	{"files", "files_preview compares with = and lets an empty preview through", "preview: an empty blob refused", edit("substr(preview, 1, 3) IS x'FFD8FF'", "substr(preview, 1, 3) = x'FFD8FF'")},
	{"files", "a preview need not be a JPEG", "preview: a PNG refused", edit("(substr(preview, 1, 3) IS x'FFD8FF'\n", "(1\n")},
	{"files", "a preview may be 2 MB", "preview: 1 MB accepted, 1 MB and a byte refused", edit("AND length(preview) <= 1048576", "AND length(preview) <= 2097152")},
	{"files", "one original may be kept twice", "one page per original: a second files row with the same sha256 is refused (files_sha256)", edit("CREATE UNIQUE INDEX files_sha256", "CREATE INDEX files_sha256")},
	{"files", "the hash and type of an original can change", "files.sha256 cannot change", notrigger("files_original_fixed")},
	{"files", "adding a preview does not bump updated_at", "adding it bumps entities.updated_at (files_touch)", notrigger("files_touch")},
	{"files", "a files row may hang off a page that is not a file", "a files row needs a page of type file", edit("  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type)\n) STRICT;\nCREATE UNIQUE INDEX files_sha256", "  FOREIGN KEY (id) REFERENCES pages(id)\n) STRICT;\nCREATE UNIQUE INDEX files_sha256")},
	{"files", "a files row may claim another type", "...and cannot claim another type to hang off a person's page", nocheck("files_entity_type")},
	{"files", "an embed cannot land on a file page", "an embed in a day page lands on the file page (a wikilink)", edit("('wikilink', 0, 'page,person,place,metric,file', 'page,person,place,metric,file',", "('wikilink', 0, 'page,person,place,metric,file', 'page,person,place,metric',")},
	{"files", "a redirect may not point at a file page", "a redirect stub may point at a file page (a rename into its title)", edit("'page,person,place,metric,file', 'old stub page", "'page,person,place,metric', 'old stub page")},
	{"files", "cookbook/keep-a-file finds an original only under its own source", "the same original again, from another source: step 0 finds the page, and nothing is written", edit(" WHERE f.sha256 = :sha256;", " WHERE f.sha256 = :sha256 AND e.source = :source;")},
	{"files", "cookbook/keep-a-file replaces a preview a file already has", "...and an existing preview is never replaced", edit("WHERE sha256 = :sha256 AND preview IS NULL AND :preview IS NOT NULL", "WHERE sha256 = :sha256 AND :preview IS NOT NULL")},
	{"files", "cookbook/keep-a-file fills the preview of a tombstoned file", "a tombstoned file is left alone: no preview is filled", edit("\n   AND id IN (SELECT id FROM entities WHERE deleted_at IS NULL);", ";")},
	{"files", "cookbook/keep-a-file leaves a promoted page without its missing day", "promotion missing day: recipe and writer agree, filling only a missing day", edit("UPDATE pages SET day = :day WHERE id = :file_id AND entity_type = 'file' AND day IS NULL;", "UPDATE pages SET day = day WHERE id = :file_id AND entity_type = 'file' AND day IS NULL;")},
	{"files", "cookbook/keep-a-file promotes a redirect stub", "a redirect stub is never promoted", edit("\n   AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = :file_id AND kind = 'redirect');", ";")},
	{"files", "cookbook/keep-a-file overwrites the text of the page it promotes", "a page with text becomes the file with its text kept; :body fills only an empty body", edit("WHERE id = :file_id AND entity_type = 'file' AND body = '';", "WHERE id = :file_id AND entity_type = 'file';")},
	{"identity", "files may be hard-deleted", "DELETE FROM files is refused", notrigger("files_no_delete")},
	{"integrity", "the orphan query forgets files", "a clean file with a row of every type: integrity ok, foreign_key_check empty, orphan query empty", edit("UNION SELECT id FROM metrics UNION SELECT id FROM files);", "UNION SELECT id FROM metrics);")},
	{"places", "a place's latitude may be anything", "a point at (91, 0.5) is refused", nocheck("places_lat")},
	{"places", "a place's longitude may be anything", "a point at (10, 180.5) is refused", nocheck("places_lon")},
	{"places", "a radius may be any size", "a radius of 9 m is refused", nocheck("places_radius")},
	{"places", "link_days may be anything", "link_days is 0 or 1", nocheck("places_link_days")},
	{"places", "a place may sit at 0°, 0°", "a point at (0, 0) is refused", nocheck("places_not_null_island")},
	{"places", "a places row may claim another type", "...and cannot claim another type", nocheck("places_entity_type")},
	{"places", "a places row may hang off a page that is not a place", "a places row needs a page of type place", edit("  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),\n  CONSTRAINT places_not_null_island", "  FOREIGN KEY (id) REFERENCES pages(id),\n  CONSTRAINT places_not_null_island")},
	{"places", "a point may be deleted", "a point is never deleted", notrigger("places_no_delete")},
	{"places", "fixing a point does not bump updated_at", "...which bumps entities.updated_at (places_touch)", notrigger("places_touch")},
	{"places", "cookbook/place-of-a-photo moves a point a second answer gives", "cookbook/place-of-a-photo: a place's point is given once; a second answer never moves it", edit("ON CONFLICT(id) DO NOTHING;\n```\n\n**Match", "ON CONFLICT(id) DO UPDATE SET lat = excluded.lat, lon = excluded.lon, radius_m = excluded.radius_m, link_days = excluded.link_days;\n```\n\n**Match")},
	{"places", "cookbook/place-of-a-photo matches the nearest place, not the smallest circle", "the match is the haversine answer for all 400 positions among 60 live places of random radii (within 1 % of a radius, either answer)", edit(" ORDER BY radius_m, d2\n", " ORDER BY d2\n")},
	{"places", "cookbook/place-of-a-photo matches a place whatever the distance", "a position in Lisbon, outside the café: Lisbon", edit(" WHERE d2 <= radius_m * radius_m\n", " WHERE 1\n")},
	{"places", "cookbook/place-of-a-photo ignores the latitude's metres per degree", "and its distance is within 0.5 % of the great-circle distance", edit("((pl.lon - :lon) * :m_per_deg_lon) * ((pl.lon - :lon) * :m_per_deg_lon) AS d2", "((pl.lon - :lon) * 111320.0) * ((pl.lon - :lon) * 111320.0) AS d2")},
	{"places", "cookbook/place-of-a-photo matches a tombstoned place", "a tombstoned place is never the answer", edit("          JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL)\n WHERE d2", "          JOIN entities e ON e.id = pl.id)\n WHERE d2")},
	{"places", "cookbook/place-of-a-photo ignores the parsed append decision", "cookbook parsed embed binding preserves aliases but appends after literal code/plain links", edit(" WHERE id = :photo_day_id AND :append_embed = 1;", " WHERE id = :photo_day_id AND instr(body, '![[' || :file_title || ']]') = 0;")},
	{"places", "cookbook/place-of-a-photo shows a photo twice in its day", "...and a second photo of that day at that place, or the same photo again, adds no link and no second embed", edit(" WHERE id = :photo_day_id AND :append_embed = 1;", " WHERE id = :photo_day_id;")},
	{"doc-save-contract", "an embed makes no link", "A every printed vector reproduces with the writer's extraction", edit("| `![[Lake.jpg]] and ![[Lake.jpg\\|a lake]]` | `Lake.jpg` |", "| `![[Lake.jpg]] and ![[Lake.jpg\\|a lake]]` | — |")},
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
	s, stopped := runSuite(suite, realDocs(), t.TempDir())
	r := ""
	if stopped != "" || s.ok != s.n || s.n == 0 {
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
		if m.witness == "" {
			t.Fatalf("%s: missing intended assertion witness", m.name)
		}
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
			if !mutantKilled(s, stopped, m.witness) {
				how := fmt.Sprintf("intended assertion %q did not fail; failed labels: %q", m.witness, s.failedLabels)
				if stopped != "" {
					how = "stopped only: " + clip(stopped, 120)
				}
				t.Errorf("MISSED: %s did not notice %q (%d/%d; %s)", m.suite, m.name, s.ok, s.n, how)
			}
		})
	}
	t.Logf("mutants: %d", len(mutants))
}

// mutantKilled matches the exact expectation label, never formatted diagnostics.
// A setup stop invalidates even an earlier intended failure.
func mutantKilled(s *S, stopped, witness string) bool {
	return witness != "" && stopped == "" && contains(s.failedLabels, witness)
}

func TestMutantWitness(t *testing.T) {
	t.Run("diagnostics are not identities", func(t *testing.T) {
		s := &S{}
		s.K("unrelated", false, "intended")
		if mutantKilled(s, "", "intended") || !contains(s.failedLabels, "unrelated") {
			t.Fatal("formatted details credited as an assertion identity")
		}
	})
	for _, tc := range []struct {
		name             string
		failures         []string
		stopped, witness string
		want             bool
	}{
		{"unrelated failure", []string{"unrelated"}, "", "intended", false},
		{"intended failure", []string{"intended"}, "", "intended", true},
		{"stopped suite", []string{"intended", "other"}, "setup failed", "intended", false},
		{"missing witness", []string{"intended"}, "", "", false},
		{"no failure", nil, "", "intended", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &S{n: len(tc.failures), failedLabels: tc.failures}
			if got := mutantKilled(s, tc.stopped, tc.witness); got != tc.want {
				t.Errorf("kill=%v want %v", got, tc.want)
			}
		})
	}
	t.Run("unchanged mutation", func(t *testing.T) {
		over, err := edit("CONSTRAINT pages_day CHECK", "CONSTRAINT pages_day CHECK").apply(realDocs())
		if err != nil {
			t.Fatal(err)
		}
		s, stopped := runSuite("dates", realDocs().with(over), t.TempDir())
		if stopped != "" || s.ok != s.n || s.n == 0 {
			t.Fatalf("no-op baseline: %d/%d stopped=%s failures=%v", s.ok, s.n, stopped, s.fails)
		}
		if mutantKilled(s, stopped, `pages.day rejects "banana"`) {
			t.Fatal("unchanged mutation credited")
		}
	})
}
