"""Round-15 probes (run with the tests venv:  DDL=... python3 r15probes.py DDLFILE).  Expected outcome is in each label.
A  Ids are carried with RETURNING, never with last_insert_rowid(): §6.1 run literally with a link sync inside it attaches the
   mood to the memo; §6.9 and §6.15 run literally keep their ids.
B  The fourth integrity check of §2.8: an FTS index that drifted from `pages` passes PRAGMA integrity_check and fails the
   FTS5 integrity-check the document names.
C  Titles reject invisible and bidi code points (the DDL and the app predicate agree); the app alone rejects unassigned ones.
D  Connection contract of §2.9: minimum SQLite version as data, trusted_schema=OFF + DEFENSIVE keep the schema working and
   stop writes to FTS shadow tables; pages_fts_au re-indexes only when title or body change.
E  Every CHECK is named, and every name can be dropped (the additive path for any rule); the DDL header is a pointer and the
   rules a stranger needs are inside the statements, where .schema shows them.
F  Tasks do not repeat (recurrence belongs to events).
G  Provenance: entities.source and links.source, shape-checked, written at insert and never changed; mirrors copy it.
H  Document text: the notes that go with A-G, and the smaller corrections."""
import os, re, sqlite3, sys
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, '..', 'lib')); sys.path.insert(0, os.path.join(HERE, '..', 'wikilinks'))
import docsql
from wikisave import title_ok, title_key
DDL = open(sys.argv[1], encoding='utf-8').read()
doc = docsql.doc_text()
live = doc[:doc.index('## 8. References')]
NOW = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
res = []
def K(label, cond, detail=''):
    res.append(bool(cond))
    if not cond: print('  FAIL', label, str(detail)[:200])
def fresh(hardened=False):
    c = sqlite3.connect(':memory:', isolation_level=None)
    if hardened: c.setconfig(sqlite3.SQLITE_DBCONFIG_DEFENSIVE, True); c.execute('PRAGMA trusted_schema = OFF')
    c.executescript(DDL); c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA recursive_triggers=ON'); return c
def ent(c, t):
    return c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES (?,{NOW},{NOW}) RETURNING id", (t,)).fetchone()[0]
def page(c, title):
    i = ent(c, 'page'); c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES (?, 'page', ?, ?)", (i, title, title_key(title))); return i
def memo(c, body='x'):
    i = ent(c, 'page'); c.execute("INSERT INTO pages(id,kind,day,body) VALUES (?, 'memo', '2026-09-30', ?)", (i, body)); return i
def tryx(c, sql, args=()):
    try: c.execute(sql, args); return 'OK'
    except sqlite3.Error as e: return 'ERR ' + str(e)
def statements(sql):
    out, acc = [], ''
    for line in sql.splitlines(keepends=True):
        acc += line
        if sqlite3.complete_statement(acc):
            if re.sub(r'--[^\n]*', '', acc).strip(): out.append(acc)
            acc = ''
    return out
def run_block(c, sql, P, after=None):
    """Execute a cookbook block statement by statement, as the app would: bind the :params it names, and keep an id a
    statement RETURNs under the :name its comment gives ('RETURNING id;  -- ... :memo_id')."""
    for st in statements(sql):
        code = re.sub(r'--[^\n]*', '', st)
        cur = c.execute(st, {k: P[k] for k in re.findall(r':(\w+)', code) if k in P})
        rows = cur.fetchall() if cur.description else None
        m = re.search(r'RETURNING id;[^\n]*?:(\w+)', st)
        if m and rows: P[m.group(1)] = rows[0][0]
        if after: after(code.strip())
def block(prefix):
    b = [s for h, s in docsql.cookbook_blocks(doc) if h.startswith(prefix)]
    return b[0] if b else ''

# ---- A  ids travel with RETURNING
K('A no §6 SQL block uses last_insert_rowid()', not [h for h, s in docsql.cookbook_blocks(doc) if 'last_insert_rowid' in s],
  [h for h, s in docsql.cookbook_blocks(doc) if 'last_insert_rowid' in s])
