"""Identity, provenance and deletion (SCHEMA.md §2.2, §2.3, D3, D8, D11): the supertype and its composite foreign keys,
ids carried by RETURNING, `source` on every row, `import_key` on entities (a re-run inserts nothing, §6.21), no hard deletes, updated_at kept by triggers."""
import os, re, sys, time
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('identity')

# ---- the supertype: a row's type and its table agree
c = fresh()
S.K('entities.entity_type rejects an unknown type', tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('foo',{NOW},{NOW},'ui')").startswith('ERR'))
S.K('entities.entity_type has no event (D22)', tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('event',{NOW},{NOW},'ui')").startswith('ERR'))
e = thing(c, 'page')
S.K('a task row cannot take a page\'s id', tryx(c, "INSERT INTO tasks(id,name) VALUES (?, 't')", (e,)).startswith('ERR'))
S.K('a domain row needs its entities row', tryx(c, "INSERT INTO tasks(id,name) VALUES (9999, 'x')").startswith('ERR'))
S.K('a domain row cannot claim another type', tryx(c, "INSERT INTO tasks(id,entity_type,name) VALUES (?, 'page', 'x')", (ent(c, 'page'),)).startswith('ERR'))
S.K('pages.entity_type cannot be task', tryx(c, "INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, 'task', 'T', 't')", (ent(c, 'task'),)).startswith('ERR'))
S.K('the tables without a single INTEGER primary key are exactly the three registries',
    sorted(t for (t,) in c.execute("select name from sqlite_schema where type='table' and name not like 'pages_fts%'")
           if [r[2] for r in c.execute(f"pragma table_info('{t}')") if r[5]] != ['INTEGER']) == ['currencies', 'lifelog_meta', 'link_kinds'])

# ---- ids by RETURNING
c = fresh()
for i in range(10): page(c, f'Seed {i}')                       # entity ids run ahead of link ids
m = ent(c, 'page')
c.execute("INSERT INTO pages(id,title,title_key,day) VALUES (?, '2026-09-30', '2026-09-30', '2026-09-30')", (m,)); g = page(c, 'Target')
link(c, m, g, 'wikilink')
S.K('last_insert_rowid() moves: after a link insert it is the link\'s id, not the page\'s', one(c, 'select last_insert_rowid()') != m)
S.K('no §6 block uses last_insert_rowid()', not [h for h, s in docsql.cookbook_blocks(DOC) if 'last_insert_rowid' in s])
c = fresh()
for i in range(10): page(c, f'Seed {i}')                       # entity ids run ahead of link ids
P = {}
def sync(st):                                                   # the §6.13 link sync, inside §6.1's transaction
    if st.upper().startswith('UPDATE PAGES SET BODY'):
        link(c, P['page_id'], page(c, 'Lifelog'), 'wikilink')
try: run_block(c, block('6.1'), P, sync); r = 'OK'
except sqlite3.Error as ex: r = 'ERR ' + str(ex)
S.K('§6.1 run literally, with a link sync inside: the mood reading points at the day page the RETURNING gave',
    r == 'OK' and c.execute("select captured_with_id from measurements").fetchall() == [(P.get('page_id'),)] and one(c, "select title from pages where id=?", (P.get('page_id'),)) == '2026-09-29', (r, P))
c = fresh()
for i in range(10): page(c, f'Seed {i}')
m9 = day_page(c, '2026-09-30', 'call the dentist'); P = dict(page_id=m9, due_day='2026-10-05')
try: run_block(c, block('6.9'), P); r = 'OK'
except sqlite3.Error as ex: r = 'ERR ' + str(ex)
S.K('§6.9 run literally: the task it RETURNed is spawned from the day page',
    r == 'OK' and c.execute("select t.id, l.to_id from tasks t join links l on l.from_id=t.id and l.kind='spawned'").fetchall() == [(P.get('task_id'), m9)], (r, P))

# ---- source on every row
c = fresh()
for t in ('entities', 'links', 'measurements', 'balances'):
    info = {r[1]: r for r in c.execute(f"pragma table_info('{t}')")}
    S.K(f'{t}.source is TEXT NOT NULL with no default', 'source' in info and info['source'][2] == 'TEXT' and info['source'][3] == 1 and info['source'][4] is None)
S.K('an entities row without a source is refused', tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at) VALUES ('page',{NOW},{NOW})").startswith('ERR'))
h = named(c, 'holding')
for v in ('ui', 'cli', 'api', 'agent:claude', 'import:bank_csv', 'import:health-2026.v2'):
    S.K(f'source {v!r} accepted on entities, measurements and balances', tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page',{NOW},{NOW},?)", (v,)) == 'OK'
        and measure(c, 1, '2026-01-01', 3, source=v) == 'OK' and balance(c, h, '2026-01-01', 1, source=v) == 'OK')
for v in ('', 'UI', 'agent claude', 'x' * 65, 'a/b', 'manual ', None):
    S.K(f'source {v!r} rejected on entities, measurements and balances', tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page',{NOW},{NOW},?)", (v,)).startswith('ERR')
        and measure(c, 1, '2026-01-01', 3, source=v).startswith('ERR') and balance(c, h, '2026-01-01', 1, source=v).startswith('ERR'))
a, b = named(c, 'person'), named(c, 'person')
S.K('entities.source cannot change', tryx(c, "UPDATE entities SET source='cli' WHERE id=?", (a,)).startswith('ERR'))
S.K('a no-op full-row update and a tombstone still pass', tryx(c, f"UPDATE entities SET source=source, deleted_at={NOW} WHERE id=?", (a,)) == 'OK')
link(c, a, b, 'friend', 'agent:claude')
S.K('the mirror of a symmetric link copies its source', c.execute('select source from links where from_id=? and to_id=?', (b, a)).fetchone() == ('agent:claude',))
S.K('links.source cannot change', tryx(c, "UPDATE links SET source='ui' WHERE from_id=?", (a,)).startswith('ERR'))
S.K('a link note edit still passes', tryx(c, "UPDATE links SET note='n', source=source WHERE from_id=?", (a,)) == 'OK')

# ---- no hard deletes
c = fresh(); ids = {t: thing(c, t) for t in ('page', 'task', 'person', 'place', 'holding')}
n0 = one(c, 'select count(*) from entities')
for typ, tbl in (('page', 'pages'), ('task', 'tasks'), ('person', 'people'), ('place', 'places'), ('holding', 'holdings')):
    S.K(f'DELETE FROM {tbl} is refused', tryx(c, f'DELETE FROM {tbl} WHERE id=?', (ids[typ],)).startswith('ERR'))
    S.K(f'DELETE of the {typ} entities row is refused', tryx(c, 'DELETE FROM entities WHERE id=?', (ids[typ],)).startswith('ERR'))
S.K('the page rows of named entities cannot be deleted either', tryx(c, 'DELETE FROM pages WHERE id=?', (ids['person'],)).startswith('ERR'))
S.K('nothing was removed (five entities, one id each)', one(c, 'select count(*) from entities') == n0 == 5)
S.K('REPLACE INTO pages is blocked under recursive_triggers=ON', tryx(c, f"REPLACE INTO pages(id,entity_type,title,title_key,body) VALUES ({ids['page']},'page','Replaced','replaced','overwritten')").startswith('ERR')
    and one(c, 'select body from pages where id=?', (ids['page'],)) == 'x')
o = ent(c, 'page')
S.K('an orphan entities row cannot be deleted either (only the entities trigger can stop it)', tryx(c, 'DELETE FROM entities WHERE id=?', (o,)).startswith('ERR'))
c.execute('PRAGMA foreign_keys=OFF')
S.K('with foreign_keys=OFF: DELETE FROM entities still refused (the trigger, not the FK)', tryx(c, 'DELETE FROM entities WHERE id=?', (ids['task'],)).startswith('ERR'))
S.K('with foreign_keys=OFF: DELETE FROM tasks still refused', tryx(c, 'DELETE FROM tasks WHERE id=?', (ids['task'],)).startswith('ERR'))
c.execute('PRAGMA foreign_keys=ON')
link(c, ids['person'], ids['place'], 'visited')
S.K('links rows may be hard-deleted (the one such table)', tryx(c, 'DELETE FROM links WHERE from_id=?', (ids['person'],)) == 'OK' and one(c, 'select count(*) from links') == 0)

# ---- updated_at, kept by triggers
c = fresh(); ids = {t: thing(c, t) for t in ('page', 'task', 'person', 'place', 'holding')}
c.execute("UPDATE entities SET updated_at='2000-01-01T00:00:00.000Z', created_at='2000-01-01T00:00:00.000Z'")
for typ, sql in (('page', "UPDATE pages SET body='y' WHERE id=?"), ('task', "UPDATE tasks SET due_day='2026-01-01' WHERE id=?"),
                 ('person', "UPDATE people SET nickname='n' WHERE id=?"),
                 ('place', "UPDATE places SET lat=48.1, lon=11.6 WHERE id=?"), ('holding', "UPDATE holdings SET institution='Bank' WHERE id=?")):
    c.execute(sql, (ids[typ],))
    S.K(f'updating a {typ} bumps entities.updated_at', one(c, 'select updated_at > created_at from entities where id=?', (ids[typ],)) == 1)
# ---- entities.import_key: a re-run, a replay or a retry inserts nothing (§6.21)
c = fresh(); B = statements(block('6.21'))
ins_ent, ins_task, upd = [s for s in B if code(s).split()[0].rstrip(';').upper() not in ('BEGIN', 'COMMIT')]
def run_import(key):
    got = c.execute(ins_ent, {'import_key': key}).fetchall()
    if got: c.execute(ins_task, {'task_id': got[0][0]})
    return got
first = run_import('todo-1'); again = run_import('todo-1')
S.K('§6.21: the first run returns an id, the re-run returns none and adds no task',
    len(first) == 1 and again == [] and one(c, 'select count(*) from tasks') == 1 and integrity_ok(c))
S.K('the same import_key under another source is another row (per-source namespace)',
    tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source,import_key) VALUES ('task',{NOW},{NOW},'import:health','todo-1')") == 'OK')
S.K('rows without an import_key never collide',
    all(tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('task',{NOW},{NOW},'import:todo')") == 'OK' for _ in range(2)))
S.K('a plain INSERT of a known key is refused (the index is unique)',
    'UNIQUE' in tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,source,import_key) VALUES ('task',{NOW},{NOW},'import:todo','todo-1')"))
S.K('entities.import_key cannot change', 'never changed' in tryx(c, "UPDATE entities SET import_key='todo-2' WHERE id=?", (first[0][0],)))
S.K('...nor be cleared', 'never changed' in tryx(c, "UPDATE entities SET import_key=NULL WHERE id=?", (first[0][0],)))
c.execute("UPDATE tasks SET due_day='2026-10-01'"); c.execute(upd, {'import_key': 'todo-1'})
S.K('§6.21: a moved task is updated in its own row', one(c, 'select due_day from tasks where id=?', (first[0][0],)) == '2026-10-15')
c.execute("UPDATE tasks SET due_day='2026-10-01'"); c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (first[0][0],))
c.execute(upd, {'import_key': 'todo-1'}); again = run_import('todo-1')
S.K('§6.21: a tombstoned import is neither updated nor inserted again',
    one(c, 'select due_day from tasks where id=?', (first[0][0],)) == '2026-10-01' and again == [] and one(c, 'select count(*) from tasks') == 1)

c = fresh(); ids = {t: thing(c, t) for t in ('page', 'task', 'person', 'place', 'holding')}
c.execute("UPDATE entities SET updated_at='2000-01-01T00:00:00.000Z'"); time.sleep(0.002)
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (ids['page'],))
r = c.execute('select updated_at, deleted_at from entities where id=?', (ids['page'],)).fetchone()
S.K('a tombstone bumps updated_at to the tombstone instant, and the trigger does not re-fire itself', r[0] == r[1] and r[0] > '2000')
S.done()
