"""Break the round-15 rules on purpose and require r15probes.py to notice.   python3 r15_mutants.py  (tests venv)
Each mutant is a copy of SCHEMA.md with one rule put back the way it was before round 15 (or damaged); the DDL and the
text the probes read are taken from that copy. A probe suite that cannot fail proves nothing."""
import os, re, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
doc = open(docsql.DOC, encoding='utf-8').read()

def mutate(old, new):
    n = doc.count(old)
    assert n == 1, f'mutation target found {n}x: {old[:70]!r}'
    return doc.replace(old, new, 1)

MUTANTS = [
 ('6.1 attaches the mood with last_insert_rowid() again',
  mutate("SELECT id, '2026-09-29', 4, 'manual', :memo_id, strftime", "SELECT id, '2026-09-29', 4, 'manual', last_insert_rowid(), strftime")),
 ('2.8 loses the FTS5 integrity-check',
  mutate("INSERT INTO pages_fts(pages_fts, rank) VALUES ('integrity-check', 1);   -- no error\n", "")),
 ('the title CHECK lets the zero-width space through',
  mutate("|| char(8203) || char(8206)", "|| char(8206)")),
 ('the title CHECK lets C1 controls through',
  mutate("|| char(127) || '-' || char(159) || char(173)", "|| char(127) || char(173)")),
 ('pages_fts_au re-indexes on every update again',
  mutate("CREATE TRIGGER pages_fts_au AFTER UPDATE OF title, body ON pages BEGIN", "CREATE TRIGGER pages_fts_au AFTER UPDATE ON pages BEGIN")),
 ('one CHECK is unnamed again',
  mutate("CONSTRAINT people_death_order CHECK", "CHECK")),
 ('tasks repeat again',
  mutate("  completed_day TEXT CONSTRAINT tasks_completed_day",
         "  repeat       TEXT NOT NULL DEFAULT 'none' CONSTRAINT tasks_repeat CHECK (repeat IN ('none','daily','weekly','monthly','yearly')),\n  completed_day TEXT CONSTRAINT tasks_completed_day")),
 ('entities.source can be changed (no fixed trigger)',
  mutate("CREATE TRIGGER entities_source_fixed BEFORE UPDATE OF source ON entities\n  WHEN NEW.source IS NOT OLD.source",
         "CREATE TRIGGER entities_source_fixed BEFORE UPDATE OF source ON entities\n  WHEN 0")),
 ('links.source can be changed',
  mutate("\n    OR NEW.source IS NOT OLD.source\n", "\n")),
 ('the mirror drops the source',
  mutate("VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NEW.source);", "VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NULL);")),
 ('the sqlite key is gone from lifelog_meta',
  mutate("  ('sqlite',      'writers need SQLite >= 3.51.3", "  ('sqlite_min',  'writers need SQLite >= 3.51.3")),
 ('2.9 no longer sets trusted_schema = OFF',
  mutate("PRAGMA trusted_schema = OFF;   -- the schema may call only side-effect-free functions (all of this one's are)\n", "")),
 ('the entities rules live outside the statement again',
  mutate("  -- Nothing is ever deleted: deleted_at is the tombstone (D11), enforced by BEFORE DELETE triggers.\n", "")),
 ('measurement values may be infinite again',
  mutate("CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)", "CHECK (value IS NULL OR value = value)")),
 ('D18 no longer says newest = highest id',
  mutate("*Newest* means **recorded last — the highest `id`**", "*Newest* means **recorded last**")),
]

def run(text):
    d = tempfile.mkdtemp(prefix='r15-mut-'); dp = os.path.join(d, 'SCHEMA.md'); ddl = os.path.join(d, 'ddl.sql')
    open(dp, 'w', encoding='utf-8').write(text); open(ddl, 'w', encoding='utf-8').write(docsql.ddl(text))
    p = subprocess.run([sys.executable, '-W', 'ignore', 'r15probes.py', ddl], cwd=HERE, env=dict(os.environ, DOC=dp, DDL=ddl), capture_output=True, text=True, stdin=subprocess.DEVNULL)
    out = p.stdout + p.stderr; m = re.search(r'round-15 probes: (\d+)/(\d+)', out)
    return (int(m.group(1)), int(m.group(2))) if m and p.returncode == 0 else (0, -1), out

(ok, n), out = run(doc)
assert ok == n > 0, f'the unmutated document must pass its own probes first: {ok}/{n}\n{out[-500:]}'
caught = 0
for name, text in MUTANTS:
    (ok, n), out = run(text)
    noticed = n != -1 and ok != n          # a crash is not 'noticed': the probes must fail cleanly
    caught += noticed
    print(f"  {'caught ' if noticed else 'MISSED '} {name:<52} {'crash' if n == -1 else f'{n - ok} of {n} probes fail'}")
print(f'{caught}/{len(MUTANTS)} broken documents were noticed')
sys.exit(0 if caught == len(MUTANTS) else 1)
