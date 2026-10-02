"""Writers and readers (contract/connections, D3, D14): the BEGIN IMMEDIATE race with real concurrent connections, the pragmas
and what they do, read-only readers under WAL, the minimum SQLite as data, and a hardened connection."""
import os, re, subprocess, sys, tempfile, threading, time
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('writers')
def mkdb():
    path = os.path.join(tempfile.mkdtemp(), 'life.db'); c = sqlite3.connect(path, isolation_level=None); c.executescript(DDL); c.close(); return path
def conn(path):
    c = sqlite3.connect(path, isolation_level=None); c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA recursive_triggers=ON'); c.execute('PRAGMA busy_timeout=5000'); return c
RESOLVE = 'SELECT p.id FROM pages p WHERE p.title_key = ?'

# ---- the race of cookbook/save-a-body
path = mkdb(); A, B = conn(path), conn(path)
A.execute('BEGIN'); S.K('deferred: the resolve finds nothing', A.execute(RESOLVE, ('diet',)).fetchall() == [])
B.execute('BEGIN IMMEDIATE'); page(B, 'Diet'); B.execute('COMMIT')
try: page(A, 'Diet'); A.execute('COMMIT'); out = 'OK'
except sqlite3.Error as e: out = str(e); A.execute('ROLLBACK')
S.K('deferred BEGIN, a read, a rival commit: the write fails at once (busy_timeout does not apply)', 'locked' in out, out)
path = mkdb(); res = {}
def writer(name, delay, hold):
    c = conn(path); time.sleep(delay); t0 = time.time()
    try:
        c.execute('BEGIN IMMEDIATE'); waited = time.time() - t0
        found = c.execute(RESOLVE, ('diet',)).fetchall()
        if not found: time.sleep(hold); page(c, 'Diet')
        c.execute('COMMIT'); res[name] = ('found' if found else 'created', waited)
    except sqlite3.Error as e: res[name] = ('ERROR', str(e)); c.execute('ROLLBACK')
th = [threading.Thread(target=writer, args=('A', 0.0, 0.4)), threading.Thread(target=writer, args=('B', 0.1, 0.0))]
[x.start() for x in th]; [x.join() for x in th]
S.K('BEGIN IMMEDIATE: the first writer creates, the second waits and then finds it', res.get('A', ('',))[0] == 'created' and res.get('B', ('',))[0] == 'found', res)
S.K('...the second really waited for the lock', res.get('B', ('', 0))[1] > 0.25, res)
S.K('...one page', conn(path).execute("select count(*) from pages where title_key='diet'").fetchone()[0] == 1)
sec6 = '\n'.join(s for _, s in docsql.cookbook_blocks())
S.K('cookbook has no bare BEGIN, and its write blocks start with BEGIN IMMEDIATE', not re.findall(r'^BEGIN;?\s*$', sec6, re.M) and len(re.findall(r'^BEGIN IMMEDIATE;', sec6, re.M)) >= 5)

