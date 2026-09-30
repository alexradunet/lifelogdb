import sqlite3, random, datetime as dt, time, sys
from fractions import Fraction
exec(open('probes1.py').read().split('# ---- P1:')[0])
Q616 = open('q616.sql').read() if len(sys.argv)<2 else None
Q617 = open('q617.sql').read() if len(sys.argv)<2 else None
random.seed(7)
def gen(n_acc=14, years=12, base='EUR'):
    c = fresh()
    c.execute("INSERT INTO currencies(code,name,subunits) VALUES ('MRU','Ouguiya',5)")   # non-decimal subunit on purpose
    c.execute("INSERT INTO currencies(code,name,subunits) VALUES ('BTC','Bitcoin',100000000)")   # user-registered: not seeded
    ccys = ['EUR','USD','JPY','BTC','GBP','MRU']
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
    # fx: canonical pairs vs EUR; leave GBP with NO rate at all, and MRU only from 2016
    fx=[]
    for cur,rate0 in (('USD',1.1),('JPY',130.0),('BTC',30000.0),('MRU',40.0)):
        d = dt.date(2011,12,1) if cur!='MRU' else dt.date(2016,1,1)
        while d <= dt.date(2024,12,31):
            rate = rate0*random.uniform(.7,1.4)
            a,b = sorted([cur,base]); 
            # stored as (from<to): 1 from = rate' to
            if a==cur: stored=(cur,base,1.0/rate)      # rate = units of cur per EUR  => 1 cur = 1/rate EUR
            else:      stored=(base,cur,rate)
            c.execute("INSERT INTO fx_rates(from_ccy,to_ccy,day,rate) VALUES (?,?,?,?)",(*stored[:2],d.isoformat(),stored[2])); fx.append((cur,d,rate))
            d += dt.timedelta(random.choice([10,20,35]))
    return c, accts

def oracle(c, accts, day, base='EUR'):
    subunits = dict(c.execute("select code,subunits from currencies"))
    tot = Fraction(0); n=0; unconv=0; rows={}
    d = dt.date.fromisoformat(day)
    for a in accts:
        if a.get('dead'): continue
        if a['open']>d or (a['close'] and a['close']<d): continue
        # effective balances = newest row per day, unless NULL
        eff = {}
        for (bd, amt) in c.execute("select day, amount from balances where account_id=? order by id", (a['id'],)): eff[bd]=amt
        days = sorted(k for k,v in eff.items() if v is not None and k<=day)
        if not days: continue
        amt = eff[days[-1]]; n+=1
        if a['cur']==base: rate = Fraction(1)
        else:
            lo,hi = sorted([a['cur'],base])
            row = c.execute("select rate from fx_rates where from_ccy=? and to_ccy=? and day<=? order by day desc limit 1",(lo,hi,day)).fetchone()
            if not row: unconv+=1; continue
            rate = Fraction(row[0]) if a['cur']==lo else 1/Fraction(row[0])
        val = (1 if a['side']=='asset' else -1)*Fraction(amt)*rate*Fraction(subunits[base])/Fraction(subunits[a['cur']])
        tot += val
    return tot, n, unconv

c, accts = gen()
month_ends = []
d = dt.date(2012,1,31)
while d < dt.date(2024,12,31):
    month_ends.append(d.isoformat()); d = (d.replace(day=1)+dt.timedelta(32)).replace(day=1); d = (d.replace(day=28)+dt.timedelta(4)).replace(day=1)-dt.timedelta(1)
month_ends.append('2024-12-31')
t=time.time()
rows = c.execute(open('q617.sql').read(), dict(from_day='2012-01-15', to_day='2024-12-20', base='EUR')).fetchall()
el=time.time()-t
print(f'series rows={len(rows)} ({el:.3f}s)  first={rows[0]} last={rows[-1]}')
bad=exact=0; worst=0; cnt_mismatch=0
byday = {r[0]:r for r in rows}
for m in month_ends:
    if m not in byday: continue
    tot,n,unc = oracle(c,accts,m)
    r = byday[m]
    # SQL: accounts counted = joined valued accounts (incl unconverted); oracle n counts valued incl unconverted
    diff = abs(Fraction(r[1] or 0) - tot) if r[1] is not None else abs(tot)
    worst = max(worst, float(diff))
    if diff <= Fraction(1,2)+Fraction(1,1000): exact+=1
    elif diff <= 1: bad+=1
    else: cnt_mismatch+=1; print('MISMATCH', m, r, float(tot), n, unc)
    if (r[2], r[3]) != (n, unc): cnt_mismatch+=1; print('COUNT MISMATCH', m, r, n, unc)
print(f'month-ends compared={len(byday)} within-rounding={exact} off-by-<=1={bad} real-mismatches={cnt_mismatch} worst_abs_diff={worst:.4f}')
print('unconverted months (GBP has no rate):', sum(1 for r in rows if r[3]>0), 'of', len(rows))
# as-of breakdown query vs series total for a random day
q = open('q616.sql').read()
for day in ['2015-06-30','2019-12-31','2024-12-31']:
    br = c.execute(q, dict(day=day, base='EUR')).fetchall()
    tot = sum(x[6] for x in br if x[6] is not None)
    print(day, 'breakdown rows', len(br), 'sum', tot, 'series', byday.get(day, ('-',))[1], 'oracle', round(float(oracle(c,accts,day)[0])))
print('plan check:'); 
for r in c.execute('explain query plan '+q, dict(day='2019-12-31', base='EUR')): print('  ', r[3])
