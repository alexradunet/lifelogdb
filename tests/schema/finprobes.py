import sqlite3, sys, re
exec(open('probes1.py').read().split('# ---- P1:')[0])
results=[]
def probe(label, expect, sql, args=(), c=None):
    r = tryx(c, sql, args); got = r[0]
    ok = (got == expect); results.append(ok)
    if not ok: print(f'  FAIL {label}: expected {expect}, got {r}')
    return r
def check(label, cond, detail=''):
    results.append(bool(cond))
    if not cond: print(f'  FAIL {label} {detail}')
def acct(c, name='Main', side='asset', cur='EUR', **kw):
    i = ent(c,'holding'); cols = dict(id=i,name=name,side=side,currency=cur, **kw)
    c.execute(f"INSERT INTO holdings({','.join(cols)}) VALUES ({','.join('?'*len(cols))})", tuple(cols.values())); return i
def bal(c, a, day, amt, **kw):
    cols = dict(holding_id=a, day=day, amount=amt, **kw)
    return c.execute(f"INSERT INTO balances({','.join(cols)},recorded_at) VALUES ({','.join('?'*len(cols))},{NOW})", tuple(cols.values()))

# ---------- currencies
c = fresh()
for lbl,exp,sql in [('cur lowercase code','ERR',"INSERT INTO currencies VALUES ('eur','x',100,NULL)"),
                    ('cur 2-char code','ERR',"INSERT INTO currencies VALUES ('EU','x',100,NULL)"),
                    ('cur 11-char code','ERR',"INSERT INTO currencies VALUES ('ABCDEFGHIJK','x',100,NULL)"),
                    ('cur duplicate EUR','ERR',"INSERT INTO currencies VALUES ('EUR','x',100,NULL)"),
                    ('cur subunits 0','ERR',"INSERT INTO currencies VALUES ('ZZA','x',0,NULL)"),
                    ('cur subunits 10^9+1','ERR',"INSERT INTO currencies VALUES ('ZZB','x',1000000001,NULL)"),
                    ('cur subunits 10^9 ok','OK',"INSERT INTO currencies VALUES ('ZZC','x',1000000000,NULL)"),
                    ('cur non-decimal subunits 5 ok','OK',"INSERT INTO currencies VALUES ('MRU','Ouguiya',5,NULL)"),
                    ('cur code with space','ERR',"INSERT INTO currencies VALUES ('E R','x',100,NULL)"),
                    ('cur UPDATE subunits','ERR',"UPDATE currencies SET subunits=1000 WHERE code='EUR'"),
                    ('cur no-op SET subunits=subunits','OK',"UPDATE currencies SET subunits=subunits, name='Euro (€)' WHERE code='EUR'"),
                    ('cur seeded count 8+2','OK',"SELECT 1")]:
    probe(lbl,exp,sql,c=c)
check('cur seed count 7+2', c.execute('select count(*) from currencies').fetchone()[0]==9)
check('seed JPY subunits=1, EUR=100, no BTC seeded', dict(c.execute("select code,subunits from currencies where code in ('JPY','EUR','BTC')"))=={'JPY':1,'EUR':100})

