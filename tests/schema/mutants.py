"""A suite that cannot fail proves nothing. Each mutant is a copy of SCHEMA.md with one rule broken — in the DDL, in a
cookbook block or in the text — and the suite that owns the rule must notice: it must run to its end and report at least
one failed expectation (a crash is not 'noticed'). The unmutated document must pass every suite named here first."""
import os, re, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
doc = docsql.doc_text()

def mutate(old, new, nth=0):
    n = doc.count(old)
    assert n > nth, f'mutation target found {n}x: {old[:70]!r}'
    i = -1
    for _ in range(nth + 1): i = doc.index(old, i + 1)
    return doc[:i] + new + doc[i + len(old):]

ORPHAN = "SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages WHERE entity_type = 'page' UNION"
MUTANTS = [   # (suite, what is broken, the broken document)
 ('dates', 'a day CHECK uses = instead of IS', mutate("CHECK (date(start_day) IS start_day)", "CHECK (date(start_day) = start_day)")),
 ('dates', 'instants lose their milliseconds', mutate("CONSTRAINT entities_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at)", "CONSTRAINT entities_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%SZ', created_at) IS created_at)")),
 ('identity', '§6.1 attaches the mood with last_insert_rowid()', mutate("SELECT id, '2026-09-29', 4, 'ui', :memo_id, strftime", "SELECT id, '2026-09-29', 4, 'ui', last_insert_rowid(), strftime")),
 ('identity', 'events lose their composite foreign key', mutate("  notes       TEXT,\n  FOREIGN KEY (id, entity_type) REFERENCES entities(id, type),", "  notes       TEXT,")),
 ('identity', 'entities.source may be NULL', mutate("source     TEXT NOT NULL CONSTRAINT entities_source CHECK (length(source)", "source     TEXT CONSTRAINT entities_source CHECK (length(source)")),
 ('identity', 'entities.source can change', mutate("  WHEN NEW.source IS NOT OLD.source OR NEW.import_id IS NOT OLD.import_id", "  WHEN NEW.import_id IS NOT OLD.import_id")),
 ('identity', 'entities.import_id can change', mutate("  WHEN NEW.source IS NOT OLD.source OR NEW.import_id IS NOT OLD.import_id", "  WHEN NEW.source IS NOT OLD.source")),
 ('identity', 'an entity may be imported twice', mutate('CREATE UNIQUE INDEX entities_import', 'CREATE INDEX entities_import')),
 ('identity', '§6.22 revives a tombstoned import', mutate("WHERE source = 'import:calendar' AND import_id = :import_id AND deleted_at IS NULL);", "WHERE source = 'import:calendar' AND import_id = :import_id);")),
 ('identity', 'the mirror drops the source', mutate("VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NEW.source);", "VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, 'ui');")),
 ('identity', 'tasks may be hard-deleted', mutate("CREATE TRIGGER tasks_no_delete BEFORE DELETE ON tasks\nBEGIN SELECT RAISE(ABORT, 'tasks are never deleted: tombstone the entity (entities.deleted_at)'); END;\n", "")),
 ('identity', 'the tombstone does not bump updated_at', mutate("CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities\n  WHEN NEW.deleted_at IS NOT OLD.deleted_at", "CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities\n  WHEN 0")),
 ('links', 'anything may be is-a a page', mutate("  ('is-a',     0, 'event',     'page',", "  ('is-a',     0, NULL,        'page',")),
 ('links', '§6.21 lists a tombstoned event under its kind', mutate("  JOIN entities e ON e.id = ev.id AND e.deleted_at IS NULL\n WHERE l.to_id = :kind_id", "  JOIN entities e ON e.id = ev.id\n WHERE l.to_id = :kind_id")),
 ('links', '§6.21 counts under a tombstoned kind page', mutate("  JOIN entities k ON k.id = pg.id AND k.deleted_at IS NULL\n", "")),
 ('named', 'people hang off entities instead of their page', mutate("  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),\n  CONSTRAINT people_death_order", "  FOREIGN KEY (id, entity_type) REFERENCES entities(id, type),\n  CONSTRAINT people_death_order")),
 ('named', 'a promotion does not cascade to the page', mutate("REFERENCES entities(id, type) ON UPDATE CASCADE,", "REFERENCES entities(id, type),")),
 ('named', 'a memo may be the page of a person', mutate("CONSTRAINT pages_named_titled CHECK (entity_type = 'page' OR kind = 'page'),", "CONSTRAINT pages_named_titled CHECK (1),")),
 ('named', 'ghost_pages lists the page of a person', mutate("WHERE p.kind = 'page' AND p.entity_type = 'page' AND p.body = ''", "WHERE p.kind = 'page' AND p.body = ''")),
 ('named', 'a wikilink cannot land on a person', mutate("('wikilink', 0, 'page,person,place,holding', 'page,person,place,holding',", "('wikilink', 0, 'page,person,place,holding', 'page',")),
 ('named', '§6.19 lists wiki pages as memos', mutate("  JOIN pages m     ON m.id = l.from_id AND m.kind = 'memo'\n", "  JOIN pages m     ON m.id = l.from_id\n")),
 ('named', 'places get a unique name again', mutate("  entity_type TEXT NOT NULL DEFAULT 'place' CONSTRAINT places_entity_type CHECK (entity_type = 'place'),\n", "  entity_type TEXT NOT NULL DEFAULT 'place' CONSTRAINT places_entity_type CHECK (entity_type = 'place'),\n  name        TEXT UNIQUE COLLATE NOCASE,\n")),
 ('pages', 'the title CHECK lets the zero-width space through', mutate("|| char(8203) || char(8206)", "|| char(8206)")),
 ('pages', 'a device name before an extension is allowed', mutate("upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)", "upper(title)")),
 ('pages', 'the title index is not unique', mutate('CREATE UNIQUE INDEX pages_title', 'CREATE INDEX pages_title')),
 ('pages', 'every page needs a day', mutate("CHECK (kind = 'page' OR day IS NOT NULL),", "CHECK (day IS NOT NULL),")),
 ('pages', 'pages_kind_fixed never fires', mutate('WHEN NEW.kind IS NOT OLD.kind', 'WHEN 0')),
 ('pages', 'pages_fts_au re-indexes on every update', mutate('CREATE TRIGGER pages_fts_au AFTER UPDATE OF title, body ON pages BEGIN', 'CREATE TRIGGER pages_fts_au AFTER UPDATE ON pages BEGIN')),
 ('pages', 'the §6.13 resolve has a kind predicate', mutate(" WHERE p.title_key = :key;", " WHERE p.kind = 'page' AND lower(p.title) = :key;")),
 ('links', 'friend is not symmetric', mutate("  ('friend',   1, 'person',", "  ('friend',   0, 'person',")),
 ('links', 'attended accepts any endpoint', mutate("  ('attended', 0, 'person',    'event',", "  ('attended', 0, NULL,        NULL,")),
 ('links', 'link kinds may change structure', mutate("  WHEN NEW.symmetric IS NOT OLD.symmetric OR NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types\n", "  WHEN 0\n")),
 ('links', '§6.18 walks with UNION ALL', mutate("  SELECT :place_id\n  UNION\n", "  SELECT :place_id\n  UNION ALL\n")),
 ('facts', 'a correction may be of another metric', mutate("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE 0;")),
 ('facts', 'measurement values may be infinite', mutate('CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)', 'CHECK (value IS NULL OR value = value)')),
 ('facts', 'measurements may be updated', mutate("CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements\nBEGIN\n", "CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements WHEN 0\nBEGIN\n")),
 ('positions', 'latitude may reach 180', mutate('CONSTRAINT positions_lat CHECK (lat BETWEEN -90 AND 90)', 'CONSTRAINT positions_lat CHECK (lat BETWEEN -180 AND 180)')),
 ('positions', 'longitude may reach 360', mutate('CONSTRAINT positions_lon CHECK (lon BETWEEN -180 AND 180)', 'CONSTRAINT positions_lon CHECK (lon BETWEEN -360 AND 360)')),
 ('positions', 'a fix may have no latitude (a NaN gets in)', mutate('  lat         REAL NOT NULL CONSTRAINT positions_lat', '  lat         REAL CONSTRAINT positions_lat')),
 ('positions', 'a photo without a location becomes a fix at 0, 0', mutate('  CONSTRAINT positions_not_null_island CHECK (NOT (lat = 0 AND lon = 0))', '  CONSTRAINT positions_not_null_island CHECK (1)')),
 ('positions', 'taken_at is checked with = instead of IS', mutate("CONSTRAINT positions_taken_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', taken_at) IS taken_at)", "CONSTRAINT positions_taken_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', taken_at) = taken_at)")),
 ('positions', 'positions may be updated', mutate("CREATE TRIGGER positions_no_update BEFORE UPDATE ON positions\nBEGIN\n", "CREATE TRIGGER positions_no_update BEFORE UPDATE ON positions WHEN 0\nBEGIN\n")),
 ('positions', 'positions may be deleted', mutate("CREATE TRIGGER positions_no_delete BEFORE DELETE ON positions\nBEGIN\n", "CREATE TRIGGER positions_no_delete BEFORE DELETE ON positions WHEN 0\nBEGIN\n")),
 ('positions', 'an import may insert a fix twice', mutate('CREATE UNIQUE INDEX positions_import', 'CREATE INDEX positions_import')),
 ('positions', 'a place may have half a point', mutate('CONSTRAINT places_coords_pair CHECK ((lat IS NULL) = (lon IS NULL))', 'CONSTRAINT places_coords_pair CHECK (1)')),
 ('positions', '§6.20 finds a tombstoned place', mutate('          JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n', '          JOIN entities e ON e.id = pl.id\n')),
 ('money', 'balance_values takes the oldest row of a day', mutate('AND x.day = b.day AND x.id > b.id);', 'AND x.day = b.day AND x.id < b.id);')),
 ('money', 'amounts may be REAL', mutate('  amount      INTEGER,', '  amount      REAL,')),
 ('money', '§6.15 forgets closed_day', mutate("   WHERE coalesce(a.opened_day, '0000-01-01') <= :day\n     AND coalesce(a.closed_day, '9999-12-31') >= :day\n", "   WHERE coalesce(a.opened_day, '0000-01-01') <= :day\n")),
 ('journal', 'tasks may be done without a completed_day', mutate('CHECK ((completed_at IS NULL) = (completed_day IS NULL))', 'CHECK (1)')),
 ('journal', '§6.2 shows superseded readings', mutate('    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n   WHERE me.day = :day', '    FROM measurements me JOIN metrics m ON m.id = me.metric_id\n   WHERE me.day = :day')),
 ('writers', '§2.6 no longer sets trusted_schema = OFF', mutate("PRAGMA trusted_schema = OFF;   -- the schema may call only side-effect-free functions (all of this one's are)\n", '')),
 ('integrity', 'the orphan query counts a person\'s page as its domain row', mutate(ORPHAN, ORPHAN.replace(" WHERE entity_type = 'page'", ''))),
 ('integrity', '§2.5 loses the FTS5 integrity-check', mutate("INSERT INTO pages_fts(pages_fts, rank) VALUES ('integrity-check', 1);   -- no error\n", '')),
 ('imports', 'the import block loses WHERE true', mutate('  FROM s.staging WHERE true\n', '  FROM s.staging\n')),
 ('evolution', 'one CHECK is unnamed', mutate('CONSTRAINT people_death_order CHECK', 'CHECK')),
 ('cookbook', '§6.17 names a column that does not exist', mutate('SELECT p.title, max(b.day) AS last_balance,', 'SELECT a.name, max(b.day) AS last_balance,')),
 ('document', 'a 2075 answer is gone from the file', mutate("  ('sqlite',    'writers need SQLite >= 3.51.3", "  ('sqlite',    'writers need SQLite >= 3.51")),
 ('document', 'a lifelog_meta key answers no question', mutate("  ('evolution', 'after the first real data", "  ('orphan',    'x'),\n  ('evolution', 'after the first real data")),
 ('document', 'the §3 totals drift from the DDL', mutate('**+ 34 triggers.**', '**+ 35 triggers.**')),
 ('diagrams', 'a foreign key is not drawn', mutate('    entities ||--o| tasks    : "id"\n', '')),
 ('diagrams', 'the link map invents an edge', mutate('    place -->|"located-in"| place\n', '    place -->|"located-in"| place\n    person -->|"mentioned"| page\n')),
 ('diagrams', 'a new link kind, the map unchanged', mutate("  ('friend',   1, 'person',    'person',       NULL),", "  ('mentor',   0, 'person',    'person',       NULL),\n  ('friend',   1, 'person',    'person',       NULL),")),
 ('diagrams', 'the correction story shows a wrong value', mutate('state "view shows 70.8" as V2', 'state "view shows 70.9" as V2')),
 ('diagrams', 'a non-key column is drawn', mutate('    link_kinds {\n        TEXT kind PK\n', '    link_kinds {\n        TEXT kind PK\n        INTEGER symmetric\n')),
]

