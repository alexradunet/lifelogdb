import sqlite3, os, sys, time
DOCPATH = os.environ.get('DOC') or os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', 'SCHEMA.md')
DDL = open(sys.argv[1] if len(sys.argv)>1 else os.environ['DDL'], encoding='utf-8').read()
NOW = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
def fresh(path=':memory:'):
    c = sqlite3.connect(path, isolation_level=None)
    c.executescript(DDL)
    c.execute('PRAGMA foreign_keys=ON')
    return c
NAMED = ('person', 'place', 'holding')      # D20: a named entity has a page, inserted first
_handles = [0]
def ent(c, typ):
    pg = None
    if typ in NAMED:
        _handles[0] += 1; t = f'Handle {_handles[0]}'
        c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES ('page',{NOW},{NOW})"); pg = c.execute('select last_insert_rowid()').fetchone()[0]
        c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES (?,'page',?,?)", (pg, t, t.lower()))
    c.execute(f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES (?,{NOW},{NOW},?)", (typ, pg))
    return c.execute('select last_insert_rowid()').fetchone()[0]
def memo(c, body='x', day='2026-06-09'):
    i = ent(c,'page'); c.execute("INSERT INTO pages(id,kind,day,body) VALUES (?,?,?,?)",(i,'memo',day,body)); return i
def tryx(c, sql, args=()):
    try:
        cur = c.execute(sql, args); return ('OK', cur.rowcount)
    except sqlite3.Error as e:
        return ('ERR', f'{type(e).__name__}: {e}')
def show(tag, expect, got): print(f'{tag:<58} expect={expect:<8} got={got}')

# ---- P1: INSERT OR IGNORE swallows CHECK/NOT NULL violations (import convention §2.4)
c = fresh()
c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')")
r = tryx(c, "INSERT OR IGNORE INTO measurements(metric_id,day,value,import_id) VALUES (2,'2026-9-3',70,'a')")
n = c.execute("select count(*) from measurements").fetchone()[0]
show('P1a OR IGNORE + malformed day (CHECK)', 'ERR?', (r, 'rows=%d'%n))
r = tryx(c, "INSERT OR IGNORE INTO measurements(metric_id,day,value,import_id) VALUES (2,'2026-06-09',NULL,'b')")
show('P1b OR IGNORE + NULL value (NOT NULL)', 'ERR?', (r, 'rows=%d'%c.execute("select count(*) from measurements").fetchone()[0]))
r = tryx(c, "INSERT OR IGNORE INTO measurements(metric_id,day,value,import_id) VALUES (999,'2026-06-09',1,'c')")
show('P1c OR IGNORE + dangling metric_id (FK)', 'ERR', r)
c.execute("INSERT INTO measurements(metric_id,day,value) VALUES (2,'2026-06-01',70)")
r = tryx(c, "INSERT OR IGNORE INTO measurements(metric_id,day,value,supersedes_id) VALUES (2,'2026-06-01',71,1)")
show('P1d OR IGNORE + supersede trigger ok', 'OK', r)
r = tryx(c, "INSERT OR IGNORE INTO measurements(metric_id,day,value,supersedes_id) VALUES (1,'2026-06-01',3,1)")
show('P1e OR IGNORE + cross-metric supersede (RAISE ABORT)', 'ERR', r)
r = tryx(c, "INSERT INTO measurements(metric_id,day,value,import_id) VALUES (2,'2026-06-09',70,'z') ON CONFLICT DO NOTHING")
r2 = tryx(c, "INSERT INTO measurements(metric_id,day,value,import_id) VALUES (2,'2026-9-3',70,'y') ON CONFLICT(import_id,metric_id) WHERE import_id IS NOT NULL DO NOTHING")
show('P1f ON CONFLICT(target) DO NOTHING + malformed day', 'ERR', r2)

# ---- P2: import_id unique only per (import_id, metric_id): two sources collide
c = fresh()
c.execute("INSERT INTO metrics(name,unit) VALUES ('steps','n')")
c.execute("INSERT OR IGNORE INTO measurements(metric_id,day,value,source,import_id) VALUES (2,'2026-06-09',8000,'apple_health','1001')")
c.execute("INSERT OR IGNORE INTO measurements(metric_id,day,value,source,import_id) VALUES (2,'2026-06-10',9000,'garmin','1001')")
show('P2 two sources, same import_id: rows kept', 2, c.execute("select count(*) from measurements where metric_id=2").fetchone()[0])

# ---- P3: hard deletes of entities are not blocked (convention only)
c = fresh(); i = memo(c)
show('P3a DELETE FROM entities (FK-protected)', 'ERR', tryx(c, "DELETE FROM entities WHERE id=?", (i,)))
show('P3b DELETE FROM pages', 'ERR?', tryx(c, "DELETE FROM pages WHERE id=?", (i,)))
show('P3c then DELETE FROM entities', 'ERR?', tryx(c, "DELETE FROM entities WHERE id=?", (i,)))
show('P3d entities rows left', 0, c.execute("select count(*) from entities").fetchone()[0])
c = fresh(); i = ent(c,'page')  # entity with no domain row
show('P3e orphan entity (type page, no pages row) accepted', 'OK?', ('rows', c.execute("select count(*) from entities where type='page' and id not in (select id from pages)").fetchone()[0]))
# id reuse after deleting the newest row (no AUTOINCREMENT)
c = fresh(); a = ent(c,'place'); c.execute("INSERT INTO places(id,name) VALUES (?, 'A')",(a,)); 
c.execute("DELETE FROM places WHERE id=?", (a,)); c.execute("DELETE FROM entities WHERE id=?", (a,))
b = ent(c,'place'); show('P3f rowid reused after hard delete', 'a!=b?', (a,b))

# ---- P4: measurements cannot be retracted
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')")
c.execute("INSERT INTO measurements(metric_id,day,value) VALUES (2,'2026-06-01',70)")
show('P4a supersede with NULL (retract) ', 'ERR', tryx(c, "INSERT INTO measurements(metric_id,day,value,supersedes_id) VALUES (2,'2026-06-01',NULL,1)"))
c.execute("INSERT INTO measurements(metric_id,day,value) VALUES (1,'2026-06-01',3)")  # mood tap logged by mistake (id 2)
show('P4b move a mis-metric row (weight->mood)', 'ERR', tryx(c, "INSERT INTO measurements(metric_id,day,value,supersedes_id) VALUES (2,'2026-06-01',70,2)"))

# ---- P5: metrics.unit / name are mutable: silently reinterprets history
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')")
c.execute("INSERT INTO measurements(metric_id,day,value) VALUES (2,'2026-06-01',70)")
show('P5a UPDATE metrics SET unit=lb', 'ERR?', tryx(c, "UPDATE metrics SET unit='lb' WHERE name='weight'"))
show('P5b metric name with spaces/uppercase accepted', 'ERR?', tryx(c, "INSERT INTO metrics(name,unit) VALUES (' Blood Pressure (sys) ','mmHg')"))

# ---- P6: D15 'spawn next task via spawned' vs D8 endpoint types
c = fresh(); t1=ent(c,'task'); c.execute("INSERT INTO tasks(id,title,due_day) VALUES (?,'rent','2026-01-01')",(t1,))
t2=ent(c,'task'); c.execute("INSERT INTO tasks(id,title,due_day) VALUES (?,'rent','2026-02-01')",(t2,))
show('P6 links(spawned) task->task (D15 says allowed)', 'ERR', tryx(c, f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES ({t2},{t1},'spawned',{NOW})"))

# ---- P7: D16 motivation: 'everything in Japan' needs place containment
c = fresh(); p1=ent(c,'place'); c.execute("INSERT INTO places(id,name) VALUES (?, 'Japan')",(p1,)); p2=ent(c,'place'); c.execute("INSERT INTO places(id,name) VALUES (?, 'Tokyo')",(p2,))
print('P7 seeded kinds usable place->place:', c.execute("select kind from link_kinds where (from_types is null or instr(','||from_types||',', ',place,')>0) and (to_types is null or instr(','||to_types||',', ',place,')>0)").fetchall())
# ---- P8: events.place_id vs links(visited event->place): two homes
show('P8 event->place expressible two ways', 'both', 'events.place_id + link visited(event,place) (see link_kinds)')

# ---- P9: wikilink pathology: a title the CHECK rejects blocks the auto-create
c = fresh()
for t in ['Health/Diet','Re: plan','Target|alias','C#']:
    i = ent(c,'page')
    r = tryx(c, "INSERT INTO pages(id,kind,title,title_key) VALUES (?,'page',?,?)", (i,t,t.lower()))
    show(f'P9 auto-create page for [[{t}]]', 'ERR?', r[0] if r[0]=='OK' else r[1][:60])