c = fresh()
for i in range(10): page(c, f'Seed {i}')                                   # entity ids run ahead of link ids
P = {}
def sync(code):                                                            # the §6.14 link sync, run right after the memo row
    if code.upper().startswith('INSERT INTO PAGES'):
        mid = c.execute("SELECT id FROM pages WHERE kind = 'memo'").fetchone()[0]
        g = page(c, 'Lifelog')
        c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'wikilink',{NOW})", (mid, g))
try:
    run_block(c, block('6.1 '), P, sync); r = 'OK'
except sqlite3.Error as e: r = 'ERR ' + str(e)
mood = c.execute("SELECT me.entity_id FROM measurements me JOIN metrics m ON m.id = me.metric_id AND m.name = 'mood'").fetchall()
memo_row = c.execute("SELECT id FROM pages WHERE kind = 'memo'").fetchall()
K('A1 §6.1 run literally, with the link sync inside its transaction: the mood reading points at the memo',
  r == 'OK' and len(mood) == 1 and len(memo_row) == 1 and mood[0][0] == memo_row[0][0], (r, mood, memo_row, P))
K('A1 ...and the memo id was carried by RETURNING', memo_row and P.get('memo_id') == memo_row[0][0], P)
c = fresh()
for i in range(10): page(c, f'Seed {i}')
m9 = memo(c, 'call the dentist'); P = dict(memo_id=m9, due_day='2026-10-05')
try: run_block(c, block('6.9 '), P); r = 'OK'
except sqlite3.Error as e: r = 'ERR ' + str(e)
t = c.execute("SELECT t.id, l.to_id, p.triaged_at IS NOT NULL FROM tasks t JOIN links l ON l.from_id = t.id AND l.kind = 'spawned' JOIN pages p ON p.id = l.to_id").fetchall()
K('A2 §6.9 run literally: the new task (the id it RETURNed) spawned from the memo, memo triaged', r == 'OK' and t == [(P.get('task_id'), m9, 1)], (r, t, P))
c = fresh(); ent(c, 'page'); ent(c, 'page')
P = dict(day='2026-09-29', amount=777, row_key='r1')
try: run_block(c, block('6.15 '), P); r = 'OK'
except sqlite3.Error as e: r = 'ERR ' + str(e)
a = c.execute("SELECT id FROM accounts").fetchall(); nb = c.execute("SELECT count(*) FROM balances WHERE account_id = ?", (P.get('account_id'),)).fetchone()[0]
K('A3 §6.15 run literally: the account RETURNs its id and every balance lands on it', r == 'OK' and a == [(P.get('account_id'),)] and nb == 4, (r, a, nb, P))

# ---- B  the FTS integrity check of §2.8
s28 = doc[doc.index('### 2.8 '):doc.index('### 2.9 ')]
blk = re.search(r'```sql\n(.*?)\n```', s28, re.S)
st28 = statements(blk.group(1)) if blk else []
fts = [s for s in st28 if 'integrity-check' in s]
K('B §2.8 names the FTS5 integrity-check (with rank 1: compare with the content table)', len(fts) == 1 and "'integrity-check', 1" in fts[0], st28)
c = fresh(); memo(c, 'alpha beta'); page(c, 'Gamma')
K('B clean file: every statement of §2.8 runs without error', fts and all(tryx(c, s) == 'OK' for s in st28))
c.execute("INSERT INTO pages_fts(rowid, title, body) VALUES (999, 'ghost', 'drifted')")
K('B drifted FTS index: PRAGMA integrity_check still says ok (why a fourth check exists)', c.execute('PRAGMA integrity_check').fetchall() == [('ok',)])
K('B drifted FTS index: the FTS statement of §2.8 fails', fts and tryx(c, fts[0]).startswith('ERR'), fts and tryx(c, fts[0]))

# ---- C  invisible and bidi code points in titles
INVIS = [0x85, 0x9F, 0xAD, 0x61C, 0x200B, 0x200E, 0x200F, 0x202A, 0x202E, 0x2060, 0x2064, 0x2066, 0x2069, 0xFEFF]
def db_accepts(t):
    c.execute('SAVEPOINT x')
    try: page(c, t); ok = True
    except sqlite3.Error: ok = False
    c.execute('ROLLBACK TO x'); c.execute('RELEASE x'); return ok