def run(suite, text):
    d = tempfile.mkdtemp(prefix='mutant-'); dp, ddl = os.path.join(d, 'SCHEMA.md'), os.path.join(d, 'ddl.sql')
    open(dp, 'w', encoding='utf-8').write(text); open(ddl, 'w', encoding='utf-8').write(docsql.ddl(text))
    p = subprocess.run([sys.executable, '-W', 'ignore', f'{suite}.py', ddl], cwd=HERE, env=dict(os.environ, DOC=dp, DDL=ddl, PYTHONDONTWRITEBYTECODE='1'),
                       capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=300)
    m = re.search(r': (\d+)/(\d+) met expectations\s*$', p.stdout.strip())
    return (int(m.group(1)), int(m.group(2))) if m else (0, -1), (p.stdout + p.stderr).strip()

for suite in sorted({s for s, _, _ in MUTANTS}):
    (ok, n), out = run(suite, doc)
    assert ok == n > 0, f'{suite} must pass on the unmutated document first: {ok}/{n}\n{out[-400:]}'
caught = 0
for suite, name, text in MUTANTS:
    (ok, n), out = run(suite, text)
    noticed = n != -1 and ok != n
    caught += noticed
    stop = re.search(r'the suite stopped: (.*)', out)
    how = 'no result: ' + out.splitlines()[-1][:80] if n == -1 else f'{n - ok} of {n} fail' + (f' (stopped: {stop.group(1)[:60]})' if stop else '')
    print(f"  {'caught ' if noticed else 'MISSED '} {suite:<10} {name:<60} {how}")
print(f'mutants: {caught}/{len(MUTANTS)} met expectations')
sys.exit(0 if caught == len(MUTANTS) else 1)
