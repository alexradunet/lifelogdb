import re, sqlite3, random, datetime as dt, calendar
exec(open('probes1.py').read().split('# ---- P1:')[0])
doc = open(DOCPATH, encoding='utf-8').read()
s = doc.index('### 6.12'); 
EXP = re.search(r'```sql\n(WITH RECURSIVE days.*?)\n```', doc[s:], re.S).group(1)
W = ['mo','tu','we','th','fr','sa','su']  # python weekday(): Mon=0
def mlen(y,m): return calendar.monthrange(y,m)[1]
def nth_weekday(y,m,pos,wd):
    if pos=='last':
        d = dt.date(y,m,mlen(y,m))
        while d.weekday()!=wd: d -= dt.timedelta(1)
        return d
    d = dt.date(y,m,1)
    while d.weekday()!=wd: d += dt.timedelta(1)
    return d + dt.timedelta(7*['first','second','third','fourth'].index(pos))
def monday(d): return d - dt.timedelta(d.weekday())
def occurs(r, d):
    st = r['start']; ev = r['every'] or 1
    if d < st or (r['until'] and d > r['until']): return False
    k = r['repeat']
    if k=='daily': return (d-st).days % ev == 0
    if k=='weekly': return W[d.weekday()] in r['wds'] and ((monday(d)-monday(st)).days//7) % ev == 0
    if k=='monthly':
        if ((d.year*12+d.month)-(st.year*12+st.month)) % ev: return False
        if r['pos']: return d == nth_weekday(d.year,d.month,r['pos'],W.index(r['wd']))
        return d.day == min(st.day, mlen(d.year,d.month))
    if k=='yearly':
        return (d.year-st.year)%ev==0 and d.month==st.month and d.day==min(st.day,mlen(d.year,d.month))
random.seed(20260930)
c = fresh(); rules=[]
for n in range(600):
    k = random.choice(['daily','weekly','monthly','monthly','yearly'])
    st = dt.date(2019,1,1)+dt.timedelta(random.randrange(0,2400))
    r = dict(repeat=k, start=st, every=random.choice([None,1,2,3,4,5,13]) if k!='yearly' else random.choice([None,1,2,4]), wds=None, pos=None, wd=None, until=None,
             dur=random.choice([0,0,0,1,3,6]))
    if k=='weekly': r['wds']=set(random.sample(W, random.randint(1,4)))
    if k=='monthly' and random.random()<.5: r['pos']=random.choice(['first','second','third','fourth','last']); r['wd']=random.choice(W)
    if random.random()<.3: r['until']=st+dt.timedelta(random.randrange(0,1500))
    i = ent(c,'event')
    c.execute("INSERT INTO events(id,title,start_day,end_day,repeat,repeat_every,repeat_weekdays,repeat_position,repeat_weekday,repeat_until) VALUES (?,?,?,?,?,?,?,?,?,?)",
      (i,f'e{n}',st.isoformat(), (st+dt.timedelta(r['dur'])).isoformat() if r['dur'] else None, k, r['every'],
       ','.join(x for x in W if x in r['wds']) if r['wds'] else None, r['pos'], r['wd'], r['until'].isoformat() if r['until'] else None))
    r['id']=i; rules.append(r)
bad=0; total=0
for (a,b) in [('2019-01-01','2020-06-30'),('2024-02-01','2024-03-31'),('2025-12-15','2026-03-15'),('2019-01-01','2031-12-31'),('2028-02-20','2028-03-05')]:
    got = set(c.execute(EXP, dict(start_day=a,end_day=b)).fetchall()) if False else set((x[0],x[2],x[3]) for x in c.execute(EXP, dict(start_day=a,end_day=b)))
    exp=set(); d0=dt.date.fromisoformat(a); d1=dt.date.fromisoformat(b)
    for r in rules:
        d=d0
        while d<=d1:
            if occurs(r,d): exp.add((r['id'], d.isoformat(), (d+dt.timedelta(r['dur'])).isoformat()))
            d+=dt.timedelta(1)
    total+=len(exp)
    diff = got ^ exp
    print(f'window {a}..{b}: sql={len(got)} oracle={len(exp)} mismatches={len(diff)}')
    if diff:
        bad+=len(diff); 
        for x in sorted(diff)[:5]: print('   ', x, 'sql-only' if x in got else 'oracle-only', [ (r['repeat'],r['start'],r['every'],r['wds'],r['pos'],r['wd'],r['until']) for r in rules if r['id']==x[0]][0])
print('TOTAL occurrences compared', total, 'mismatches', bad)