c = fresh()
for cp in INVIS:
    ch = chr(cp)
    for t in (f'Diet{ch}plan', f'{ch}Diet', f'Diet{ch}'):
        K(f'C U+{cp:04X} in a title is rejected by the DB and by the app', not db_accepts(t) and not title_ok(t), (repr(t), db_accepts(t), title_ok(t)))
KEEP = ['Café', '日本語 ノート', '\U0001F389 Party', 'می‌خواهم', '\U0001F468‍\U0001F469‍\U0001F467', 'İstanbul', 'a b', '❤️']
for t in KEEP:
    K(f'C {t!r} is accepted by both (ZWNJ/ZWJ and variation selectors carry meaning in scripts and emoji)', db_accepts(t) and title_ok(t), (db_accepts(t), title_ok(t)))
for t in ('Diet͸', '\U000E0080x'):
    K(f'C unassigned code point {t!r}: the app rejects it (its fold could change with Unicode), the DB cannot tell', not title_ok(t) and db_accepts(t))

# ---- D  the connection contract
c = fresh(); meta = dict(c.execute('SELECT key, value FROM lifelog_meta'))
K("D lifelog_meta has a 'sqlite' key naming the minimum versions", '3.51.3' in meta.get('sqlite', '') and '3.53' in meta.get('sqlite', ''), meta.get('sqlite'))
K('D this suite runs on such a SQLite', tuple(map(int, sqlite3.sqlite_version.split('.'))) >= (3, 51, 3), sqlite3.sqlite_version)
s29 = doc[doc.index('### 2.9 '):doc.index('### 2.10 ')]
b29 = re.search(r'```sql\n(.*?)\n```', s29, re.S)
K('D the §2.9 block sets trusted_schema = OFF', b29 and re.search(r'PRAGMA trusted_schema\s*=\s*OFF', b29.group(1)))
for w in ('3.51.3', 'SQLITE_DBCONFIG_DEFENSIVE', 'PRAGMA optimize', 'immutable', 'network file', 'autocommit', 'checkpoint'):
    K(f'D §2.9 says {w!r}', w in s29)
c = sqlite3.connect(':memory:', isolation_level=None); c.setconfig(sqlite3.SQLITE_DBCONFIG_DEFENSIVE, True)
for st in statements(b29.group(1) if b29 else ''): c.execute(st)
try:
    c.executescript(DDL); p = page(c, 'Hardened'); c.execute("UPDATE pages SET body = 'searchable words' WHERE id = ?", (p,))
    mm = memo(c, 'another'); c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'wikilink',{NOW})", (mm, p))
    c.execute("DELETE FROM links WHERE from_id = ? AND kind = 'wikilink' AND to_id NOT IN (SELECT value FROM json_each('[]'))", (mm,))
    got = (c.execute("SELECT count(*) FROM pages_fts WHERE pages_fts MATCH 'searchable'").fetchone()[0], c.execute('SELECT count(*) FROM ghost_pages').fetchone()[0],
           c.execute('SELECT count(*) FROM measurement_values').fetchone()[0], c.execute('SELECT count(*) FROM balance_values').fetchone()[0])
    hard = 'OK'
