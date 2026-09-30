"""Synthetic lifelog generator on the v1.9 DDL (§3 extracted from the doc). 1 'year' ~ 1,095 memos, 50,000 measurements,
300 events, 200 tasks, 120 balances, ~3,000 links. Deterministic."""
import sqlite3, random, datetime as dt, sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
WORDS = ("today morning coffee walk work meeting plan review health sleep dinner friend family trip train book read idea note "
         "project schedule weather rain sun run gym weight mood tired happy calm busy call email doctor visit garden cook market").split()
def sentence(r, n): return ' '.join(r.choice(WORDS) for _ in range(n)).capitalize() + '.'
def iso(d, h=12, m=0): return f'{d.isoformat()}T{h:02d}:{m:02d}:00.000Z'
def populate(path, years=1, ddl=None, seed=1):
    ddl = ddl or os.environ['DDL']
    r = random.Random(seed)
    c = sqlite3.connect(path, isolation_level=None)
    c.executescript(open(ddl).read())
    c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA synchronous=OFF')
    c.execute('BEGIN')
    d0 = dt.date(2026 - years, 10, 1); nid = 1
    def ent(typ, created, tz='Europe/Berlin'):
        nonlocal nid
        c.execute('INSERT INTO entities(id,type,created_at,updated_at,tz) VALUES(?,?,?,?,?)', (nid, typ, created, created, tz)); nid += 1; return nid - 1
    metrics = {n: i for n, i in c.execute('select name,id from metrics')}
    for n, u in [('weight', 'kg'), ('sleep', 'h'), ('steps', 'count'), ('heart_rate', 'bpm'), ('temp', 'C')]:
        c.execute('INSERT INTO metrics(name,unit) VALUES(?,?)', (n, u)); metrics[n] = c.execute('select last_insert_rowid()').fetchone()[0]
    people = [ent('person', iso(d0)) for _ in range(40)]
    for i, p in enumerate(people): c.execute('INSERT INTO people(id,name,notes) VALUES(?,?,?)', (p, f'Person {i}', sentence(r, 12)))
    places = [ent('place', iso(d0)) for _ in range(25)]
    for i, p in enumerate(places): c.execute('INSERT INTO places(id,name) VALUES(?,?)', (p, f'Place {i}'))
    accts = []
    for i in range(10):
        a = ent('account', iso(d0)); accts.append(a)
        c.execute("INSERT INTO accounts(id,name,side,currency,category,institution,opened_day) VALUES(?,?,?,?,?,?,?)",
                  (a, f'Account {i}', 'liability' if i == 9 else 'asset', 'EUR', 'cash', f'Bank {i}', d0.isoformat()))
    ndays = years * 365; wiki = []
    for day in range(ndays):
        d = d0 + dt.timedelta(days=day)
        for k in range(3):
            body = ' '.join(sentence(r, r.randint(6, 18)) for _ in range(r.randint(2, 6)))
            if wiki and r.random() < .5: body += f' [[{r.choice(wiki)}]]'
            p = ent('page', iso(d, 8 + k * 4, r.randint(0, 59)))
            c.execute("INSERT INTO pages(id,kind,day,body) VALUES(?, 'memo', ?, ?)", (p, d.isoformat(), body))
        if day % 12 == 0:
            t = f'Topic {day}'; p = ent('page', iso(d)); wiki.append(t)
            c.execute("INSERT INTO pages(id,kind,title,title_key,day,body) VALUES(?, 'page', ?, ?, ?, ?)", (p, t, t.lower(), d.isoformat(), sentence(r, 40)))
        if day % 4 == 0:
            e = ent('event', iso(d)); c.execute("INSERT INTO events(id,title,start_day,place_id,notes) VALUES(?,?,?,?,?)", (e, sentence(r, 3), d.isoformat(), r.choice(places), sentence(r, 10)))
        if day % 6 == 0:
            t = ent('task', iso(d)); c.execute("INSERT INTO tasks(id,title,due_day) VALUES(?,?,?)", (t, sentence(r, 4), (d + dt.timedelta(days=7)).isoformat()))
        if day % 30 == 0:
            for a in accts: c.execute("INSERT INTO balances(account_id,day,amount,recorded_at,source) VALUES(?,?,?,?,'statement')", (a, d.isoformat(), r.randint(0, 5_000_000), iso(d, 20)))
        rows = []
        for n, m in metrics.items():
            k = 1 if n == 'mood' else (120 if n == 'heart_rate' else (48 if n == 'temp' else 1))
            for j in range(k):
                rows.append((m, d.isoformat(), iso(d, j * 24 // k % 24, j % 60), 'Europe/Berlin', round(r.uniform(1, 100), 2), iso(d, 23), 'device'))
        c.executemany("INSERT INTO measurements(metric_id,day,taken_at,tz,value,recorded_at,source) VALUES(?,?,?,?,?,?,?)", rows)
    pids = [i for (i,) in c.execute("select id from pages where kind='memo' limit 3000")]
    for i in range(0, len(pids) - 1, 1):
        c.execute("INSERT OR IGNORE INTO links(from_id,to_id,kind,created_at) VALUES(?,?, 'related', ?)", (pids[i], r.choice(people), iso(d0))) if False else None
    for pid in pids[:3000]:
        c.execute("INSERT INTO links(from_id,to_id,kind,created_at) VALUES(?,?, 'about', ?) ON CONFLICT DO NOTHING", (pid, r.choice(people), iso(d0)))
    c.execute('COMMIT'); c.execute('PRAGMA synchronous=FULL')
    return c
if __name__ == '__main__':
    import time; t = time.time(); c = populate(sys.argv[1], int(sys.argv[2]))
    print({t_: c.execute(f'select count(*) from {t_}').fetchone()[0] for t_ in ['entities', 'pages', 'measurements', 'events', 'tasks', 'balances', 'links']}, f'{time.time()-t:.1f}s', os.path.getsize(sys.argv[1]) // 1024, 'KiB')
