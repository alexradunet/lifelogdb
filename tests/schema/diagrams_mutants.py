"""Break the diagrams (and the DDL under them) on purpose and require diagrams.py to notice.   python3 diagrams_mutants.py
Each mutant is a copy of SCHEMA.md with one statement changed; the DDL the checks read is taken from that copy. Half of them edit a
diagram, half change the DDL and leave the diagram alone — the drift these checks exist for. A crash is not 'noticed' (record #13)."""
import os, re, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
doc = open(docsql.DOC, encoding='utf-8').read()

def mutate(old, new):
    n = doc.count(old)
    assert n == 1, f'mutation target found {n}x: {old[:70]!r}'
    return doc.replace(old, new, 1)

MUTANTS = [
 ('diagram: a foreign key is not drawn (entities -> tasks)',   mutate('    entities ||--o| tasks    : "id"\n', '')),
 ('diagram: wrong cardinality (events.place_id is nullable)',  mutate('places   |o--o{ events   : "place_id"', 'places   ||--o{ events   : "place_id"')),
 ('diagram: a column has the wrong type',                      mutate('        TEXT day "local capture day"', '        INTEGER day "local capture day"')),
 ('diagram: a column that does not exist',                     mutate('        TEXT body "CommonMark"', '        TEXT body "CommonMark"\n        TEXT nickname')),
 ('diagram: the FK mark is missing on links.kind',             mutate('        TEXT kind FK\n', '        TEXT kind\n')),
 ('diagram: the link map invents an edge',                     mutate('    place -->|"located-in"| place\n', '    place -->|"located-in"| place\n    person -->|"mentioned"| page\n')),
 ('diagram: the correction story shows a wrong value',         mutate('state "view shows 70.8" as V2', 'state "view shows 70.9" as V2')),
 ('diagram: the save flow renumbers a step',                   mutate('2b. INSERT entities and pages', '2c. INSERT entities and pages')),
 ('diagram: one of the nine is deleted (writers)',             re.sub(r'```mermaid\n%% diagram: writers\n.*?\n```\n', '', doc, count=1, flags=re.S)),
 ('DDL: events.place_id becomes NOT NULL, diagram unchanged',  mutate('  place_id    INTEGER REFERENCES places(id),', '  place_id    INTEGER NOT NULL REFERENCES places(id),')),
 ('DDL: a new link kind, link map unchanged',                  mutate("  ('friend',   1, 'person',    'person',       NULL),", "  ('mention',  0, 'page',      'person',       NULL),\n  ('friend',   1, 'person',    'person',       NULL),")),
 ('DDL: a new table, diagrams unchanged',                      mutate("CREATE TABLE links (", "CREATE TABLE tags_extra (id INTEGER PRIMARY KEY, name TEXT) STRICT;\n\nCREATE TABLE links (")),
]

def run(text):
    d = tempfile.mkdtemp(prefix='diag-mut-'); dp = os.path.join(d, 'SCHEMA.md'); ddl = os.path.join(d, 'ddl.sql')
    open(dp, 'w', encoding='utf-8').write(text); open(ddl, 'w', encoding='utf-8').write(docsql.ddl(text))
    p = subprocess.run([sys.executable, '-W', 'ignore', 'diagrams.py', ddl], cwd=HERE, env=dict(os.environ, DOC=dp, PYTHONDONTWRITEBYTECODE='1'), capture_output=True, text=True, stdin=subprocess.DEVNULL)
    out = p.stdout + p.stderr; m = re.search(r'diagram checks: (\d+)/(\d+)', out)
    return (int(m.group(1)), int(m.group(2))) if m and p.returncode == 0 else (0, -1), out

(ok, n), out = run(doc)
assert ok == n > 0, f'the unmutated document must pass its own checks first: {ok}/{n}\n{out[-500:]}'
caught = 0
for name, text in MUTANTS:
    (ok, n), out = run(text)
    noticed = n != -1 and ok != n
    caught += noticed
    print(f"  {'caught ' if noticed else 'MISSED '} {name:<58} {'crash' if n == -1 else f'{n - ok} of {n} checks fail'}")
print(f'{caught}/{len(MUTANTS)} broken documents were noticed')
sys.exit(0 if caught == len(MUTANTS) else 1)
