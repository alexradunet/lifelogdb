"""A suite that cannot fail proves nothing. Each mutant is a copy of the docs tree with one rule broken — in schema.sql, in
a cookbook block or in the text of a page — and the suite that owns the rule must notice: it must run to its end and report at least
one failed expectation (a crash is not 'noticed'). The unmutated document must pass every suite named here first."""
import os, re, shutil, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
FILES = {r: docsql.page(r) for r in ['schema/schema.sql'] + docsql.pages()}

def mutate(old, new, nth=0):
    """{file: its broken text}: the nth occurrence of old, counted over schema.sql and then the pages in reading order."""
    hits = [(r, i) for r, t in FILES.items() for i in [m.start() for m in re.finditer(re.escape(old), t)]]
    assert len(hits) > nth, f'mutation target found {len(hits)}x: {old[:70]!r}'
    r, i = hits[nth]
    return {r: FILES[r][:i] + new + FILES[r][i + len(old):]}

ORPHAN = "SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages WHERE entity_type IN ('page','place') UNION"
MUTANTS = [   # (suite, what is broken, the broken document)
 ('dates', 'a day CHECK uses = instead of IS', mutate("CONSTRAINT pages_day CHECK (day IS NULL OR date(day) IS day)", "CONSTRAINT pages_day CHECK (day IS NULL OR date(day) = day)")),
 ('dates', 'instants lose their milliseconds', mutate("CONSTRAINT entities_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at)", "CONSTRAINT entities_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%SZ', created_at) IS created_at)")),
 ('identity', 'cookbook/capture attaches the mood with last_insert_rowid()', mutate("SELECT id, '2026-09-29', 4, 'ui', :page_id, strftime", "SELECT id, '2026-09-29', 4, 'ui', last_insert_rowid(), strftime")),
 ('identity', 'entities.source may be NULL', mutate("source      TEXT NOT NULL CONSTRAINT entities_source CHECK (length(source)", "source      TEXT CONSTRAINT entities_source CHECK (length(source)")),
 ('identity', 'entities.source can change', mutate("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.import_key IS NOT OLD.import_key")),
 ('identity', 'entities.import_key can change', mutate("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.source IS NOT OLD.source")),
 ('identity', 'an entity may be imported twice', mutate('CREATE UNIQUE INDEX entities_import', 'CREATE INDEX entities_import')),
 ('identity', 'cookbook/import-a-row-once revives a tombstoned import', mutate("WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);", "WHERE source = 'import:vault' AND import_key = :import_key);")),
 ('identity', 'the mirror drops the source', mutate("VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NEW.source);", "VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, 'ui');")),
 ('identity', 'people may be hard-deleted', mutate("CREATE TRIGGER people_no_delete BEFORE DELETE ON people\nBEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;\n", "")),
 ('identity', 'editing a page does not bump updated_at', mutate("CREATE TRIGGER pages_touch AFTER UPDATE ON pages BEGIN\n", "CREATE TRIGGER pages_touch AFTER UPDATE ON pages WHEN 0 BEGIN\n")),
 ('identity', 'the tombstone does not bump updated_at', mutate("CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities\n  WHEN NEW.deleted_at IS NOT OLD.deleted_at", "CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities\n  WHEN 0")),
 ('links', 'a redirect may not point at a person', mutate("'page,person,place', 'old stub page", "'page',         'old stub page")),
 ('links', 'located-in accepts any endpoint', mutate("  ('located-in', 0, 'place',   'place',", "  ('located-in', 0, NULL,      NULL,")),
 ('named', 'people hang off entities instead of their page', mutate("  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),\n  CONSTRAINT people_death_day_order", "  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),\n  CONSTRAINT people_death_day_order")),
 ('named', 'a promotion does not cascade to the page', mutate("REFERENCES entities(id, entity_type) ON UPDATE CASCADE,", "REFERENCES entities(id, entity_type),")),
 ('named', 'a day page may be promoted', mutate("CHECK (date(title) IS NOT title OR entity_type = 'page')", "CHECK (1)")),
 ('named', 'ghost_pages lists the page of a person', mutate("WHERE p.entity_type = 'page' AND p.body = ''", "WHERE p.body = ''")),
 ('named', 'a wikilink cannot land on a person', mutate("('wikilink', 0, 'page,person,place', 'page,person,place',", "('wikilink', 0, 'page,person,place', 'page',")),
 ('named', 'a page may claim an unknown type', mutate("CHECK (entity_type IN ('page','person','place')),   -- 'page', or the named entity this page is", "CHECK (1),   -- 'page', or the named entity this page is")),
 ('pages', 'the title CHECK lets the zero-width space through', mutate("|| char(8203) || char(8206)", "|| char(8206)")),
 ('pages', 'a device name before an extension is allowed', mutate("upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)", "upper(title)")),
 ('pages', 'the title index is not unique', mutate('CREATE UNIQUE INDEX pages_title', 'CREATE INDEX pages_title')),
 ('pages', 'a page may have no title', mutate("  title       TEXT NOT NULL,              -- filename-safe", "  title       TEXT,                       -- filename-safe")),
 ('pages', 'pages_fts_update re-indexes on every update', mutate('CREATE TRIGGER pages_fts_update AFTER UPDATE OF title, body ON pages BEGIN', 'CREATE TRIGGER pages_fts_update AFTER UPDATE ON pages BEGIN')),
 ('pages', 'the cookbook/save-a-body resolve scans by lower(title)', mutate(" WHERE p.title_key = :key;", " WHERE lower(p.title) = :key;")),
 ('links', 'friend is not symmetric', mutate("  ('friend',   1, 'person',", "  ('friend',   0, 'person',")),
 ('links', 'at accepts any endpoint', mutate("  ('at',       0, 'page',      'place',", "  ('at',       0, NULL,        NULL,")),
 ('links', 'link kinds may change structure', mutate("  WHEN NEW.symmetric IS NOT OLD.symmetric OR NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types\n", "  WHEN 0\n")),
 ('journal', 'cookbook/where-was-i lists the places of a tombstoned day', mutate("  FROM pages d\n  JOIN entities de ON de.id = d.id AND de.deleted_at IS NULL\n  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'\n  JOIN pages pl   ON pl.id = l.to_id\n  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE", "  FROM pages d\n  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'\n  JOIN pages pl   ON pl.id = l.to_id\n  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE")),
 ('links', 'cookbook/inside-a-place lists a tombstoned place', mutate('JOIN entities epl ON epl.id = pl.id AND epl.deleted_at IS NULL', 'JOIN entities epl ON epl.id = pl.id')),
 ('links', 'cookbook/inside-a-place walks with UNION ALL', mutate("  SELECT :place_id\n  UNION\n", "  SELECT :place_id\n  UNION ALL\n")),
 ('facts', 'a correction may be of another metric', mutate("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE 0;")),
 ('facts', 'measurement values may be infinite', mutate('CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)', 'CHECK (value IS NULL OR value = value)')),
 ('facts', 'measurements may be updated', mutate("CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements\nBEGIN\n", "CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements WHEN 0\nBEGIN\n")),
 ('journal', 'cookbook/where-was-i says I was at a tombstoned place', mutate("  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE d.title_key = :day", "  JOIN entities e ON e.id = pl.id\n WHERE d.title_key = :day")),
 ('journal', 'a day page may have no day', mutate("CONSTRAINT pages_day_page CHECK (date(title) IS NOT title OR day IS title)", "CONSTRAINT pages_day_page CHECK (1)")),
 ('journal', 'pages_day_page compares the day with = instead of IS', mutate("CHECK (date(title) IS NOT title OR day IS title)", "CHECK (date(title) IS NOT title OR day = title)")),
 ('journal', 'cookbook/capture looks for the day page under another key', mutate("JOIN entities e ON e.id = p.id WHERE p.title_key = '2026-09-29';", "JOIN entities e ON e.id = p.id WHERE p.title_key = 'today';")),
 ('journal', 'cookbook/days-that-name lists a tombstoned day', mutate("  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id = :entity_id", "  JOIN entities e ON e.id = d.id\n WHERE l.to_id = :entity_id")),
 ('journal', 'cookbook/days-that-name forgets the about links', mutate("l.kind IN ('wikilink', 'about')", "l.kind = 'wikilink'")),
 ('journal', 'cookbook/days-that-name lists pages that are not days', mutate("  JOIN pages d    ON d.id = l.from_id AND d.title = d.day\n  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id", "  JOIN pages d    ON d.id = l.from_id\n  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL\n WHERE l.to_id")),
 ('habits', 'two periods of one habit may overlap', mutate("   WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id\n", "   WHERE 0 AND EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id\n")),
 ('habits', 'an update may make periods overlap', mutate("WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id", "WHERE 0 AND EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id")),
 ('habits', 'a metric with a unit may be a habit', mutate("   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';", "   WHERE 0;")),
 ('habits', 'habit periods may be deleted', mutate("CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods\nBEGIN", "CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods WHEN 0\nBEGIN")),
 ('habits', 'habit_periods.source can change', mutate("CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\n  WHEN NEW.source IS NOT OLD.source", "CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\n  WHEN 0")),
 ('habits', 'a period may end before it starts', mutate("CONSTRAINT habit_periods_order CHECK (end_day IS NULL OR end_day >= start_day)", "CONSTRAINT habit_periods_order CHECK (1)")),
 ('habits', 'start_day is checked with = instead of IS', mutate("CHECK (date(start_day) IS start_day)", "CHECK (date(start_day) = start_day)")),
 ('habits', 'cookbook/habits counts a day not recorded as not done', mutate("sum(s.value IS 0) AS not_done", "sum(s.value IS NOT 1) AS not_done")),
 ('habits', 'cookbook/habits loses the idempotent re-run', mutate('A re-sent period is idempotent: insert it with\n`ON CONFLICT(metric_id, start_day) DO NOTHING`', 'A re-sent period is idempotent: insert it again')),
 ('habits', 'cookbook/habits re-sends a period without its end_day', mutate('carrying the `end_day` it was sent with', 'as it is')),
 ('habits', 'cookbook/day-view lists a habit outside its periods', mutate("   WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day\n  UNION ALL", "   WHERE 1\n  UNION ALL")),
 ('journal', 'cookbook/day-view shows superseded readings', mutate('    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n   WHERE me.day = :day', '    FROM measurements me JOIN metrics m ON m.id = me.metric_id\n   WHERE me.day = :day')),
 ('writers', 'contract/connections no longer sets trusted_schema = OFF', mutate("PRAGMA trusted_schema = OFF;   -- the schema may call only side-effect-free functions (all of this one's are)\n", '')),
 ('integrity', 'the orphan query counts a person\'s page as its domain row', mutate(ORPHAN, ORPHAN.replace(" WHERE entity_type IN ('page','place')", ''))),
 ('integrity', 'contract/integrity-checks loses the FTS5 integrity-check', mutate("INSERT INTO pages_fts(pages_fts, rank) VALUES ('integrity-check', 1);   -- no error\n", '')),
 ('imports', 'the import block loses WHERE true', mutate('  FROM s.staging WHERE true\n', '  FROM s.staging\n')),
 ('evolution', 'one CHECK is unnamed', mutate('CONSTRAINT people_death_day_order CHECK', 'CHECK')),
 ('cookbook', 'cookbook/inside-a-place names a column that does not exist', mutate('SELECT d.day, pl.title AS place', 'SELECT d.day, pl.name AS place')),
 ('document', 'a 2075 answer is gone from the file', mutate("  ('sqlite',    'writers need SQLite >= 3.51.3", "  ('sqlite',    'writers need SQLite >= 3.51")),
 ('document', 'a lifelog_meta key answers no question', mutate("  ('evolution', 'after the first real data", "  ('orphan',    'x'),\n  ('evolution', 'after the first real data")),
 ('document', 'cookbook/import-a-row-once bypasses the save contract', mutate('run the link sync of [save a body](save-a-body.md)', 'skip')),
 ('document', 'the deletes row hides the registries', mutate("the registries (metrics, link_kinds, lifelog_meta) are the owner''s administrative rows", "metrics, link_kinds and lifelog_meta are the owner''s administrative rows")),
 ('document', 'the schema totals drift from the DDL', mutate('**+ 24 triggers.**', '**+ 25 triggers.**')),
 ('diagrams', 'a foreign key is not drawn', mutate('    pages    ||--o| people   : "id"\n', '')),
 ('diagrams', 'the link map invents an edge', mutate('    place -->|"located-in"| place\n', '    place -->|"located-in"| place\n    person -->|"mentioned"| page\n')),
 ('diagrams', 'a new link kind, the map unchanged', mutate("  ('friend',   1, 'person',    'person',       NULL),", "  ('mentor',   0, 'person',    'person',       NULL),\n  ('friend',   1, 'person',    'person',       NULL),")),
 ('diagrams', 'the correction story shows a wrong value', mutate('state "view shows 70.8" as V2', 'state "view shows 70.9" as V2')),
 ('diagrams', 'a non-key column is drawn', mutate('    link_kinds {\n        TEXT kind PK\n', '    link_kinds {\n        TEXT kind PK\n        INTEGER symmetric\n')),
 ('named', 'the ark query lists a symmetric relation in both legs', mutate(" AND l.kind NOT IN (SELECT kind FROM link_kinds WHERE symmetric = 1)", "")),
 ('facts', 'metric-series has no upper bound', mutate("me.day <= :day", "1")),
 ('facts', 'metric-series includes the 91st day', mutate("me.day > date(", "me.day >= date(")),
 ('named', 'a promotion does not revive a tombstoned page', mutate(", deleted_at = NULL   -- cascades", "   -- cascades")),
 ('named', 'a promotion takes a redirect stub', mutate("\n   AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = :ghost_id AND kind = 'redirect')", "")),
]

def run(suite, broken):
    d = tempfile.mkdtemp(prefix='mutant-'); root, ddl = os.path.join(d, 'docs'), os.path.join(d, 'ddl.sql')
    try:
        shutil.copytree(docsql.DOCS, root)
        for r, t in broken.items(): open(os.path.join(root, *r.split('/')), 'w', encoding='utf-8', newline='\n').write(t)
        open(ddl, 'w', encoding='utf-8', newline='\n').write(docsql.ddl(root))
        p = subprocess.run([sys.executable, '-W', 'ignore', f'{suite}.py', ddl], cwd=HERE, env=dict(os.environ, DOCS=root, DDL=ddl, PYTHONDONTWRITEBYTECODE='1', PYTHONUTF8='1'),
                           capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=300)
        m = re.search(r': (\d+)/(\d+) met expectations\s*$', p.stdout.strip())
        return (int(m.group(1)), int(m.group(2))) if m else (0, -1), (p.stdout + p.stderr).strip()
    finally: shutil.rmtree(d, ignore_errors=True)

for suite in sorted({s for s, _, _ in MUTANTS}):
    (ok, n), out = run(suite, {})
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
