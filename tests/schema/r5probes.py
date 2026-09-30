import sqlite3, sys, re
exec(open('probes1.py').read().split('# ---- P1:')[0])
res=[]
def P(label, expect, c, sql, args=()):
    r = tryx(c, sql, args); ok = r[0]==expect; res.append(ok)
    if not ok: print('  FAIL', label, '->', r)
    return r
def K(label, cond):
    res.append(bool(cond))
    if not cond: print('  FAIL', label)
RA = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
def m(c, metric, day, value, **kw):
    cols = dict(metric_id=metric, day=day, value=value, **kw)
    c.execute(f"INSERT INTO measurements({','.join(cols)},recorded_at) VALUES ({','.join('?'*len(cols))},{RA})", tuple(cols.values()))
def livedb():
    """a small populated database with one row of every entity type"""
    c = fresh(); ids = {}
    ids['page'] = memo(c,'hello')
    for typ,sql in (('event',"INSERT INTO events(id,title,start_day) VALUES (?, 'E','2026-06-01')"),('task',"INSERT INTO tasks(id,title) VALUES (?, 'T')"),
                    ('person',"INSERT INTO people(id,name) VALUES (?, 'P')"),('place',"INSERT INTO places(id,name) VALUES (?, 'Pl')"),
                    ('holding',"INSERT INTO holdings(id,name,side,currency) VALUES (?, 'A','asset','EUR')")):
        ids[typ] = ent(c,typ); c.execute(sql,(ids[typ],))
    return c, ids

# ---- R4-02 / R4-03: import idiom and key
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('steps','n')")   # metric id 2
imp = "INSERT INTO measurements(metric_id,day,value,source,import_id,recorded_at) VALUES (2,?,?,?,?,%s) ON CONFLICT(source,import_id,metric_id) WHERE import_id IS NOT NULL DO NOTHING" % RA
P('R4-03 first import row','OK',c,imp,('2026-06-09',8000,'apple','1001'))
r = P('R4-03 same source+id+metric again is a no-op','OK',c,imp,('2026-06-09',8000,'apple','1001')); K('R4-03 duplicate inserted 0 rows', r[1]==0)
P('R4-03 same import_id from another source is kept','OK',c,imp,('2026-06-10',9000,'garmin','1001'))
K('R4-03 both sources stored', c.execute("select count(*) from measurements where metric_id=2").fetchone()[0]==2)
P('R4-02 ON CONFLICT still raises on a bad day','ERR',c,imp,('2026-6-9',1,'apple','2002'))
r = tryx(c, f"INSERT OR IGNORE INTO measurements(metric_id,day,value,source,import_id,recorded_at) VALUES (2,'2026-6-9',1,'apple','2002',{RA})")
K('R4-02 (documented) OR IGNORE swallows a bad day', r==('OK',0))

# ---- R4-06: retraction of a measurement
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')")   # mood=1, weight=2
m(c,2,'2026-06-01',70); m(c,1,'2026-06-01',3)                                     # row 1 weight, row 2 = a mood tap logged by mistake
vis = lambda: c.execute("select id,metric_id,value from measurement_values order by id").fetchall()
P('R4-06 a first reading with NULL value is rejected','ERR',c,f"INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (2,'2026-06-02',NULL,{RA})")
P('R4-06 retract the mis-tap','OK',c,f"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (1,'2026-06-01',NULL,2,{RA})")
K('R4-06 the tap and its retraction are hidden, weight stays', vis()==[(1,2,70.0)])
P('R4-06 a retraction must be of the same metric','ERR',c,f"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (1,'2026-06-01',NULL,1,{RA})")
P('R4-06 a retracted row cannot be corrected twice','ERR',c,f"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (1,'2026-06-01',4,2,{RA})")
P('R4-06 correct the retraction = re-entry','OK',c,f"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (1,'2026-06-01',4,3,{RA})")
K('R4-06 re-entered value is visible again', (4,1,4.0) in [(r[0],r[1],r[2]) for r in c.execute("select id,metric_id,value from measurement_values")])
K('R4-06 nothing was deleted', c.execute("select count(*) from measurements").fetchone()[0]==4)
P('R4-06 UPDATE still rejected','ERR',c,"UPDATE measurements SET value=NULL WHERE id=1")