except sqlite3.Error as e: hard, got = 'ERR ' + str(e), None
K('D with the §2.9 pragmas (trusted_schema=OFF) and DEFENSIVE: DDL, writes, FTS, views and json_each all work', hard == 'OK' and got == (1, 0, 0, 0), (hard, got))
K('D ...and a write to an FTS shadow table is refused', tryx(c, 'DELETE FROM pages_fts_data').startswith('ERR'))
import subprocess
hc = subprocess.run([sys.executable, 'cookbook_doc.py', sys.argv[1]], cwd=HERE, env=dict(os.environ, HARDENED='1'), capture_output=True, text=True, stdin=subprocess.DEVNULL)
K('D every §6 block runs on a hardened connection (DEFENSIVE + trusted_schema=OFF)', 'hardened connection' in hc.stdout and 'cookbook failures: 0' in hc.stdout, (hc.stdout + hc.stderr)[-300:])
c = fresh(); p = memo(c, 'first words')
before = c.total_changes; c.execute(f'UPDATE pages SET triaged_at = {NOW} WHERE id = ?', (p,)); d_triage = c.total_changes - before
before = c.total_changes; c.execute("UPDATE pages SET body = 'second words' WHERE id = ?", (p,)); d_body = c.total_changes - before
K('D triage (no title/body change) touches pages + entities only, not the FTS index', d_triage == 2, d_triage)
K('D a body change re-indexes: old word gone, new word found', d_body > 2 and c.execute("SELECT count(*) FROM pages_fts WHERE pages_fts MATCH 'first'").fetchone()[0] == 0
  and c.execute("SELECT count(*) FROM pages_fts WHERE pages_fts MATCH 'second'").fetchone()[0] == 1, d_body)

# ---- E  named CHECKs; the header is a pointer
code = re.sub(r'--[^\n]*', '', DDL)
nchk = len(re.findall(r'\bCHECK\s*\(', code)); nnamed = re.findall(r'\bCONSTRAINT\s+(\w+)\s+CHECK\s*\(', code)
K('E every CHECK in §3 is named', nchk > 0 and nchk == len(nnamed), (nchk, len(nnamed)))
K('E constraint names are unique', len(nnamed) == len(set(nnamed)), [n for n in nnamed if nnamed.count(n) > 1])
tbl_of = {}
for m in re.finditer(r'CREATE TABLE (\w+) \((.*?)\n\) STRICT;', code, re.S):
    for n in re.findall(r'\bCONSTRAINT\s+(\w+)\s+CHECK', m.group(2)): tbl_of[n] = m.group(1)
c = fresh(); memo(c, 'm'); page(c, 'P')
bad = []
for n, t in tbl_of.items():
    c.execute('BEGIN')
    if tryx(c, f'ALTER TABLE {t} DROP CONSTRAINT {n}') != 'OK': bad.append(n)
    c.execute('ROLLBACK')
K('E every named CHECK can be dropped by name on a populated database (then rolled back)', tbl_of and not bad, bad)
K('E integrity clean after the drops were rolled back', c.execute('PRAGMA integrity_check').fetchall() == [('ok',)])
c = fresh(); p1 = ent(c, 'person'); c.execute("INSERT INTO people(id,name,birth_day) VALUES (?, 'Ada', '1815-12-10')", (p1,))
K('E the loosening path is one DROP + ADD: people_birth_day to an EDTF subset (YYYY, YYYY-MM, YYYY-MM-DD)',
  tryx(c, 'BEGIN') == 'OK' and tryx(c, 'ALTER TABLE people DROP CONSTRAINT people_birth_day') == 'OK'
  and tryx(c, "ALTER TABLE people ADD CONSTRAINT people_birth_day CHECK (birth_day IS NULL OR date(birth_day) IS birth_day OR birth_day GLOB '[0-9][0-9][0-9][0-9]' OR (birth_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]' AND substr(birth_day, 6, 2) BETWEEN '01' AND '12'))") == 'OK'
  and tryx(c, 'COMMIT') == 'OK')
p2 = ent(c, 'person')
K('E ...then 1870 and 1870-05 are stored, junk still is not', tryx(c, "INSERT INTO people(id,name,birth_day) VALUES (?, 'Anc', '1870-05')", (p2,)) == 'OK'
  and tryx(c, "UPDATE people SET birth_day = '1870' WHERE id = ?", (p2,)) == 'OK' and tryx(c, "UPDATE people SET birth_day = '1870-13' WHERE id = ?", (p2,)).startswith('ERR'))
