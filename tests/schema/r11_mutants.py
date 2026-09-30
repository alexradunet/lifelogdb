"""Break the round-11 rules on purpose and require r11probes.py to notice.   python3 r11_mutants.py
Each mutant is a copy of SCHEMA.md with one statement put back the way it was before the merge (or damaged); the DDL and the
cookbook blocks the probes read are taken from that copy. A probe suite that cannot fail proves nothing (records #6, #12)."""
import os, re, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
doc = open(docsql.DOC, encoding='utf-8').read()
live = doc[:doc.index('## 8. Validation records')]

def mutate(old, new, nth=None):
    """Replace one exact statement of the live text; nth picks the nth of several identical ones (0-based)."""
    n = live.count(old)
    assert n >= 1 and (nth is None and n == 1 or nth is not None and nth < n), f'mutation target not found {n}x: {old[:60]!r}'
    if nth is None: return doc.replace(old, new, 1)
    i = -1
    for _ in range(nth + 1): i = doc.index(old, i + 1)
    return doc[:i] + new + doc[i + len(old):]

GHOST = "WHERE p.kind = 'page' AND p.body = '' AND e.deleted_at IS NULL"
MUTANTS = [
 ('kinds are memo/note/wiki again',              mutate("CHECK (kind IN ('memo','page')),", "CHECK (kind IN ('memo','note','wiki')),")),
 ('every page needs a day again',                mutate("CHECK (kind = 'page' OR day IS NOT NULL),", "CHECK (day IS NOT NULL),")),
 ('nothing needs a day',                         mutate("CHECK (kind = 'page' OR day IS NOT NULL),", "CHECK (1),")),
 ('index predicate is on kind',                  mutate("ON pages(title_key) WHERE title_key IS NOT NULL;", "ON pages(title_key) WHERE kind = 'page';")),
 ('index has no predicate (memos indexed)',      mutate("ON pages(title_key) WHERE title_key IS NOT NULL;", "ON pages(title_key);")),
 ('index is not unique',                         mutate("CREATE UNIQUE INDEX pages_title", "CREATE INDEX pages_title")),
 ('ghost_pages view still says wiki',            mutate(GHOST, "WHERE p.kind = 'wiki' AND p.body = '' AND e.deleted_at IS NULL", nth=0)),
 ('§6.13 block still says wiki',                 mutate(GHOST, "WHERE p.kind = 'wiki' AND p.body = '' AND e.deleted_at IS NULL", nth=1)),
 ('§6.2 day view still says note',               mutate("WHERE p.day = :day AND p.kind = 'page' AND e.deleted_at IS NULL", "WHERE p.day = :day AND p.kind = 'note' AND e.deleted_at IS NULL")),
 ('§6.14 resolve has the old kind predicate',    mutate(" WHERE p.title_key = :key;", " WHERE p.kind IN ('note','wiki') AND p.title_key = :key;")),
 ('pages_kind_fixed never fires',                mutate("WHEN NEW.kind IS NOT OLD.kind", "WHEN 0")),
 ('pages_title_fixed never fires',               mutate("WHEN NEW.title IS NOT OLD.title", "WHEN 0")),
]

def run(text):
    d = tempfile.mkdtemp(prefix='r11-mut-'); dp = os.path.join(d, 'SCHEMA.md'); ddl = os.path.join(d, 'ddl.sql')
    open(dp, 'w', encoding='utf-8').write(text); open(ddl, 'w', encoding='utf-8').write(docsql.ddl(text))
    p = subprocess.run([sys.executable, '-W', 'ignore', 'r11probes.py', ddl], cwd=HERE, env=dict(os.environ, DOC=dp), capture_output=True, text=True, stdin=subprocess.DEVNULL)
    out = p.stdout + p.stderr; m = re.search(r'round-11 probes: (\d+)/(\d+)', out)
    return (int(m.group(1)), int(m.group(2))) if m and p.returncode == 0 else (0, -1), out

(ok, n), out = run(doc)
assert ok == n > 0, f'the unmutated document must pass its own probes first: {ok}/{n}\n{out[-400:]}'
caught = 0
for name, text in MUTANTS:
    (ok, n), out = run(text)
    noticed = n != -1 and ok != n          # a crash is not 'noticed': the probes must fail cleanly
    caught += noticed
    print(f"  {'caught ' if noticed else 'MISSED '} {name:<45} {'crash' if n == -1 else f'{n - ok} of {n} probes fail'}")
print(f'{caught}/{len(MUTANTS)} broken documents were noticed')
sys.exit(0 if caught == len(MUTANTS) else 1)