# ---- R4-10: recorded_at
c = fresh()
P('R4-10 measurement without recorded_at','ERR',c,"INSERT INTO measurements(metric_id,day,value) VALUES (1,'2026-06-01',3)")
P('R4-10 malformed recorded_at','ERR',c,"INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (1,'2026-06-01',3,'2026-06-01 10:00')")
P('R4-10 valid recorded_at','OK',c,f"INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (1,'2026-06-01',3,{RA})")

# ---- R4-07: no hard deletes of entities or domain rows
c, ids = livedb(); n_entities = c.execute("select count(*) from entities").fetchone()[0]
for typ,tbl in (('page','pages'),('event','events'),('task','tasks'),('person','people'),('place','places'),('holding','holdings')):
    P(f'R4-07 DELETE FROM {tbl}','ERR',c,f"DELETE FROM {tbl} WHERE id=?",(ids[typ],))
    P(f'R4-07 DELETE its entities row ({typ})','ERR',c,"DELETE FROM entities WHERE id=?",(ids[typ],))
K('R4-07 nothing removed', c.execute("select count(*) from entities").fetchone()[0]==n_entities==9)   # 6 types + the 3 pages of the named ones (D20)
P('R4-07 REPLACE INTO pages blocked (recursive_triggers=ON)','OK' if False else 'ERR',(lambda cc:(cc.execute('PRAGMA recursive_triggers=ON'),cc)[1])(c),
  f"REPLACE INTO pages(id,entity_type,kind,day,body) VALUES ({ids['page']},'page','memo','2026-06-09','overwritten')")
K('R4-07 the memo body survived the REPLACE', c.execute("select body from pages where id=?",(ids['page'],)).fetchone()[0]=='hello')
orph = ent(c,'page')                                   # an orphan entity (no pages row): only the entities trigger can stop its deletion
P('R4-07 DELETE of an orphan entities row','ERR',c,"DELETE FROM entities WHERE id=?",(orph,))
c.execute('PRAGMA foreign_keys=OFF')                   # a connection that forgot the pragma: the trigger, not the FK, must hold
P('R4-07 DELETE FROM entities with foreign_keys=OFF','ERR',c,"DELETE FROM entities WHERE id=?",(ids['task'],))
P('R4-07 DELETE FROM tasks with foreign_keys=OFF','ERR',c,"DELETE FROM tasks WHERE id=?",(ids['task'],))
c.execute('PRAGMA foreign_keys=ON')
P('R4-07 tombstoning still works','OK',c,f"UPDATE entities SET deleted_at={RA} WHERE id=?",(ids['page'],))
a,b = ids['person'], ids['place']
c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'visited',{RA})",(a,b))
P('R4-07 links may still be hard-deleted (D11)','OK',c,"DELETE FROM links WHERE from_id=?",(a,))
c.execute("INSERT INTO metrics(name,unit) VALUES ('w','kg')"); m(c,2,'2026-06-01',70)
P('R4-07 DELETE of a measurement still rejected','ERR',c,"DELETE FROM measurements")

