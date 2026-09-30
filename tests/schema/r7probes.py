import sqlite3, sys, re, os, tempfile, threading, time, subprocess
DDL = open(sys.argv[1], encoding='utf-8').read()
doc = open(os.environ.get('DOC') or os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', 'SCHEMA.md'), encoding='utf-8').read()
res=[]
def K(label, cond, detail=''):
    res.append(bool(cond))
    if not cond: print('  FAIL', label, detail)
RA = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
def mkdb():
    d = tempfile.mkdtemp(); path = os.path.join(d,'life.db')
    c = sqlite3.connect(path, isolation_level=None); c.executescript(DDL); c.close(); return path
def conn(path, timeout_ms=5000):
    c = sqlite3.connect(path, isolation_level=None); c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA recursive_triggers=ON'); c.execute(f'PRAGMA busy_timeout={timeout_ms}'); return c
def blk(head):
    s = doc.index(head); return re.search(r'```sql\n(.*?)\n```', doc[s:], re.S).group(1)
def create_page(c, title, key):
    c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES ('page',{RA},{RA})")
    c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES (last_insert_rowid(),'page',?,?)",(title,key))
RESOLVE = "SELECT p.id FROM pages p WHERE p.title_key = ?"

# ---- A. R4-13: the resolve-then-create race (§6.14) with real concurrent connections
path = mkdb(); A, B = conn(path), conn(path)
A.execute('BEGIN'); K('A1 deferred: resolve finds nothing', A.execute(RESOLVE,('diet',)).fetchall()==[])
B.execute('BEGIN IMMEDIATE'); create_page(B,'Diet','diet'); B.execute('COMMIT')             # another writer wins the race
try: create_page(A,'Diet','diet'); A.execute('COMMIT'); outcome='OK'
except sqlite3.Error as e: outcome=str(e); A.execute('ROLLBACK')
K('A1 deferred BEGIN + read + rival commit: the write FAILS at once', 'locked' in outcome, outcome)
K('A1 one page only', conn(path).execute("select count(*) from pages where title_key='diet'").fetchone()[0]==1)
path = mkdb(); out = {}
def writer(name, delay, hold):
    c = conn(path); time.sleep(delay); t0=time.time()
    try:
        c.execute('BEGIN IMMEDIATE'); waited=time.time()-t0
        found = c.execute(RESOLVE,('diet',)).fetchall()
        if not found:
            time.sleep(hold); create_page(c,'Diet','diet')
        c.execute('COMMIT'); out[name]=('found' if found else 'created', round(waited,2))
    except sqlite3.Error as e: out[name]=('ERROR',str(e)); c.execute('ROLLBACK')
ta = threading.Thread(target=writer,args=('A',0.0,0.4)); tb = threading.Thread(target=writer,args=('B',0.1,0.0))
ta.start(); tb.start(); ta.join(); tb.join()
K('A2 IMMEDIATE: first writer creates, second waits then FINDS it (no error)', out.get('A',('',))[0]=='created' and out.get('B',('',))[0]=='found', out)
K('A2 the second writer really waited for the lock (>0.25 s)', out.get('B',('',0))[1] > 0.25, out)
K('A2 exactly one page', conn(path).execute("select count(*) from pages where title_key='diet'").fetchone()[0]==1)
# the doc's §6.14 block itself: BEGIN IMMEDIATE first, resolve inside the transaction, COMMIT last
b614 = blk('### 6.14'); K('A3 §6.14 opens with BEGIN IMMEDIATE', b614.lstrip().startswith('BEGIN IMMEDIATE'))
try: inside = b614.index('BEGIN IMMEDIATE') < b614.index('SELECT p.id') < b614.index('INSERT INTO entities') < b614.index('COMMIT')
except ValueError: inside = False
K('A3 §6.14 resolves INSIDE the transaction', inside)
path = mkdb(); c = conn(path)
for st in [s for s in re.split(r';\s*\n', b614) if re.sub(r'--.*','',s).strip()]:
    if ':page_id' in st or ':target' in st or ':found_id' in st: continue      # link sync / revive: covered by tests/wikilinks
    c.execute(st, {k:v for k,v in dict(key='cafe',title='Cafe').items() if ':'+k in st})
K('A3 §6.14 block runs and creates the page', c.execute("select count(*) from pages where title_key='cafe'").fetchone()[0]==1)
# every write transaction in §6 is IMMEDIATE
sec6 = doc[doc.index('## 6. Query cookbook'):doc.index('## 7. Explicit non-goals')]
bare = re.findall(r'^BEGIN;?\s*$', sec6, re.M); imm = re.findall(r'^BEGIN IMMEDIATE;', sec6, re.M)
K('A4 no bare BEGIN in §6', not bare, bare); K('A4 §6 has the IMMEDIATE transactions (6.1, 6.9, 6.14, 6.15)', len(imm)>=4, len(imm))

# ---- B. pragmas
fresh = sqlite3.connect(mkdb()); K('B1 SQLite default synchronous is FULL (2), so FULL only records intent', fresh.execute('PRAGMA synchronous').fetchone()[0]==2)
s29 = doc[doc.index('### 2.9 Connection setup'):doc.index('### 2.10 Money')]
K('B2 §2.9 sets synchronous = FULL', re.search(r'PRAGMA synchronous\s*=\s*FULL', s29) is not None); K('B2 §2.9 no longer says NORMAL', 'synchronous  = NORMAL' not in s29 and 'standard WAL pairing' not in s29)
K('B2 §2.9 states the BEGIN IMMEDIATE rule', 'Every write transaction starts with `BEGIN IMMEDIATE`' in s29)
hdr = DDL[:DDL.index('PRAGMA application_id')]
K('B3 DDL header lists synchronous=FULL and BEGIN IMMEDIATE', 'PRAGMA synchronous = FULL' in hdr and 'BEGIN IMMEDIATE' in hdr)

# ---- C. read-only readers never write and never block the writer
path = mkdb(); w = conn(path); w.execute('BEGIN IMMEDIATE'); w.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES ('page',{RA},{RA})")
ro = sqlite3.connect(f'file:{path}?mode=ro', uri=True, isolation_level=None)
K('C1 mode=ro reader is not blocked by an open write transaction', ro.execute('select count(*) from entities').fetchone()[0]==0)
w.execute('COMMIT'); K('C1 …and sees the commit', ro.execute('select count(*) from entities').fetchone()[0]==1)
for stmt in ("INSERT INTO lifelog_meta VALUES ('x','y')","DELETE FROM entities","UPDATE entities SET deleted_at=NULL","DROP TABLE lifelog_meta"):
    try: ro.execute(stmt); K('C1 mode=ro refuses: '+stmt[:30], False)
    except sqlite3.Error as e: K('C1 mode=ro refuses: '+stmt[:30], 'readonly' in str(e))
t=time.time(); w.execute("INSERT INTO lifelog_meta VALUES ('k','v')"); K('C1 the writer is not slowed by a connected reader (<0.5 s)', time.time()-t < 0.5)
r = subprocess.run(['sqlite3','-readonly',path,"INSERT INTO lifelog_meta VALUES ('z','z')"],capture_output=True,text=True); K('C2 sqlite3 -readonly refuses writes', 'readonly' in (r.stderr+r.stdout))

# ---- D. the inconsistencies are gone from the live text (§1–§7); decision text keeps history via pointers
live = doc[:doc.index('## 8. Validation records')]
K('D1 no "mirrors 100%" claim left in §1–§7', 'mirrors 100%' not in live)
K('D2 §2.1 lists life.db alone (round 12): no export/, backups/ or dump/ in the layout or the DDL header', all(x not in live.replace('export/interop', '') for x in ('export/', 'backups/', 'dump/')))
K('D3 principle 5 says life.db is irreplaceable and names only the FTS index and title_key as derived', 'and `title_key` can be dropped and rebuilt' in live.replace('\n   ',' ') and '`life.db` is irreplaceable' in live)
K('D4 §2.3 no longer says "Every table uses"', 'Every table uses `INTEGER PRIMARY KEY`' not in live)
c = sqlite3.connect(':memory:'); c.executescript(DDL)
nk = sorted(t for (t,) in c.execute("select name from sqlite_schema where type='table' and name not like 'pages_fts%' and name not like 'sqlite_%'") if not any(r[5]==1 and r[2]=='INTEGER' and c.execute(f"select count(*) from pragma_table_info('{t}') where pk>0").fetchone()[0]==1 for r in c.execute(f"pragma table_info('{t}')")))
K('D4 the tables without a single INTEGER primary key are exactly the four the text names', nk==sorted(['currencies','fx_rates','link_kinds','lifelog_meta']), nk)
K('D5 readers paragraph no longer lists sqlite-web as a reader', 'Readers (Datasette, sqlite-web' not in live)
K('D6 D14/D17 carry the sqlite-web pointer', 'dropped — it is a second writer' in live and 'Datasette listens on localhost only' in live)
# D7: the documented advice works
path = mkdb(); c = conn(path)
def task(title): 
    c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES ('task',{RA},{RA})"); i=c.execute('select last_insert_rowid()').fetchone()[0]
    c.execute("INSERT INTO tasks(id,title,due_day,repeat) VALUES (?,?, '2026-01-01','monthly')",(i,title)); return i
t1, t2 = task('pay rent'), task('pay rent')
try: c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'spawned',{RA})",(t2,t1)); K('D7 spawned task->task stays rejected', False)
except sqlite3.Error: K('D7 spawned task->task stays rejected', True)
c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'related',{RA})",(t2,t1)); K('D7 next occurrence linked with symmetric related (2 edges)', c.execute("select count(*) from links where kind='related'").fetchone()[0]==2)
c.execute("INSERT INTO metrics(name,unit) VALUES ('rent_paid','')"); mid = c.execute("select id from metrics where name='rent_paid'").fetchone()[0]
for day,v in (('2026-01-31',1),('2026-02-28',0),('2026-03-31',1)): c.execute(f"INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (?,?,?,{RA})",(mid,day,v))
K('D7 per-occurrence tracking via a 0/1 habit metric works', c.execute("select group_concat(value) from (select value from measurement_values where metric_id=? order by day)",(mid,)).fetchone()[0]=='1.0,0.0,1.0')
# D8: created_at is app-written, updated_at trigger-maintained (D12 addendum)
K('D8 entities.created_at has no default and is NOT NULL (app-written)', [ (r[3], r[4]) for r in c.execute("pragma table_info('entities')") if r[1]=='created_at'][0]==(1,None))
try: c.execute("INSERT INTO entities(type,updated_at) VALUES ('page','2026-06-09T10:00:00.000Z')"); K('D8 insert without created_at rejected', False)
except sqlite3.Error: K('D8 insert without created_at rejected', True)
print(f'round-7 probes: {sum(res)}/{len(res)} met expectations')