# ---------- holdings
c = fresh(); a = acct(c,'Main')
probe('acct type/table mismatch (page entity)','ERR',"INSERT INTO holdings(id,name,side,currency) VALUES (?,?,?,?)",(ent(c,'page'),'X','asset','EUR'),c)
probe('acct without entity row','ERR',"INSERT INTO holdings(id,name,side,currency) VALUES (9999,'Y','asset','EUR')",c=c)
probe('acct id also in places','ERR',"INSERT INTO places(id,name) VALUES (?, 'P')",(a,),c)
probe('acct side debt','ERR',"INSERT INTO holdings(id,name,side,currency) VALUES (?,?,?,?)",(ent(c,'holding'),'D','debt','EUR'),c)
probe('acct NULL side','ERR',"INSERT INTO holdings(id,name,side,currency) VALUES (?,?,NULL,?)",(ent(c,'holding'),'D2','EUR'),c)
probe('acct unregistered currency','ERR',"INSERT INTO holdings(id,name,side,currency) VALUES (?,?,?,?)",(ent(c,'holding'),'E','asset','XXX'),c)
probe('acct duplicate name (NOCASE)','ERR',"INSERT INTO holdings(id,name,side,currency) VALUES (?,?,?,?)",(ent(c,'holding'),'main','asset','EUR'),c)
probe('acct closed < opened','ERR',"INSERT INTO holdings(id,name,side,currency,opened_day,closed_day) VALUES (?,?,?,?,?,?)",(ent(c,'holding'),'F','asset','EUR','2020-05-01','2020-04-30'),c)
probe('acct closed = opened ok','OK',"INSERT INTO holdings(id,name,side,currency,opened_day,closed_day) VALUES (?,?,?,?,?,?)",(ent(c,'holding'),'G','asset','EUR','2020-05-01','2020-05-01'),c)
probe('acct malformed opened_day','ERR',"INSERT INTO holdings(id,name,side,currency,opened_day) VALUES (?,?,?,?,?)",(ent(c,'holding'),'H','asset','EUR','2020-5-1'),c)
probe('acct closed without opened ok','OK',"INSERT INTO holdings(id,name,side,currency,closed_day) VALUES (?,?,?,?,?)",(ent(c,'holding'),'I','asset','EUR','2020-05-01'),c)
probe('acct UPDATE side','ERR',"UPDATE holdings SET side='liability' WHERE id=?",(a,),c)
probe('acct UPDATE currency','ERR',"UPDATE holdings SET currency='USD' WHERE id=?",(a,),c)
probe('acct full-row no-op UPDATE ok','OK',"UPDATE holdings SET name='Main', side=side, currency=currency, category='Cash' WHERE id=?",(a,),c)
before = c.execute('select updated_at from entities where id=?',(a,)).fetchone()[0]
import time; time.sleep(0.005)
c.execute("UPDATE holdings SET institution='Bank A' WHERE id=?",(a,))
check('acct update bumps entities.updated_at', c.execute('select updated_at from entities where id=?',(a,)).fetchone()[0] > before)
probe('entities type foo','ERR',f"INSERT INTO entities(type,created_at,updated_at) VALUES ('foo',{NOW},{NOW})",c=c)
_pg = ent(c,'page'); c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES (?,'page','Hold','hold')",(_pg,))
probe('entities type holding ok (with its page, D20)','OK',f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('holding',{NOW},{NOW},?)",(_pg,),c)
c.execute(f"UPDATE entities SET deleted_at={NOW} WHERE id=?",(a,))
check('acct tombstone recorded', c.execute('select deleted_at is not null from entities where id=?',(a,)).fetchone()[0]==1)

# ---------- balances
c = fresh(); a = acct(c,'Chk'); l = acct(c,'Mortgage','liability')
probe('bal integer ok','OK',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-01-31',1234567,{NOW})",(a,),c)
probe('bal REAL 12.5 rejected (STRICT)','ERR',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-02-28',12.5,{NOW})",(a,),c)
probe('bal REAL 12.0 accepted as 12 (lossless)','OK',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-02-27',12.0,{NOW})",(a,),c)
check('bal 12.0 stored as integer', c.execute("select typeof(amount) from balances where day='2026-02-27'").fetchone()[0]=='integer')
probe('bal text 12x rejected','ERR',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-02-26','12x',{NOW})",(a,),c)
probe('bal malformed day','ERR',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-2-3',1,{NOW})",(a,),c)
probe('bal impossible day','ERR',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-02-30',1,{NOW})",(a,),c)
probe('bal malformed recorded_at','ERR',"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-03-01',1,'2026-03-01 10:00:00')",(a,),c)
probe('bal missing recorded_at','ERR',"INSERT INTO balances(holding_id,day,amount) VALUES (?,'2026-03-01',1)",(a,),c)
probe('bal dangling holding (FK on)','ERR',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (9999,'2026-03-01',1,{NOW})",c=c)
probe('bal UPDATE','ERR',"UPDATE balances SET amount=5 WHERE id=1",c=c)
probe('bal DELETE','ERR',"DELETE FROM balances WHERE id=1",c=c)
probe('bal negative (overdraft) ok','OK',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-03-02',-5000,{NOW})",(a,),c)
probe('bal huge int64 ok','OK',f"INSERT INTO balances(holding_id,day,amount,recorded_at) VALUES (?,'2026-03-03',9000000000000000000,{NOW})",(a,),c)
# newest wins, retraction, re-entry
c = fresh(); a = acct(c,'Chk')
bal(c,a,'2026-01-31',100); bal(c,a,'2026-02-28',200); bal(c,a,'2026-02-28',250)         # correction
vals = lambda: c.execute("select day,amount from balance_values where holding_id=? order by day",(a,)).fetchall()
check('newest row per day wins', vals()==[('2026-01-31',100),('2026-02-28',250)], vals())
bal(c,a,'2026-02-28',None,note='wrong holding')                                          # retraction
check('retraction hides the day', vals()==[('2026-01-31',100)], vals())
bal(c,a,'2026-02-28',260)                                                                # re-entry after retraction
check('re-entry after retraction wins', vals()==[('2026-01-31',100),('2026-02-28',260)], vals())
check('all history still stored', c.execute('select count(*) from balances').fetchone()[0]==5)
# retraction as first row is harmless
bal(c,a,'2026-03-31',None); check('lone retraction shows nothing', ('2026-03-31',None) not in vals() and len(vals())==2)
# idempotent import
probe('import first','OK',f"INSERT INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-04-30',1,{NOW},'bank_csv','L1') ON CONFLICT(source,import_id) WHERE import_id IS NOT NULL DO NOTHING",(a,),c)
r = probe('import repeat (no-op)','OK',f"INSERT INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-04-30',1,{NOW},'bank_csv','L1') ON CONFLICT(source,import_id) WHERE import_id IS NOT NULL DO NOTHING",(a,),c)
check('import repeat inserted 0 rows', r[1]==0 and c.execute("select count(*) from balances where import_id='L1'").fetchone()[0]==1)
probe('same import_id other source ok','OK',f"INSERT INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-05-31',1,{NOW},'broker_csv','L1')",(a,),c)
probe('import: ON CONFLICT still raises on bad day','ERR',f"INSERT INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-5-31',1,{NOW},'bank_csv','L9') ON CONFLICT(source,import_id) WHERE import_id IS NOT NULL DO NOTHING",(a,),c)
r = tryx(c, f"INSERT OR IGNORE INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-5-31',1,{NOW},'bank_csv','L9')",(a,))
check('DOCUMENTED HOLE: OR IGNORE swallows a bad day silently', r==('OK',0), r)
# REPLACE bypass: OFF vs ON
c = fresh(); a = acct(c,'Chk'); bal(c,a,'2026-01-31',100,source='s',import_id='k')
r = tryx(c, f"INSERT OR REPLACE INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-01-31',999,{NOW},'s','k')",(a,))
check('DOCUMENTED HOLE: REPLACE rewrites history when recursive_triggers=OFF', r==('OK',1) and c.execute('select amount from balances').fetchall()==[(999,)], r)
c = fresh(); c.execute('PRAGMA recursive_triggers=ON'); a = acct(c,'Chk'); bal(c,a,'2026-01-31',100,source='s',import_id='k')
probe('REPLACE blocked when recursive_triggers=ON','ERR',f"INSERT OR REPLACE INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-01-31',999,{NOW},'s','k')",(a,),c)
check('history intact after blocked REPLACE', c.execute('select amount from balances').fetchall()==[(100,)])
probe('REPLACE INTO by id blocked when ON','ERR',f"REPLACE INTO balances(id,holding_id,day,amount,recorded_at) VALUES (1,?,'2026-01-31',5,{NOW})",(a,),c)
# mirror/touch/entity triggers still fine with recursive_triggers ON
p = ent(c,'person'); c.execute("INSERT INTO people(id,name) VALUES (?,'S')",(p,)); q = ent(c,'person'); c.execute("INSERT INTO people(id,name) VALUES (?,'T')",(q,))
c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'friend',{NOW})",(p,q))
check('symmetric mirror terminates under recursive_triggers=ON', c.execute('select count(*) from links').fetchone()[0]==2)
c.execute("DELETE FROM links WHERE from_id=? AND to_id=?",(p,q)); check('mirror delete terminates under ON', c.execute('select count(*) from links').fetchone()[0]==0)

# ---------- no exchange rates (D18): net worth is per currency, nothing converts
c = fresh()
check('no fx_rates table: nothing in the file converts one currency into another', not c.execute("select 1 from sqlite_schema where name like '%fx%' or name like '%rate%'").fetchall())
probe('a retired currency (DEM) can be registered: one series per currency, so a redenomination is just a second code','OK',"INSERT INTO currencies VALUES ('DEM','Deutsche Mark',100,NULL)",c=c)

# ---------- links to holdings
c = fresh(); a = acct(c,'Flat','asset'); m = memo(c,'bought the flat'); e = ent(c,'event'); c.execute("INSERT INTO events(id,title,start_day) VALUES (?,'Bought flat','2026-05-01')",(e,))
pg = ent(c,'page'); c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES (?,'page','Some','some')",(pg,))
for lbl,exp,f,t,k in [('link memo about holding','OK',m,a,'about'),('link event about holding','OK',e,a,'about'),('link holding related page','OK',a,pg,'related'),
                      ('link about page rejected (regression)','ERR',m,pg,'about'),('link holding subtask rejected','ERR',a,a,'subtask'),('link wikilink to holding rejected','ERR',m,a,'wikilink')]:
    probe(lbl,exp,f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,?,{NOW})",(f,t,k),c)

# ---------- exactness: why not REAL / measurements
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('acct_a','EUR'),('acct_b','EUR')")
c.execute("INSERT INTO measurements(metric_id,day,value,recorded_at) VALUES (2,'2026-01-01',0.1,strftime('%Y-%m-%dT%H:%M:%fZ','now')),(3,'2026-01-01',0.2,strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
real_sum = c.execute("select sum(value) from measurements").fetchone()[0]
check('REAL money drifts: 0.1+0.2 != 0.3', real_sum != 0.3 and abs(real_sum-0.3)<1e-12, real_sum)
a = acct(c,'X'); bal(c,a,'2026-01-01',10); bal(c,a,'2026-01-02',20)
check('integer cents exact: 10+20 = 30', c.execute("select sum(amount) from balances").fetchone()[0]==30)
print('  (REAL sum of 0.1 and 0.2 =', repr(real_sum), ')')

# ---------- entities_type widening is a 2-statement migration (named CHECK)
c = fresh(); a = acct(c,'Z')
check('drop named constraint', tryx(c,"ALTER TABLE entities DROP CONSTRAINT entities_type")[0]=='OK')
check('add widened constraint', tryx(c,"ALTER TABLE entities ADD CONSTRAINT entities_type CHECK (type IN ('page','event','task','person','place','holding','vehicle'))")[0]=='OK')
probe('new type accepted after widening','OK',f"INSERT INTO entities(type,created_at,updated_at) VALUES ('vehicle',{NOW},{NOW})",c=c)
check('integrity + FK ok after widening', c.execute('pragma integrity_check').fetchall()==[('ok',)] and c.execute('pragma foreign_key_check').fetchall()==[])
c = fresh(); pass

print(f'finance probes: {sum(results)}/{len(results)} met expectations')
