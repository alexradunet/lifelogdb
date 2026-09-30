import sqlite3, sys, unicodedata
exec(open('probes1.py').read().split('# ---- P1:')[0])
res=[]
def T(label, expect, c, sql, args=()):
    r = tryx(c, sql, args); ok = r[0]==expect; res.append(ok)
    if not ok: print('  FAIL', label, '->', r)
def key(t): return unicodedata.normalize('NFC', unicodedata.normalize('NFC', t).casefold())
def page(c, kind, title=None, day='2026-06-09', body='', k=None):
    i = ent(c,'page'); k = k if k is not None else (key(title) if title else None)
    return i, ("INSERT INTO pages(id,kind,title,title_key,day,body) VALUES (?,?,?,?,?,?)", (i,kind,title,k,day,body))
c = fresh()
# --- pages / titles
i,(s,a) = page(c,'page','Café notes'); T('page ok','OK',c,s,a)
for t in ['CAFÉ NOTES','Cafe\u0301 notes','café NOTES']:
    i,(s,a)=page(c,'page',t); T('unicode-dup '+repr(t),'ERR',c,s,a)
i,(s,a)=page(c,'page','Straße'); T('Straße ok','OK',c,s,a)
i,(s,a)=page(c,'page','STRASSE'); T('STRASSE dup','ERR',c,s,a)
i,(s,a)=page(c,'page','Diet'); T('Diet ok','OK',c,s,a)
i,(s,a)=page(c,'page','DIET'); T('DIET dup (two pages)','ERR',c,s,a)
i,(s,a)=page(c,'page','Cafe notes'); T('no-accent distinct','OK',c,s,a)
i,(s,a)=page(c,'memo'); T('memo ok','OK',c,s,a)
i,(s,a)=page(c,'memo','titled'); T('titled memo','ERR',c,s,a)
i,(s,a)=page(c,'memo',None,day=None); T('memo w/o day','ERR',c,s,a)
i,(s,a)=page(c,'page','No day',day=None); T('page w/o day ok (round 11: a page may have no day)','OK',c,s,a)
i,(s,a)=page(c,'page','Second no day',day=None); T('another page w/o day ok','OK',c,s,a)
i,(s,a)=page(c,'page','x/y'); T('slash','ERR',c,s,a)
for ch in ['\\',':','*','?','"','<','>','|','\x01','\t','\n','\x1f','\x7f']:
    i,(s,a)=page(c,'page','a'+ch+'b',k='ab'); T('char '+repr(ch),'ERR',c,s,a)
for t in ['.hidden','..','trail.','CON','con','Nul','prn','AUX','COM1','lpt9',' pad','pad ']:
    i,(s,a)=page(c,'page',t,k=t.lower().strip() or 'x'); T('reserved/unsafe '+repr(t),'ERR',c,s,a)
for t in ['CONSOLE','com10','LPT0','Lifelog v1.2','日本語 ノート','a'*240]:
    i,(s,a)=page(c,'page',t); T('ok title '+repr(t[:12]),'OK',c,s,a)
i,(s,a)=page(c,'page','é'*121); T('242 bytes','ERR',c,s,a)
i,(s,a)=page(c,'page','Diet2',k='dyet2'); T('ascii wrong key','ERR',c,s,a)
i,(s,a)=page(c,'page','Diet3',k='Diet3'); T('key with capital','ERR',c,s,a)
i,(s,a)=page(c,'page','Diet4',k=' diet4'); T('key with space','ERR',c,s,a)
i = ent(c,'page'); T('page w/o key','ERR',c,"INSERT INTO pages(id,kind,title,title_key,day) VALUES (?,'page','Diet5',NULL,NULL)",(i,))
i,(s,a)=page(c,'page','Über',k='über'); T('non-ascii plausible key','OK',c,s,a)
# immutability
pid = c.execute("select id from pages where title='Diet'").fetchone()[0]
T('title update','ERR',c,"UPDATE pages SET title='Diet x' WHERE id=?",(pid,)); T('no-op title ok','OK',c,"UPDATE pages SET title=title, body='b' WHERE id=?",(pid,))
T('kind change','ERR',c,"UPDATE pages SET kind='memo' WHERE id=?",(pid,)); T('no-op kind ok','OK',c,"UPDATE pages SET kind=kind WHERE id=?",(pid,))
# --- dates / instants
T('bad instant space','ERR',c,"INSERT INTO entities(type,created_at,updated_at) VALUES ('page','2026-06-09 10:00:00.000','2026-06-09T10:00:00.000Z')")
T('bad instant no ms','ERR',c,"INSERT INTO entities(type,created_at,updated_at) VALUES ('page','2026-06-09T10:00:00Z','2026-06-09T10:00:00.000Z')")
for d in ['2026-9-3','2026-02-31','banana','2026-13-01']:
    e = ent(c,'event'); T('event bad day '+d,'ERR',c,"INSERT INTO events(id,title,start_day) VALUES (?, 'x', ?)",(e,d))
