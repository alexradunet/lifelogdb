import re, sqlite3, sys, time, random, datetime as dt, calendar
exec(open('probes1.py').read().split('# ---- P1:')[0])
doc = open(DOCPATH, encoding='utf-8').read()
s = doc.index('## 6. Query cookbook'); e = doc.index('## 7. Explicit non-goals')
blocks = re.findall(r'```sql\n(.*?)\n```', doc[s:e], re.S)
print('cookbook sql blocks:', len(blocks))

if os.environ.get('HARDENED'):          # §2.9: DEFENSIVE + trusted_schema=OFF must leave every block working (round 15)
    def fresh(path=':memory:'):
        c = sqlite3.connect(path, isolation_level=None)
        c.setconfig(sqlite3.SQLITE_DBCONFIG_DEFENSIVE, True); c.execute('PRAGMA trusted_schema = OFF')
        c.executescript(DDL); c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA recursive_triggers=ON')
        return c
    print('hardened connection: SQLITE_DBCONFIG_DEFENSIVE + trusted_schema=OFF')
c = fresh()
# seed
me = memo(c, 'Shipped the schema. [[Lifelog]]', '2026-09-29')
wp = ent(c,'page'); c.execute("INSERT INTO pages(id,kind,title,title_key,day) VALUES (?,'page','Lifelog','lifelog',NULL)",(wp,))
pe = ent(c,'person'); c.execute("INSERT INTO people(id,name) VALUES (?,'Sam')",(pe,))
ev = ent(c,'event'); c.execute("INSERT INTO events(id,title,start_day) VALUES (?,'Trip','2026-09-29')",(ev,))
ta = ent(c,'task'); c.execute("INSERT INTO tasks(id,title,due_day) VALUES (?,'do','2026-09-28')",(ta,))
c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES ({pe},{ev},'attended',{NOW})")
c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES ({me},{wp},'wikilink',{NOW})")
c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')")
c.execute("INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (2,'2026-09-29',71.2,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
c.execute("INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (2,'2026-09-28',70.9,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
ac = ent(c,'holding'); c.execute("INSERT INTO holdings(id,name,side,currency,opened_day) VALUES (?,'Seed','asset','EUR','2019-01-01')",(ac,))
for d_,v_ in (('2026-05-31',100000),('2026-08-31',120000)): c.execute(f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,?,?,{NOW})",(ac,d_,v_))
usd = ent(c,'holding'); c.execute("INSERT INTO holdings(id,name,side,currency,opened_day) VALUES (?,'Brokerage','asset','USD','2019-01-01')",(usd,))
c.execute(f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-06-30',5000000,{NOW})",(usd,))
P = dict(found_id=wp, target_id=wp, target_ids='[]', place_id=1, mistaken_row_id=2, holding_id=ac, row_key='r1', amount=777, from_day='2026-01-15', to_day='2026-09-10', day='2026-09-29', page_id=wp, person_id=pe, entity_id=pe, handle_title='Bob Sample', handle_key='bob sample', memo_id=me, task_id=ta, due_day='2026-10-05', query='schema',
         key='newpage', title='Newpage', start_day='2026-09-01', end_day='2026-10-31', metric_id=2, wrong_row_id=1, source='ui')
fails = 0
for i,b in enumerate(blocks,1):
    stmts=[]; cur=''
    for line in b.split('\n'):
        cur += line+'\n'
        if sqlite3.complete_statement(cur): stmts.append(cur); cur=''
    stmts = [x for x in stmts if re.sub(r'--.*','',x).strip()]
    nrows = None; ok = True
    for st in stmts:
        if 'SELECT id FROM metrics' in st or True:
            try:
                cur = c.execute(st, {k:v for k,v in P.items() if ':'+k in st})
                rows = cur.fetchall() if cur.description else None
                if rows is not None: nrows = len(rows)
                m = re.search(r'RETURNING id;[^\n]*?:(\w+)', st)          # the id the app keeps (round 15)
                if m and rows: P[m.group(1)] = rows[0][0]
            except sqlite3.Error as ex:
                ok=False; print(f'  block {i} FAILED: {ex}\n    {st[:120]!r}')
    fails += (not ok)
    print(f'  block {i:>2}: {"ok" if ok else "FAIL"}  stmts={len(stmts)} rows={nrows}')
print('cookbook failures:', fails)

# ---- perf: measurement_values at 120k rows
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('w','kg')")
c.execute('BEGIN')
c.executemany("INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (2,?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now'))", [((dt.date(2000,1,1)+dt.timedelta(days=i%9000)).isoformat(), i*0.001) for i in range(120000)])
c.execute('COMMIT')
t=time.time(); n=c.execute("select count(*) from measurement_values").fetchone()[0]; print('perf: measurement_values count', n, f'{time.time()-t:.3f}s')
print('  plan:', c.execute("explain query plan select count(*) from measurement_values").fetchall()[-1][3])
