"""Break the round-12 rules on purpose and require r12probes.py to notice.   python3 r12_mutants.py
Each mutant is a copy of SCHEMA.md with one statement put back the way it was before round 12 (or damaged); the DDL and the
text the probes read are taken from that copy. A probe suite that cannot fail proves nothing."""
import os, re, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
doc = open(docsql.DOC, encoding='utf-8').read()

def mutate(old, new):
    n = doc.count(old)
    assert n == 1, f'mutation target found {n}x: {old[:70]!r}'
    return doc.replace(old, new, 1)

ORPHAN = "SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages UNION SELECT id FROM events UNION SELECT id FROM tasks UNION SELECT id FROM people UNION SELECT id FROM places UNION SELECT id FROM accounts);"
MUTANTS = [
 ('the export key is back in lifelog_meta',        mutate("  ('evolution',   'after the first real data", "  ('export',      'export/ = markdown mirror of the prose only; derived nightly'),\n  ('evolution',   'after the first real data")),
 ('the backups key is back in lifelog_meta',       mutate("  ('evolution',   'after the first real data", "  ('backups',     'life-YYYYMMDD.db = verified nightly snapshots; restore with restore.sh'),\n  ('evolution',   'after the first real data")),
 ('a backups/ folder is back in the layout',       mutate("└── life.db                  # canonical: all structured data + all prose", "├── life.db                  # canonical: all structured data + all prose\n└── backups/                 # nightly snapshots")),
 ('the orphan query forgets the accounts table',   mutate(ORPHAN, ORPHAN.replace(" UNION SELECT id FROM accounts", ""))),
 ('the foreign_key_check line is gone from 2.8',   mutate("PRAGMA foreign_key_check;    -- no rows\n", "")),
 ('the imports step runs nightly.sh again',        mutate("1. **Trial run first.**", "1. **Snapshot first.** Run `nightly.sh` (§2.8).")),
 ('the titles key says export filenames again',    mutate("'page titles never change and are valid file names everywhere:", "'page titles are export filenames and never change:")),
 ('a 2075 question is dropped',                    mutate("| 20 | What is a memo, what is a page, and can one become the other? | `pages_kind` | `untitled`, `never changes` |\n", "")),
 ('the off-box copy is mandatory again',           mutate("| The file is damaged or lost | `synchronous=FULL` and WAL on SQLite ≥ 3.51.3, on a local disk (§2.9); the integrity checks find damage (§2.8) |", "| The file is damaged or lost | a mandatory off-box copy and a drilled restore (§2.8) |")),
]

def run(text):
    d = tempfile.mkdtemp(prefix='r12-mut-'); dp = os.path.join(d, 'SCHEMA.md'); ddl = os.path.join(d, 'ddl.sql')
    open(dp, 'w', encoding='utf-8').write(text); open(ddl, 'w', encoding='utf-8').write(docsql.ddl(text))
    p = subprocess.run([sys.executable, '-W', 'ignore', 'r12probes.py', ddl], cwd=HERE, env=dict(os.environ, DOC=dp), capture_output=True, text=True, stdin=subprocess.DEVNULL)
    out = p.stdout + p.stderr; m = re.search(r'round-12 probes: (\d+)/(\d+)', out)
    return (int(m.group(1)), int(m.group(2))) if m and p.returncode == 0 else (0, -1), out

(ok, n), out = run(doc)
assert ok == n > 0, f'the unmutated document must pass its own probes first: {ok}/{n}\n{out[-500:]}'
caught = 0
for name, text in MUTANTS:
    (ok, n), out = run(text)
    noticed = n != -1 and ok != n          # a crash is not 'noticed': the probes must fail cleanly
    caught += noticed
    print(f"  {'caught ' if noticed else 'MISSED '} {name:<48} {'crash' if n == -1 else f'{n - ok} of {n} probes fail'}")
print(f'{caught}/{len(MUTANTS)} broken documents were noticed')
sys.exit(0 if caught == len(MUTANTS) else 1)