# --- events/tasks CHECK matrix
def ev(**kw):
    e = ent(c,'event'); cols = dict(id=e,title='t',start_day='2026-06-01',**kw); return ("INSERT INTO events(%s) VALUES (%s)"%(','.join(cols),','.join('?'*len(cols))), tuple(cols.values()))
T('end<start','ERR',c,*ev(end_day='2026-05-31')); T('end_at<start_at','ERR',c,*ev(start_at='2026-06-01T10:00:00.000Z',end_at='2026-06-01T09:00:00.000Z'))
T('every w/o repeat','ERR',c,*ev(repeat_every=2)); T('until w/o repeat','ERR',c,*ev(repeat_until='2026-07-01')); T('every 0','ERR',c,*ev(repeat='daily',repeat_every=0))
T('until<start','ERR',c,*ev(repeat='daily',repeat_until='2026-05-01')); T('weekly w/o weekdays','ERR',c,*ev(repeat='weekly'))
T('daily with weekdays','ERR',c,*ev(repeat='daily',repeat_weekdays='mo'))
for w in ['Mon,Wed','friday','xyz','mo we fr','mo,,fr',',mo','mo,','mo,mo,zz','MO']: T('weekdays '+w,'ERR',c,*ev(repeat='weekly',repeat_weekdays=w))
T('weekdays mo,we,fr ok','OK',c,*ev(repeat='weekly',repeat_weekdays='mo,we,fr'))
T('last friday ok','OK',c,*ev(repeat='monthly',repeat_position='last',repeat_weekday='fr'))
T('position w/o weekday','ERR',c,*ev(repeat='monthly',repeat_position='last')); T('position on weekly','ERR',c,*ev(repeat='weekly',repeat_weekdays='mo',repeat_position='last',repeat_weekday='fr'))
T('position fifth','ERR',c,*ev(repeat='monthly',repeat_position='fifth',repeat_weekday='fr'))
t = ent(c,'task'); T('done w/o completed_at','ERR',c,"INSERT INTO tasks(id,title,status) VALUES (?,'x','done')",(t,))
t = ent(c,'task'); T('open with completed_at','ERR',c,f"INSERT INTO tasks(id,title,status,completed_at) VALUES (?,'x','open',{NOW})",(t,))
t = ent(c,'task'); T('recurring task w/o due_day','ERR',c,"INSERT INTO tasks(id,title,repeat) VALUES (?,'x','daily')",(t,))
t = ent(c,'task'); T('status dropped','ERR',c,"INSERT INTO tasks(id,title,status) VALUES (?,'x','dropped')",(t,))
p = ent(c,'person'); T('death<birth','ERR',c,"INSERT INTO people(id,name,birth_day,death_day) VALUES (?, 'x','2000-01-01','1999-01-01')",(p,))
# --- places / metrics
pl = ent(c,'place'); T('place ok','OK',c,"INSERT INTO places(id,name) VALUES (?,'Berlin')",(pl,)); pl2=ent(c,'place'); T('place dup nocase','ERR',c,"INSERT INTO places(id,name) VALUES (?,'berlin')",(pl2,))
T('metric dup nocase','ERR',c,"INSERT INTO metrics(name,unit) VALUES ('MOOD','')"); T('metric ok','OK',c,"INSERT INTO metrics(name,unit) VALUES ('weight','kg')")
# --- measurements: append-only & supersede
T('m ins','OK',c,"INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (2,'2026-06-01',70,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
T('m update value','ERR',c,"UPDATE measurements SET value=1"); T('m update supersedes','ERR',c,"UPDATE measurements SET supersedes_id=NULL"); T('m update entity','ERR',c,"UPDATE measurements SET entity_id=NULL"); T('m delete','ERR',c,"DELETE FROM measurements")
T('m correction','OK',c,"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (2,'2026-06-01',71,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
T('m 2nd correction of same row','ERR',c,"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (2,'2026-06-01',72,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
T('m chain correct correction','OK',c,"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (2,'2026-06-01',73,2,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
T('m cross-metric','ERR',c,"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (1,'2026-06-01',3,3,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
T('m dangling supersedes','ERR',c,"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (2,'2026-06-01',3,999,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
T('m self supersede','ERR',c,"INSERT INTO measurements(id,metric_id,day,value,supersedes_id,recorded_at) VALUES (50,2,'2026-06-01',3,50,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
T('m import ins','OK',c,"INSERT INTO measurements(metric_id,day,value,import_id,recorded_at) VALUES (2,'2026-06-02',70,'i1',strftime('%Y-%m-%dT%H:%M:%fZ','now'))"); T('m import dup ignore','OK',c,"INSERT OR IGNORE INTO measurements(metric_id,day,value,import_id,recorded_at) VALUES (2,'2026-06-02',99,'i1',strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
res.append(c.execute("select group_concat(value) from measurement_values where metric_id=2 and day='2026-06-01'").fetchone()[0]=='73.0')
# --- links
def lk(f,t,k): return (f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,?,{NOW})",(f,t,k))
pa = ent(c,'person'); c.execute("INSERT INTO people(id,name) VALUES (?, 'A')",(pa,)); pb = ent(c,'person'); c.execute("INSERT INTO people(id,name) VALUES (?, 'B')",(pb,))
ea = ent(c,'event'); c.execute("INSERT INTO events(id,title,start_day) VALUES (?, 'E','2026-06-01')",(ea,)); ta=ent(c,'task'); c.execute("INSERT INTO tasks(id,title) VALUES (?, 'T')",(ta,)); tb=ent(c,'task'); c.execute("INSERT INTO tasks(id,title) VALUES (?, 'T2')",(tb,))
m1 = memo(c,'m'); pw = pid
for lbl,exp,f,t,k in [('friend ok','OK',pa,pb,'friend'),('Friend unregistered','ERR',pa,pb,'Friend'),('attended ok','OK',pa,ea,'attended'),('attended event->person','ERR',ea,pa,'attended'),
   ('friend person->event','ERR',pa,ea,'friend'),('subtask ok','OK',ta,tb,'subtask'),('subtask page->page','ERR',m1,pw,'subtask'),('spawned task->page ok','OK',ta,m1,'spawned'),
   ('spawned task->task','ERR',ta,tb,'spawned'),('wikilink page->person','ERR',m1,pa,'wikilink'),('wikilink ok','OK',m1,pw,'wikilink'),('redirect ok','OK',m1,pw,'redirect'),
   ('about memo->person','OK',m1,pa,'about'),('about memo->task','ERR',m1,ta,'about'),('related any','OK',ta,pa,'related'),('lives-in removed (round 6)','ERR',pa,pl,'lives-in'),
   ('visited person->place ok','OK',pa,pl,'visited'),('visited event->place removed (round 6)','ERR',ea,pl,'visited'),('dangling endpoint typed','ERR',9999,pl,'visited')]:
    T('link '+lbl,exp,c,*lk(f,t,k))
res.append(c.execute("select count(*) from links where kind='friend'").fetchone()[0]==2)     # mirrored
T('links immutable','ERR',c,"UPDATE links SET kind='related' WHERE kind='friend'"); 
c.execute("DELETE FROM links WHERE kind='friend' AND from_id=?",(pa,)); res.append(c.execute("select count(*) from links where kind='friend'").fetchone()[0]==0)
T('link_kinds symmetric fixed','ERR',c,"UPDATE link_kinds SET symmetric=1 WHERE kind='about'"); T('link_kinds note editable','OK',c,"UPDATE link_kinds SET note='n' WHERE kind='about'")
T('kind name uppercase','ERR',c,"INSERT INTO link_kinds(kind,symmetric) VALUES ('Boss',0)"); T('sym kind differing types','ERR',c,"INSERT INTO link_kinds(kind,symmetric,from_types,to_types) VALUES ('mentor',1,'person','place')")
T('kind type list malformed','ERR',c,"INSERT INTO link_kinds(kind,symmetric,from_types,to_types) VALUES ('k2',0,'Person','page')")
# --- tombstone touch
u0 = c.execute("select updated_at from entities where id=?",(m1,)).fetchone()[0]; import time; time.sleep(.005)
c.execute(f"UPDATE entities SET deleted_at={NOW} WHERE id=?",(m1,)); r=c.execute("select updated_at,deleted_at from entities where id=?",(m1,)).fetchone(); res.append(r[0]>u0 and r[0]==r[1])
# --- FTS
c.execute("UPDATE pages SET body='needle' WHERE id=?",(pw,)); res.append(len(c.execute("select 1 from pages_fts where pages_fts match 'needle'").fetchall())==1)
c.execute("UPDATE pages SET body='other' WHERE id=?",(pw,)); res.append(len(c.execute("select 1 from pages_fts where pages_fts match 'needle'").fetchall())==0)
T('fts integrity','OK',c,"INSERT INTO pages_fts(pages_fts, rank) VALUES('integrity-check', 1)")
res.append(c.execute('pragma integrity_check').fetchall()==[('ok',)] and c.execute('pragma foreign_key_check').fetchall()==[])
print(f'regression (v1.4 behaviours on the v1.5 DDL): {sum(res)}/{len(res)}')
