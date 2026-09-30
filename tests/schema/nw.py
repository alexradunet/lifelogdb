import sqlite3, random, datetime as dt, time, sys
exec(open('probes1.py').read().split('# ---- P1:')[0])
random.seed(7)
def gen(n_acc=14, years=12):
    c = fresh()
    c.execute("INSERT INTO currencies(code,name,subunits) VALUES ('MRU','Ouguiya',5)")   # non-decimal subunit on purpose
    c.execute("INSERT INTO currencies(code,name,subunits) VALUES ('BTC','Bitcoin',100000000)")   # user-registered: not seeded
    ccys = ['EUR','USD','JPY','BTC','GBP','MRU']   # amounts of different currencies are never added
    accts=[]
    for k in range(n_acc):
        i = ent(c,'account'); cur = random.choice(ccys); side = random.choice(['asset','asset','asset','liability'])
        o = dt.date(2012,1,1)+dt.timedelta(random.randrange(0,900)); cl = o+dt.timedelta(random.randrange(400,4000)) if random.random()<.35 else None
        c.execute("INSERT INTO accounts(id,name,side,currency,opened_day,closed_day) VALUES (?,?,?,?,?,?)",(i,f'acct{k}',side,cur,o.isoformat(), cl.isoformat() if cl else None))
        accts.append(dict(id=i,side=side,cur=cur,open=o,close=cl,name=f'acct{k}'))
    # tombstone one account
    dead = accts[3]['id']; c.execute(f"UPDATE entities SET deleted_at={NOW} WHERE id=?", (dead,)); accts[3]['dead']=True
    # balances with corrections & retractions
    for a in accts:
        d = a['open']; end = a['close'] or dt.date(2024,12,31)
        while d <= end:
            if random.random()<.8:
                amt = random.randrange(-5_000_000, 90_000_000) if a['cur']!='BTC' else random.randrange(0, 900_000_000)
                c.execute(f"INSERT INTO balances(account_id,day,amount,recorded_at) VALUES (?,?,?,{NOW})",(a['id'],d.isoformat(),amt))
                r = random.random()
                if r<.15:  # correction
                    c.execute(f"INSERT INTO balances(account_id,day,amount,recorded_at) VALUES (?,?,?,{NOW})",(a['id'],d.isoformat(),amt+random.randrange(1,5000)))
                elif r<.25: # retraction
                    c.execute(f"INSERT INTO balances(account_id,day,amount,recorded_at) VALUES (?,?,NULL,{NOW})",(a['id'],d.isoformat()))
                    if random.random()<.5: c.execute(f"INSERT INTO balances(account_id,day,amount,recorded_at) VALUES (?,?,?,{NOW})",(a['id'],d.isoformat(),amt+7))
            d += dt.timedelta(random.choice([30,31,45,90]))
    return c, accts

def oracle(c, accts, day):
    """{currency: (signed net in that currency's minor units, accounts counted)} — exact integers, straight from the rules of 6.16."""
    out = {}
    d = dt.date.fromisoformat(day)
    for a in accts:
        if a.get('dead'): continue
        if a['open']>d or (a['close'] and a['close']<d): continue
        # effective balances = newest row per day, unless NULL
        eff = {}
        for (bd, amt) in c.execute("select day, amount from balances where account_id=? order by id", (a['id'],)): eff[bd]=amt
        days = sorted(k for k,v in eff.items() if v is not None and k<=day)
        if not days: continue
        tot, n = out.get(a['cur'], (0, 0))
        out[a['cur']] = (tot + (1 if a['side']=='asset' else -1)*eff[days[-1]], n+1)
    return out

c, accts = gen()
month_ends = []
d = dt.date(2012,1,31)
while d < dt.date(2024,12,31):
    month_ends.append(d.isoformat()); d = (d.replace(day=1)+dt.timedelta(32)).replace(day=1); d = (d.replace(day=28)+dt.timedelta(4)).replace(day=1)-dt.timedelta(1)
month_ends.append('2024-12-31')
t=time.time()
rows = c.execute(open('q617.sql').read(), dict(from_day='2012-01-15', to_day='2024-12-20')).fetchall()
el=time.time()-t
print(f'series rows={len(rows)} ({el:.3f}s)  first={rows[0]} last={rows[-1]}')
series = {}
for (day, cur, net, n) in rows: series.setdefault(day, {})[cur] = (net, n)
compared = mismatches = 0
for m in month_ends:
    if m not in series: continue
    compared += 1
    if series[m] != oracle(c, accts, m): mismatches += 1; print('MISMATCH', m, series[m], oracle(c, accts, m))
print(f'month-ends compared={compared} real-mismatches={mismatches}   (every figure an exact integer, per currency)')
assert compared >= 150 and len({r[1] for r in rows}) >= 5, (compared, {r[1] for r in rows})   # the fixture really is multi-currency and long
# as-of query: its rows summed per currency (as 6.16 says to wrap it) vs the oracle, on days that are not month-ends too
q = open('q616.sql').read()
wrapped = 'SELECT currency, sum(net_minor), count(*) FROM (' + q.strip().rstrip(';') + ') GROUP BY currency'
bad = 0
for day in ['2015-06-30','2016-02-29','2019-12-31','2021-07-04','2024-12-31']:
    got = {cur: (net, n) for cur, net, n in c.execute(wrapped, dict(day=day))}
    want = oracle(c, accts, day)
    if got != want: bad += 1; print('MISMATCH breakdown', day, got, want)
    per_acct = c.execute(q, dict(day=day)).fetchall()
    print(day, 'per-account rows', len(per_acct), 'currencies', sorted(got), 'series', series.get(day, {}) == got if day in series else '-')
print(f'as-of days compared=5 real-mismatches={bad}')
assert bad == 0 and mismatches == 0
print('plan check:'); 
for r in c.execute('explain query plan '+q, dict(day='2019-12-31')): print('  ', r[3])