first = re.search(r'^(?!\s*--)\s*\S', DDL, re.M)
head = DDL[:first.start()] if first else DDL
K('E the DDL header before the first statement is a short pointer (<= 12 lines)', head.count('\n') <= 12, head.count('\n'))
K('E the header points at lifelog_meta', 'lifelog_meta' in head)
c = fresh(); schema = ' '.join(s for (s,) in c.execute('SELECT sql FROM sqlite_schema WHERE sql IS NOT NULL'))
for obj, phrase in [('entities', 'tombstone'), ('entities', 'never back-dated'), ('entities', 'IS, not ='), ('pages', 'never renamed'),
                    ('pages', 'title_key'), ('measurements', 'append-only'), ('measurements', 'RETRACTS'), ('balances', 'minor units'),
                    ('balances', 'highest id'), ('link_kinds', 'CLOSED registry'), ('links', 'source'), ('lifelog_meta', 'the contract')]:
    sql = c.execute("SELECT sql FROM sqlite_schema WHERE name = ?", (obj,)).fetchone()
    K(f'E .schema shows it: {obj} carries {phrase!r} inside its CREATE statement', sql and phrase in sql[0])
K("E lifelog_meta 'evolution' says every CHECK is named", 'every CHECK is named' in meta.get('evolution', ''), meta.get('evolution'))

# ---- F  tasks do not repeat
cols = [r[1] for r in c.execute("PRAGMA table_info('tasks')")]
K('F tasks has no repeat columns', not [x for x in cols if x.startswith('repeat')], cols)
K('F events still repeat', 'repeat' in [r[1] for r in c.execute("PRAGMA table_info('events')")])
K("F lifelog_meta 'recurrence' speaks of events only", 'event' in meta.get('recurrence', '') and 'task' not in meta.get('recurrence', ''), meta.get('recurrence'))

# ---- G  provenance
c = fresh()
for t in ('entities', 'links'):
    info = {r[1]: r for r in c.execute(f"PRAGMA table_info('{t}')")}
    K(f'G {t}.source exists and is nullable TEXT', 'source' in info and info['source'][2] == 'TEXT' and info['source'][3] == 0)
for v in ('ui', 'cli', 'api', 'agent:claude', 'import:bank_csv', 'import:health-2026.v2'):
    K(f'G source {v!r} is accepted', tryx(c, f"INSERT INTO entities(type,created_at,updated_at,source) VALUES ('page',{NOW},{NOW},?)", (v,)) == 'OK')
for v in ('', 'UI', 'agent claude', 'x' * 65, 'a/b'):
    K(f'G source {v!r} is rejected', tryx(c, f"INSERT INTO entities(type,created_at,updated_at,source) VALUES ('page',{NOW},{NOW},?)", (v,)).startswith('ERR'))
try:
    e1 = c.execute(f"INSERT INTO entities(type,created_at,updated_at,source) VALUES ('person',{NOW},{NOW},'ui') RETURNING id").fetchone()[0]
    c.execute("INSERT INTO people(id,name) VALUES (?, 'A')", (e1,))
    e2 = c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES ('person',{NOW},{NOW}) RETURNING id").fetchone()[0]
    c.execute("INSERT INTO people(id,name) VALUES (?, 'B')", (e2,))
    K('G entities.source cannot change', tryx(c, "UPDATE entities SET source = 'cli' WHERE id = ?", (e1,)).startswith('ERR'))
    K('G ...cannot be filled in later either (written at insert)', tryx(c, "UPDATE entities SET source = 'cli' WHERE id = ?", (e2,)).startswith('ERR'))
    K('G a no-op full-row update and a tombstone still pass', tryx(c, f"UPDATE entities SET source = source, deleted_at = {NOW} WHERE id = ?", (e1,)) == 'OK')
    c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'friend',{NOW},'agent:claude')", (e1, e2))
    K('G the mirror of a symmetric link copies its source', c.execute("SELECT source FROM links WHERE from_id = ? AND to_id = ?", (e2, e1)).fetchone() == ('agent:claude',))
    K('G links.source cannot change', tryx(c, "UPDATE links SET source = 'ui' WHERE from_id = ?", (e1,)).startswith('ERR'))
    K('G a link note edit still passes', tryx(c, "UPDATE links SET note = 'n', source = source WHERE from_id = ?", (e1,)) == 'OK')
