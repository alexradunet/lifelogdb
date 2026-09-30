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
doc = open(DOCPATH,encoding='utf-8').read()

# ---- R4-04: tz on entities and measurements
c = fresh()
for tz in ['Europe/Berlin','UTC','America/Argentina/Buenos_Aires','Etc/GMT+5','Asia/Kolkata','America/Port-au-Prince',None]:
    P(f'entities.tz {tz!r} accepted','OK',c,f"INSERT INTO entities(type,created_at,updated_at,tz) VALUES ('page',{RA},{RA},?)",(tz,))
    P(f'measurements.tz {tz!r} accepted','OK',c,f"INSERT INTO measurements(metric_id,day,taken_at,tz,value,recorded_at) VALUES (1,'2026-06-09','2026-06-09T22:30:00.000Z',?,3,{RA})",(tz,))
for tz in ['','Europe Berlin','x'*65,'Europe/Berlin\n','ünï/x','a;b','Europe/Berlin ']:
    P(f'entities.tz {tz[:14]!r} rejected','ERR',c,f"INSERT INTO entities(type,created_at,updated_at,tz) VALUES ('page',{RA},{RA},?)",(tz,))
    P(f'measurements.tz {tz[:14]!r} rejected','ERR',c,f"INSERT INTO measurements(metric_id,day,tz,value,recorded_at) VALUES (1,'2026-06-09',?,3,{RA})",(tz,))
P('entities.tz 64 chars accepted','OK',c,f"INSERT INTO entities(type,created_at,updated_at,tz) VALUES ('page',{RA},{RA},?)",('x'*64,))
# the point of the column: the same UTC instant reads as different local times
c.execute("DELETE FROM measurements") if False else None
K('a tz-less writer still works (NULL = unknown)', c.execute("select count(*) from entities where tz is null").fetchone()[0]==1)
# §6.1 example from the doc text runs and records tz
i = doc.index('### 6.1 Capture a memo'); blk = re.search(r'```sql\n(.*?)\n```', doc[i:], re.S).group(1)
c = fresh(); c.executescript(blk.replace('BEGIN;','BEGIN;',1))
K('§6.1 memo stored with its tz', c.execute("select tz from entities where type='page'").fetchone()[0]=='Europe/Berlin')

# ---- R4-05: tasks.completed_day
c = fresh(); t = lambda: ent(c,'task')
P('done without completed_day','ERR',c,f"INSERT INTO tasks(id,title,status,completed_at) VALUES (?,'x','done',{RA})",(t(),))
P('done without completed_at','ERR',c,"INSERT INTO tasks(id,title,status,completed_day) VALUES (?,'x','done','2026-06-09')",(t(),))
P('open with completed_day','ERR',c,"INSERT INTO tasks(id,title,status,completed_day) VALUES (?,'x','open','2026-06-09')",(t(),))
P('malformed completed_day','ERR',c,f"INSERT INTO tasks(id,title,status,completed_at,completed_day) VALUES (?,'x','done',{RA},'2026-6-9')",(t(),))
P('impossible completed_day','ERR',c,f"INSERT INTO tasks(id,title,status,completed_at,completed_day) VALUES (?,'x','done',{RA},'2026-02-30')",(t(),))
a = t(); P('done with both','OK',c,f"INSERT INTO tasks(id,title,status,completed_at,completed_day) VALUES (?,'ship it','done','2026-06-09T22:30:00.000Z','2026-06-10')",(a,))
b = t(); c.execute("INSERT INTO tasks(id,title,due_day) VALUES (?,'open one','2026-06-01')",(b,))
P('complete an open task (the documented UPDATE)','OK',c,f"UPDATE tasks SET status='done', completed_at={RA}, completed_day='2026-06-11' WHERE id=?",(b,))
P('un-complete: status open but day left set','ERR',c,"UPDATE tasks SET status='open' WHERE id=?",(b,))
i = doc.index('### 6.2 The day view'); DV = re.search(r'```sql\n(.*?)\n```', doc[i:], re.S).group(1)
rows = c.execute(DV, dict(day='2026-06-10')).fetchall()
K('§6.2 day view lists the task done on LOCAL 2026-06-10 (UTC instant is 06-09 22:30)', ('done','2026-06-09T22:30:00.000Z','ship it') in rows)
K('§6.2 day view does not list it on 2026-06-09', ('done','2026-06-09T22:30:00.000Z','ship it') not in c.execute(DV, dict(day='2026-06-09')).fetchall())
K('§6.2 day view still runs with every branch', len(c.execute(DV, dict(day='2026-06-11')).fetchall())>=1)

# ---- R4-11 b,d: one home for an event's place, no undated lives-in
c = fresh(); pe=ent(c,'person'); c.execute("INSERT INTO people(id,name) VALUES (?, 'P')",(pe,)); pl=ent(c,'place'); c.execute("INSERT INTO places(id,name) VALUES (?, 'Berlin')",(pl,))
ev=ent(c,'event'); c.execute("INSERT INTO events(id,title,start_day,end_day,place_id) VALUES (?, 'Living in Berlin','2015-03-01','2018-08-31',?)",(ev,pl))
L = lambda f,t,k: (f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,?,{RA})",(f,t,k))
P('lives-in is no longer a kind','ERR',c,*L(pe,pl,'lives-in')); P('visited person->place ok','OK',c,*L(pe,pl,'visited'))
P('visited event->place rejected (place_id is the one home)','ERR',c,*L(ev,pl,'visited')); P('visited place->person rejected','ERR',c,*L(pl,pe,'visited'))
K('seeded kinds no longer include lives-in', c.execute("select count(*) from link_kinds where kind='lives-in'").fetchone()[0]==0)
Q = "SELECT pl.name FROM events ev JOIN entities e ON e.id=ev.id AND e.deleted_at IS NULL JOIN places pl ON pl.id=ev.place_id WHERE ev.start_day <= :day AND coalesce(ev.end_day, ev.start_day) >= :day"
K("'where did I live on 2015-06-01' is answered by a dated event", c.execute(Q, dict(day='2015-06-01')).fetchall()==[('Berlin',)])
K("…and is empty after it ended", c.execute(Q, dict(day='2019-01-01')).fetchall()==[])
# ---- instants: %f is the fixed-width millisecond form; CURRENT_TIMESTAMP is not
c = fresh()
K("strftime('%f') renders SS.SSS (3 digits, rounded)", c.execute("select strftime('%f','2026-06-09 21:14:03.482999')").fetchone()[0]=='03.483')
K("whole-second input is widened to .000Z (one fixed width)", c.execute("select strftime('%Y-%m-%dT%H:%M:%fZ','2026-06-09T21:14:03Z')").fetchone()[0]=='2026-06-09T21:14:03.000Z')
K("CURRENT_TIMESTAMP is second-precision and non-ISO", re.fullmatch(r'\d{4}-\d\d-\d\d \d\d:\d\d:\d\d', c.execute("select CURRENT_TIMESTAMP").fetchone()[0]) is not None)
K("same-second instants order by their fraction as plain text", c.execute("select '2026-06-09T21:14:03.482Z' < '2026-06-09T21:14:03.483Z'").fetchone()[0]==1)

print(f'round-6 probes: {sum(res)}/{len(res)} met expectations')
