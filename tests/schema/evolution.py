"""Evolution after the freeze (D13, D17, architecture/non-goals): every CHECK is named and droppable by name; an unnamed one is not,
a looser second CHECK does not relax the first, ADD CONSTRAINT checks existing rows; enums widen on a populated database;
the partial-date and tokenizer paths of architecture/non-goals work; a link kind widens by migration; a promotion can strand a link (architecture/non-goals); an
entity uid is additive (D3); a comment outside a statement is not stored."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('evolution')

def populated():
    c = fresh()
    for t in ('page', 'person', 'place'): thing(c, t)
    page(c, 'Some page'); return c

# ---- every CHECK named, every name droppable
body = code(DDL)
nchk = len(re.findall(r'\bCHECK\s*\(', body)); names = re.findall(r'\bCONSTRAINT\s+(\w+)\s+CHECK\s*\(', body)
S.K('every CHECK in schema is named', nchk > 0 and nchk == len(names), (nchk, len(names)))
S.K('constraint names are unique', len(names) == len(set(names)), [n for n in names if names.count(n) > 1])
S.K('names are <table>_<column or rule>', all(any(n.startswith(t + '_') for t in re.findall(r'CREATE TABLE (\w+)', body)) for n in names), [n for n in names if not any(n.startswith(t + '_') for t in re.findall(r'CREATE TABLE (\w+)', body))])
tbl_of = {}
for m in re.finditer(r'CREATE TABLE (\w+) \((.*?)\n\) STRICT;', body, re.S):
    for n in re.findall(r'\bCONSTRAINT\s+(\w+)\s+CHECK', m.group(2)): tbl_of[n] = m.group(1)
c = populated(); bad = []
for n, t in tbl_of.items():
    c.execute('BEGIN')
    if tryx(c, f'ALTER TABLE {t} DROP CONSTRAINT {n}') != 'OK': bad.append(n)
    c.execute('ROLLBACK')
S.K('every named CHECK can be dropped by name on a populated database', tbl_of and not bad, bad)
S.K('integrity clean after the drops were rolled back', integrity_ok(c))

# ---- what a name buys (SQLite >= 3.53)
t = sqlite3.connect(':memory:', isolation_level=None)
t.execute("CREATE TABLE u (x INTEGER CHECK (x > 0)) STRICT")
S.K('an unnamed CHECK cannot be dropped', 'no such constraint' in tryx(t, 'ALTER TABLE u DROP CONSTRAINT x'))
t.execute("CREATE TABLE v (x INTEGER CONSTRAINT v_x CHECK (x > 0)) STRICT")
t.execute('ALTER TABLE v ADD CONSTRAINT v_x2 CHECK (x > -10)')
S.K('adding a looser second CHECK does not relax the first (both apply)', tryx(t, 'INSERT INTO v VALUES (-5)').startswith('ERR'))
t.execute('INSERT INTO v VALUES (5)')
S.K('ADD CONSTRAINT checks the existing rows (tightening is as safe as loosening)', tryx(t, 'ALTER TABLE v ADD CONSTRAINT v_x3 CHECK (x > 10)').startswith('ERR'))

# ---- widening enums on a populated database
WIDEN = {
 'entities_entity_type': ('entities', "entity_type IN ('page','person','place','vehicle')", lambda c: tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('vehicle',{NOW},{NOW},'ui')")),
}
def named_page(c, typ):
    i = ent(c, typ); c.execute("INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, ?, ?, ?)", (i, typ, f'W{i}', f'w{i}')); return i
for name, (tbl, expr, use) in WIDEN.items():
    c = populated()
    S.K(f'{name}: the new value is refused before', use(c).startswith('ERR'))
    c.execute('BEGIN'); r1 = tryx(c, f'ALTER TABLE {tbl} DROP CONSTRAINT {name}'); r2 = tryx(c, f'ALTER TABLE {tbl} ADD CONSTRAINT {name} CHECK ({expr})'); c.execute('COMMIT')
    S.K(f'{name}: DROP and ADD the widened CHECK in one transaction', r1 == r2 == 'OK', (r1, r2))
    S.K(f'{name}: the new value is accepted', use(c) == 'OK')
    S.K(f'{name}: integrity and foreign keys clean', integrity_ok(c))

# ---- architecture/non-goals partial dates: DROP + ADD of people_birth_day
c = fresh(); p1 = named(c, 'person', 'Ada', birth_day='1815-12-10'); p2 = named(c, 'person', 'Ancestor')
S.K('birth_day refuses 1870 and 1870-05 today', tryx(c, "UPDATE people SET birth_day='1870' WHERE id=?", (p2,)).startswith('ERR') and tryx(c, "UPDATE people SET birth_day='1870-05' WHERE id=?", (p2,)).startswith('ERR'))
c.execute('BEGIN')
r = (tryx(c, 'ALTER TABLE people DROP CONSTRAINT people_birth_day'),
     tryx(c, "ALTER TABLE people ADD CONSTRAINT people_birth_day CHECK (birth_day IS NULL OR date(birth_day) IS birth_day OR birth_day GLOB '[0-9][0-9][0-9][0-9]' OR (birth_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]' AND substr(birth_day, 6, 2) BETWEEN '01' AND '12'))"))
c.execute('COMMIT')
S.K('the named CHECK is dropped and re-added looser on a populated STRICT table', r == ('OK', 'OK'), r)
S.K('YYYY and YYYY-MM are stored; full dates are untouched', tryx(c, "UPDATE people SET birth_day='1870' WHERE id=?", (p2,)) == 'OK' and tryx(c, "UPDATE people SET birth_day='1870-05' WHERE id=?", (p2,)) == 'OK'
    and one(c, 'select birth_day from people where id=?', (p1,)) == '1815-12-10')
S.K('junk and month 13 are still refused', all(tryx(c, 'UPDATE people SET birth_day=? WHERE id=?', (v, p2)).startswith('ERR') for v in ('abc', '1870-13', '1870-5', '18700', '1870-05-01x')))
S.K('integrity clean and the touch trigger still works', integrity_ok(c) and one(c, 'select updated_at >= created_at from entities where id=?', (p2,)) == 1)

# ---- architecture/non-goals CJK search: the tokenizer switch is one transaction on a derived index
c = fresh(); jp = '日本語のノートを書く'
for i, b in enumerate((jp, 'Zürich café notes', 'plain english text')): page(c, f'Text {i}', body=b)
def hits(q): return one(c, 'select count(*) from pages_fts where pages_fts match ?', (q,))
S.K('before: unicode61 finds the whole CJK run and accented words, not a part of the run', (hits(jp), hits('zurich'), hits('本語'), hits('ノート')) == (1, 1, 0, 0))
c.execute('BEGIN IMMEDIATE'); c.execute('DROP TABLE pages_fts')
c.execute("CREATE VIRTUAL TABLE pages_fts USING fts5(title, body, content='pages', content_rowid='id', tokenize='trigram remove_diacritics 1')")
c.execute("INSERT INTO pages_fts(pages_fts) VALUES('rebuild')"); c.execute('COMMIT')
S.K('after: trigram finds 3+-character parts and still folds accents', (hits('本語の'), hits('ノート'), hits('日本語'), hits('zurich')) == (1, 1, 1, 1))
S.K('...but not a two-character word (the known limit)', hits('本語') == 0)
m = page(c, 'New text', body='これは新しい記録です'); n1 = hits('新しい'); c.execute("UPDATE pages SET body='全く別の内容' WHERE id=?", (m,))
S.K('the sync triggers keep working after the switch', n1 == 1 and (hits('新しい'), hits('別の内')) == (0, 1))
S.K('...and the FTS integrity-check passes', tryx(c, "INSERT INTO pages_fts(pages_fts, rank) VALUES('integrity-check', 1)") == 'OK')

# ---- a link kind is widened by a migration (D8): drop the guard, update the row, recreate the guard
c = populated(); guard = c.execute("select sql from sqlite_schema where name='link_kinds_structure_fixed'").fetchone()[0]
c.execute('BEGIN IMMEDIATE'); c.execute('DROP TRIGGER link_kinds_structure_fixed')
c.execute("UPDATE link_kinds SET to_types = 'person,place' WHERE kind = 'at'"); c.execute(guard); c.execute('COMMIT')
S.K('after the migration a day page may be at a person\'s home page', link(c, day_page(c), named(c, 'person'), 'at') == 'OK')
S.K('...and the guard is back: the next change is refused', 'fixed at registration' in tryx(c, "UPDATE link_kinds SET to_types = NULL WHERE kind = 'at'"))

# ---- a promotion can leave a link its kind now refuses (architecture/non-goals): links are checked at insert only
c = fresh(); d = day_page(c, '2026-07-31'); w = named(c, 'place', 'Lakeside')
S.K('a day page at the place [[Lakeside]]', link(c, d, w, 'at') == 'OK')
c.execute("UPDATE entities SET entity_type = 'person' WHERE id = ?", (w,)); domain(c, 'person', w)
S.K('turning Lakeside into a person keeps the old at edge, which a new insert would refuse',
    one(c, "select count(*) from links where to_id = ? and kind = 'at'", (w,)) == 1 and 'endpoint type not allowed' in link(c, day_page(c, '2026-08-01'), w, 'at'))

# ---- an entity uid is additive after the freeze (D3): add, backfill, unique index, then NOT NULL
import uuid
c = populated(); c.execute('BEGIN IMMEDIATE')
c.execute("ALTER TABLE entities ADD COLUMN uid TEXT CONSTRAINT entities_uid CHECK (uid IS NULL OR length(uid) = 36)")
for (i,) in c.execute('select id from entities').fetchall(): c.execute('UPDATE entities SET uid = ? WHERE id = ?', (str(uuid.uuid4()), i))
c.execute('CREATE UNIQUE INDEX entities_uid_unique ON entities(uid)'); c.execute('ALTER TABLE entities ALTER COLUMN uid SET NOT NULL'); c.execute('COMMIT')
S.K('every existing entity has a unique uid, and an entity without one is refused afterwards',
    one(c, 'select count(distinct uid) = count(*) from entities') == 1 and 'NOT NULL' in tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page',{NOW},{NOW},'ui')"))
S.K('...with integrity and foreign keys clean', integrity_ok(c))

# ---- comments: inside a statement kept, outside dropped (why the rules live inside)
t = sqlite3.connect(':memory:'); t.executescript('-- outside comment\nCREATE TABLE k (\n  -- inside comment\n  x INTEGER\n) STRICT;')
sql = t.execute("select sql from sqlite_schema where name='k'").fetchone()[0]
S.K('a comment inside a CREATE statement is stored in the file, one outside is not', 'inside comment' in sql and 'outside comment' not in ' '.join(r[0] or '' for r in t.execute('select sql from sqlite_schema')))
S.done()