except sqlite3.Error as e:
    K('G the provenance rows can be written at all', False, e)
K("G lifelog_meta has a 'provenance' key", 'written at insert' in meta.get('provenance', ''), meta.get('provenance'))
s211 = doc[doc.index('### 2.11 '):doc.index('## 3. The schema')]
K('G the 2075 table asks who wrote a row', re.search(r'^\|\s*\d+\s*\|[^|]*[Ww]ho or what wrote[^|]*\|\s*`provenance`', s211, re.M))

# ---- I  measurement values are finite
c = fresh()
def mv(v, sup=None):
    return tryx(c, f"INSERT INTO measurements(metric_id,day,value,recorded_at,supersedes_id) VALUES (1,'2026-01-01',?,{NOW},?)", (v, sup))
K('I +Infinity is rejected', mv(float('inf')).startswith('ERR'))
K('I -Infinity is rejected', mv(float('-inf')).startswith('ERR'))
K('I the largest finite double and ordinary values are accepted', mv(1.7976931348623157e308) == 'OK' and mv(-0.0) == 'OK' and mv(70.5) == 'OK')
K('I NaN binds as NULL: rejected as a first reading', mv(float('nan')).startswith('ERR'))
first = c.execute('SELECT max(id) FROM measurements').fetchone()[0]
K('I ...but on a correction it is a retraction the DB cannot tell apart (why the app must never bind NaN)',
  mv(float('nan'), first) == 'OK' and c.execute('SELECT value FROM measurements WHERE supersedes_id = ?', (first,)).fetchone() == (None,))
d7 = doc[doc.index('### D7 '):doc.index('### D8 ')]
K('I D7 says values are finite and warns about NaN', 'finite' in d7 and 'NaN' in d7)

# ---- H  document text
d18 = doc[doc.index('### D18 '):doc.index('### D19 ')]
K('H D18 says "newest" is the highest id, and that a later import wins', 'highest `id`' in d18 and 'import' in d18[d18.index('highest `id`'):][:400])
K('H §2.11 names the INSERT OR REPLACE trap on symmetric links', 'too many levels of trigger recursion' in s211)
c = fresh(); a1 = ent(c, 'person'); c.execute("INSERT INTO people(id,name) VALUES (?, 'a')", (a1,)); b1 = ent(c, 'person'); c.execute("INSERT INTO people(id,name) VALUES (?, 'b')", (b1,))
c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'friend',{NOW})", (a1, b1))
K('H ...and it is real: OR REPLACE on a symmetric link raises that error', 'too many levels of trigger recursion' in tryx(c, f"INSERT OR REPLACE INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'friend',{NOW})", (a1, b1)))
K('H the threat table sends in-value damage to a checksumming filesystem', 'chattr +C' in s211 and 'scrub' in s211)
K('H Appendix A no longer lists INSERT OR IGNORE as taken', '`INSERT OR IGNORE` idempotency, blobs' not in doc)
K('H §7 no longer cites a §1.3', '§1.3' not in live)
K('H D7 and D18 use the word bitemporal', 'bitemporal' in doc[doc.index('### D7 '):doc.index('### D8 ')] and 'bitemporal' in d18)
sec7 = doc[doc.index('## 7. '):doc.index('## 8. References')]
K('H §7 partial dates name EDTF', 'EDTF' in sec7)
K('H §7 has the task-recurrence row', 'tick off' in sec7)
d10 = doc[doc.index('### D10 '):doc.index('### D11 ')]
K('H D10 names RFC 9557 and a tz rename', 'RFC 9557' in d10 and 'Europe/Kyiv' in d10)
s614 = doc[doc.index('### 6.14 '):doc.index('### 6.15 ')]
K('H §6.14 says the UI tells the owner a save revives a deleted page', 'revives a deleted page' in s614)
K('H AGENTS.md speaks of four integrity checks', 'four integrity checks' in open(os.path.join(HERE, '..', '..', 'AGENTS.md'), encoding='utf-8').read())

print(f'round-15 probes: {sum(res)}/{len(res)} met expectations')