# ---- R4-08: every named enum CHECK can be widened on a populated database
WIDEN = {
 'entities_type': ("entities", "type IN ('page','event','task','person','place','holding','vehicle')",
                   f"INSERT INTO entities(type,created_at,updated_at) VALUES ('vehicle',{RA},{RA})"),
 'pages_kind': ("pages", "kind IN ('memo','page','image')",
                "INSERT INTO pages(id,kind,title,title_key,day) VALUES ({e},'image','Pic','pic','2026-06-09')"),
 'tasks_status': ("tasks", "status IN ('open','done','dropped')", "INSERT INTO tasks(id,title,status) VALUES ({t},'x','dropped')"),
 'events_repeat': ("events", "repeat IN ('none','daily','weekly','monthly','yearly','hourly')", "INSERT INTO events(id,title,start_day,repeat) VALUES ({ev},'h','2026-06-01','hourly')"),
 'events_repeat_position': ("events", "repeat_position IS NULL OR (repeat = 'monthly' AND repeat_position IN ('first','second','third','fourth','fifth','last') AND repeat_weekday IN ('mo','tu','we','th','fr','sa','su'))",
                   "INSERT INTO events(id,title,start_day,repeat,repeat_position,repeat_weekday) VALUES ({ev},'f','2026-06-01','monthly','fifth','fr')"),
 'holdings_side': ("holdings", "side IN ('asset','liability','equity')", "INSERT INTO holdings(id,name,side,currency) VALUES ({ac},'Eq','equity','EUR')"),
}
for name,(tbl,expr,use) in WIDEN.items():
    c, ids = livedb()
    before = tryx(c, use.format(e=ent(c,'page'),t=ent(c,'task'),ev=ent(c,'event'),ac=ent(c,'holding')) if '{' in use else use)[0]
    P(f'R4-08 {name}: the new value is rejected before','ERR',c,use.format(e=ent(c,'page'),t=ent(c,'task'),ev=ent(c,'event'),ac=ent(c,'holding')) if '{' in use else use)
    c.execute('BEGIN')
    P(f'R4-08 {name}: DROP CONSTRAINT','OK',c,f"ALTER TABLE {tbl} DROP CONSTRAINT {name}")
    P(f'R4-08 {name}: ADD widened','OK',c,f"ALTER TABLE {tbl} ADD CONSTRAINT {name} CHECK ({expr})")
    c.execute('COMMIT')
    # the widened value needs a fresh entity row of the right type where a domain row is inserted
    if tbl == 'entities': u = use
    else:
        typ = {'pages':'page','tasks':'task','events':'event','holdings':'holding'}[tbl]
        u = use.format(e=ent(c,'page') if False else 0, t=0, ev=0, ac=0)
        newid = ent(c, typ); u = use.format(e=newid, t=newid, ev=newid, ac=newid)
    P(f'R4-08 {name}: the widened value is accepted','OK',c,u)
    K(f'R4-08 {name}: integrity + FK clean', c.execute('pragma integrity_check').fetchall()==[('ok',)] and c.execute('pragma foreign_key_check').fetchall()==[])
# ---- R4-09: metrics
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); m(c,2,'2026-06-01',70)
P('R4-09 UPDATE metrics.unit','ERR',c,"UPDATE metrics SET unit='lb' WHERE name='weight'")
P('R4-09 no-op SET unit=unit with a note edit','OK',c,"UPDATE metrics SET unit=unit, notes='body weight' WHERE name='weight'")
P('R4-09 rename (typo fix) allowed','OK',c,"UPDATE metrics SET name='body_weight' WHERE name='weight'")
for nm in ['Blood Pressure','bp sys','bp-sys','','Weight','x(y)','ünï']:
    P(f'R4-09 metric name {nm!r} rejected','ERR',c,"INSERT INTO metrics(name,unit) VALUES (?, 'x')",(nm,))
for nm in ['bp_sys','x1','_a','steps_walked']:
    P(f'R4-09 metric name {nm!r} accepted','OK',c,"INSERT INTO metrics(name,unit) VALUES (?, 'x')",(nm,))