# ---- pragmas
c = sqlite3.connect(mkdb()); c.execute('PRAGMA synchronous=FULL'); S.K('synchronous=FULL reads back as 2 (what the writer checks)', c.execute('PRAGMA synchronous').fetchone()[0] == 2)
b26 = sql_blocks(doc_page('contract/connections.md'))
want = {'journal_mode': 'WAL', 'synchronous': 'FULL', 'foreign_keys': 'ON', 'recursive_triggers': 'ON', 'trusted_schema': 'OFF'}
S.K('the contract/connections block sets every pragma the contract names', b26 and all(re.search(rf'PRAGMA {k}\s*=\s*{v}', b26[0]) for k, v in want.items()), b26[:1])
head = DDL[:DDL.index('PRAGMA application_id')]
S.K('the DDL header names SQLite >= 3.51.3, the pragmas and BEGIN IMMEDIATE', all(x in head for x in ('3.51.3', 'foreign_keys = ON', 'recursive_triggers = ON', 'synchronous = FULL', 'trusted_schema = OFF', 'BEGIN IMMEDIATE')))
c = sqlite3.connect(':memory:', isolation_level=None); c.executescript(DDL)
S.K('journal_mode=WAL is stored in the file by the DDL', sqlite3.connect(mkdb()).execute('PRAGMA journal_mode').fetchone()[0] == 'wal')
c.execute('BEGIN'); c.execute('PRAGMA foreign_keys=ON'); inside = c.execute('PRAGMA foreign_keys').fetchone()[0]; c.execute('COMMIT')
S.K('PRAGMA foreign_keys is a silent no-op inside a transaction', inside == 0)
meta = dict(c.execute('select key, value from lifelog_meta'))
S.K("lifelog_meta.sqlite names 3.51.3 and 3.53", '3.51.3' in meta.get('sqlite', '') and '3.53' in meta.get('sqlite', ''))
S.K('this suite runs on such a SQLite', tuple(map(int, sqlite3.sqlite_version.split('.'))) >= (3, 51, 3), sqlite3.sqlite_version)

# ---- read-only readers under WAL
path = mkdb(); w = conn(path); w.execute('BEGIN IMMEDIATE'); ent(w, 'page')
ro = sqlite3.connect(f'file:{path}?mode=ro', uri=True, isolation_level=None)
S.K('a mode=ro reader is not blocked by an open write transaction', ro.execute('select count(*) from entities').fetchone()[0] == 0)
w.execute('COMMIT'); S.K('...and sees the commit', ro.execute('select count(*) from entities').fetchone()[0] == 1)
for st in ("INSERT INTO lifelog_meta VALUES ('x','y')", 'DELETE FROM entities', 'UPDATE entities SET deleted_at=NULL', 'DROP TABLE lifelog_meta'):
    S.K(f'mode=ro refuses: {st[:30]}', 'readonly' in tryx(ro, st))
t0 = time.time(); w.execute("INSERT INTO lifelog_meta VALUES ('k','v')")
S.K('the writer is not slowed by a connected reader', time.time() - t0 < 0.5)
r = subprocess.run(['sqlite3', '-readonly', path, "INSERT INTO lifelog_meta VALUES ('z','z')"], capture_output=True, text=True, stdin=subprocess.DEVNULL)
S.K('sqlite3 -readonly refuses writes', 'readonly' in r.stderr + r.stdout)

# ---- a hardened connection: DEFENSIVE + the contract/connections pragmas
c = sqlite3.connect(':memory:', isolation_level=None); c.setconfig(sqlite3.SQLITE_DBCONFIG_DEFENSIVE, True)
for st in statements(b26[0] if b26 else ''): c.execute(st)
try:
    c.executescript(DDL); p = page(c, 'Hardened'); c.execute("UPDATE pages SET body='searchable words' WHERE id=?", (p,))
    mm = page(c, 'Another', body='another'); link(c, mm, p, 'wikilink'); named(c, 'person', 'Ada')
    c.execute("DELETE FROM links WHERE from_id=? AND kind='wikilink' AND to_id NOT IN (SELECT value FROM json_each('[]'))", (mm,))
    got = (one(c, "SELECT count(*) FROM pages_fts WHERE pages_fts MATCH 'searchable'"), one(c, 'SELECT count(*) FROM ghost_pages'), one(c, 'SELECT count(*) FROM measurement_values'))
    hard = 'OK'
except sqlite3.Error as e: hard, got = 'ERR ' + str(e), None
S.K('with DEFENSIVE and trusted_schema=OFF: DDL, writes, FTS, views, json_each and a named entity all work', hard == 'OK' and got == (1, 0, 0), (hard, got))
S.K('...and a write to an FTS shadow table is refused', tryx(c, 'DELETE FROM pages_fts_data').startswith('ERR'))
S.done()