# ---- R4-17: no-op writes to immutable columns pass, real changes do not
c, ids = livedb(); p2 = ent(c,'person'); c.execute("INSERT INTO people(id,name) VALUES (?, 'Q')",(p2,))
c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'attended',{RA})",(ids['person'], ids['event']))
P('R4-17 full-row UPDATE of a link, only note changes','OK',c,"UPDATE links SET note='hi', from_id=from_id, to_id=to_id, kind=kind WHERE kind='attended'")
P('R4-17 changing a link kind','ERR',c,"UPDATE links SET kind='related' WHERE kind='attended'")
P('R4-17 changing a link endpoint','ERR',c,"UPDATE links SET to_id=? WHERE kind='attended'",(ids['place'],))
P('R4-17 link_kinds: note edit with symmetric=symmetric','OK',c,"UPDATE link_kinds SET note='n', symmetric=symmetric, from_types=from_types WHERE kind='attended'")
P('R4-17 link_kinds: changing symmetric','ERR',c,"UPDATE link_kinds SET symmetric=1 WHERE kind='about'")
P('R4-17 link_kinds: changing to_types','ERR',c,"UPDATE link_kinds SET to_types='person' WHERE kind='about'")

# ---- R4-11 a/c: located-in and parent-of
c, ids = livedb()
japan, kanto, tokyo = ids['place'], ent(c,'place'), ent(c,'place')
c.execute("UPDATE places SET name='Japan' WHERE id=?",(japan,)); c.execute("INSERT INTO places(id,name) VALUES (?, 'Kanto')",(kanto,)); c.execute("INSERT INTO places(id,name) VALUES (?, 'Tokyo')",(tokyo,))
L = lambda f,t,k: (f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,?,{RA})",(f,t,k))
P('R4-11 Tokyo located-in Kanto','OK',c,*L(tokyo,kanto,'located-in')); P('R4-11 Kanto located-in Japan','OK',c,*L(kanto,japan,'located-in'))
P('R4-11 place located-in person rejected','ERR',c,*L(tokyo,ids['person'],'located-in')); P('R4-11 person located-in place rejected','ERR',c,*L(ids['person'],tokyo,'located-in'))
K('R4-11 located-in is one-way (no mirror)', c.execute("select count(*) from links where kind='located-in'").fetchone()[0]==2)
kid = ent(c,'person'); c.execute("INSERT INTO people(id,name) VALUES (?, 'Kid')",(kid,))
P('R4-11 parent-of person->person','OK',c,*L(ids['person'],kid,'parent-of')); P('R4-11 parent-of person->place rejected','ERR',c,*L(ids['person'],tokyo,'parent-of'))
K('R4-11 parent-of is one-way (direction kept)', c.execute("select count(*) from links where kind='parent-of'").fetchone()[0]==1)
ev = ent(c,'event'); c.execute("INSERT INTO events(id,title,start_day,place_id) VALUES (?, 'Trip','2019-04-02',?)",(ev,tokyo))
doc = open(DOCPATH,encoding='utf-8').read()
s = doc.index('### 6.19'); Q = re.search(r'```sql\n(.*?)\n```', doc[s:], re.S).group(1) if s>0 else None
rows = c.execute(Q, dict(place_id=japan, from_day='2019-01-01', to_day='2019-12-31')).fetchall()
K('R4-11 §6.19 finds the Tokyo trip inside Japan', len(rows)==1 and rows[0][0]=='Trip')
c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES ({japan},{tokyo},'located-in',{RA})")   # a cycle
K('R4-11 §6.19 terminates on a cycle', len(c.execute(Q, dict(place_id=japan, from_day='2019-01-01', to_day='2019-12-31')).fetchall())==1)

# ---- orphan-entity detector (SCHEMA.md 2.8)
doc = open(DOCPATH,encoding='utf-8').read()
i = doc.index('### 2.8 '); blk = re.search(r'```sql\n(.*?)\n```', doc[i:], re.S).group(1)
OQ = re.sub(r'\s*--.*$', '', [l for l in blk.splitlines() if l.startswith('SELECT id FROM entities')][0])
c, ids = livedb(); K('orphan query (2.8): healthy database returns nothing', c.execute(OQ).fetchall()==[])
o = ent(c,'place'); K('orphan query (2.8): finds an entity with no domain row', c.execute(OQ).fetchall()==[(o,)])
print(f'round-5 probes: {sum(res)}/{len(res)} met expectations')
