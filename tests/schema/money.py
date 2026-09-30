"""Money (SCHEMA.md §2.7, D18): currencies, holdings, balances, exact integers, retraction, idempotent imports, and
net worth per currency — §6.15 and §6.16 taken from the document and compared with an exact-integer oracle."""
import os, random, sys, datetime as dt
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('money')

# ---- currencies
c = fresh()
for lbl, exp, sql in [('lowercase code', 'ERR', "INSERT INTO currencies VALUES ('eur','x',100,NULL)"), ('2-char code', 'ERR', "INSERT INTO currencies VALUES ('EU','x',100,NULL)"),
                      ('11-char code', 'ERR', "INSERT INTO currencies VALUES ('ABCDEFGHIJK','x',100,NULL)"), ('duplicate EUR', 'ERR', "INSERT INTO currencies VALUES ('EUR','x',100,NULL)"),
                      ('subunits 0', 'ERR', "INSERT INTO currencies VALUES ('ZZA','x',0,NULL)"), ('subunits 10^9+1', 'ERR', "INSERT INTO currencies VALUES ('ZZB','x',1000000001,NULL)"),
                      ('subunits 10^9', 'OK', "INSERT INTO currencies VALUES ('ZZC','x',1000000000,NULL)"), ('a 1/5 subunit (MRU)', 'OK', "INSERT INTO currencies VALUES ('MRU','Ouguiya',5,NULL)"),
                      ('a code with a space', 'ERR', "INSERT INTO currencies VALUES ('E R','x',100,NULL)"), ('UPDATE subunits', 'ERR', "UPDATE currencies SET subunits=1000 WHERE code='EUR'"),
                      ('a no-op SET subunits=subunits', 'OK', "UPDATE currencies SET subunits=subunits, name='Euro (€)' WHERE code='EUR'"),
                      ('a retired currency (DEM): a redenomination is a second code', 'OK', "INSERT INTO currencies VALUES ('DEM','Deutsche Mark',100,NULL)")]:
    S.K(f'currency: {lbl} -> {exp}', tryx(c, sql).startswith(exp))
S.K('seeded: JPY 1, EUR 100, no BTC', dict(c.execute("select code,subunits from currencies where code in ('JPY','EUR','BTC')")) == {'JPY': 1, 'EUR': 100})
for tk in ('V', 'ZM', 'BRK.B', 'VWCE.DE'):
    S.K(f'a ticker ({tk}) is not a currency code (§7: quantity × price is deferred)', tryx(c, "INSERT INTO currencies VALUES (?,'x',100,NULL)", (tk,)).startswith('ERR'))
S.K('no exchange-rate table: nothing in the file converts one currency into another', not c.execute("select 1 from sqlite_schema where name like '%fx%' or name like '%rate%'").fetchall())

# ---- holdings
c = fresh(); a = named(c, 'holding', 'Main')
def hold(side='asset', cur='EUR', **kw): i = ent(c, 'holding'); c.execute("INSERT INTO pages(id,entity_type,kind,title,title_key) VALUES (?, 'holding', 'page', ?, ?)", (i, f'H{i}', f'h{i}')); return i, dict(id=i, side=side, currency=cur, **kw)
def ins(cols): return tryx(c, f"INSERT INTO holdings({','.join(cols)}) VALUES ({','.join('?' * len(cols))})", tuple(cols.values()))
for lbl, exp, kw in [('side debt', 'ERR', dict(side='debt')), ('NULL side', 'ERR', dict(side=None)), ('an unregistered currency', 'ERR', dict(cur='XXX')),
                     ('closed before opened', 'ERR', dict(opened_day='2020-05-01', closed_day='2020-04-30')), ('closed = opened', 'OK', dict(opened_day='2020-05-01', closed_day='2020-05-01')),
                     ('closed without opened', 'OK', dict(closed_day='2020-05-01'))]:
    _, cols = hold(**kw); S.K(f'holding: {lbl} -> {exp}', ins(cols).startswith(exp))
S.K('UPDATE side refused', tryx(c, "UPDATE holdings SET side='liability' WHERE id=?", (a,)).startswith('ERR'))
S.K('UPDATE currency refused', tryx(c, "UPDATE holdings SET currency='USD' WHERE id=?", (a,)).startswith('ERR'))
S.K('a full-row no-op update of side and currency passes', tryx(c, "UPDATE holdings SET side=side, currency=currency, category='Cash' WHERE id=?", (a,)) == 'OK')

# ---- balances
c = fresh(); a = named(c, 'holding', 'Chk')
S.K('an integer amount', balance(c, a, '2026-01-31', 1234567) == 'OK')
S.K('12.5 refused (STRICT INTEGER)', balance(c, a, '2026-02-28', 12.5).startswith('ERR'))
S.K('12.0 accepted and stored as the integer 12 (lossless)', balance(c, a, '2026-02-27', 12.0) == 'OK' and one(c, "select typeof(amount) from balances where day='2026-02-27'") == 'integer')
S.K("'12x' refused", balance(c, a, '2026-02-26', '12x').startswith('ERR'))
S.K('a malformed recorded_at refused', tryx(c, "INSERT INTO balances(holding_id,day,amount,recorded_at,source) VALUES (?,'2026-03-01',1,'2026-03-01 10:00:00','ui')", (a,)).startswith('ERR'))
S.K('a missing recorded_at refused', tryx(c, "INSERT INTO balances(holding_id,day,amount,source) VALUES (?,'2026-03-01',1,'ui')", (a,)).startswith('ERR'))
S.K('a dangling holding refused (FK on)', balance(c, 9999, '2026-03-01', 1).startswith('ERR'))
S.K('UPDATE refused', tryx(c, 'UPDATE balances SET amount=5').startswith('ERR'))
S.K('DELETE refused', tryx(c, 'DELETE FROM balances').startswith('ERR'))
S.K('a negative (overdraft) and a huge int64 amount accepted', balance(c, a, '2026-03-02', -5000) == 'OK' and balance(c, a, '2026-03-03', 9000000000000000000) == 'OK')
c = fresh(); a = named(c, 'holding', 'Chk')
vals = lambda: c.execute('select day, amount from balance_values where holding_id=? order by day', (a,)).fetchall()
balance(c, a, '2026-01-31', 100); balance(c, a, '2026-02-28', 200); balance(c, a, '2026-02-28', 250)
S.K('the newest row per day wins', vals() == [('2026-01-31', 100), ('2026-02-28', 250)], vals())
balance(c, a, '2026-02-28', None, note='wrong holding')
S.K('a NULL amount retracts the day', vals() == [('2026-01-31', 100)])
balance(c, a, '2026-02-28', 260)
S.K('a value after the retraction wins again', vals() == [('2026-01-31', 100), ('2026-02-28', 260)])
S.K('all history is still stored', one(c, 'select count(*) from balances') == 5)
imp = f"INSERT INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,?,1,{NOW},?,?) ON CONFLICT(source,import_id) WHERE import_id IS NOT NULL DO NOTHING"
c.execute(imp, (a, '2026-04-30', 'import:bank_csv', 'L1')); c.execute(imp, (a, '2026-04-30', 'import:bank_csv', 'L1'))
S.K('an import row repeated inserts nothing', one(c, "select count(*) from balances where import_id='L1'") == 1)
S.K('the same import_id from another importer is kept', tryx(c, imp, (a, '2026-05-31', 'import:broker_csv', 'L1')) == 'OK')
S.K('ON CONFLICT still raises on a bad day', tryx(c, imp, (a, '2026-5-31', 'import:bank_csv', 'L9')).startswith('ERR'))
c0 = fresh(rt=False); a0 = named(c0, 'holding'); balance(c0, a0, '2026-01-31', 100, source='import:s', import_id='k')
S.K('with recursive_triggers=OFF, REPLACE rewrites a balance', tryx(c0, f"INSERT OR REPLACE INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-01-31',999,{NOW},'import:s','k')", (a0,)) == 'OK'
    and c0.execute('select amount from balances').fetchall() == [(999,)])
c1 = fresh(); a1 = named(c1, 'holding'); balance(c1, a1, '2026-01-31', 100, source='import:s', import_id='k')
S.K('with recursive_triggers=ON it is refused', tryx(c1, f"INSERT OR REPLACE INTO balances(holding_id,day,amount,recorded_at,source,import_id) VALUES (?,'2026-01-31',999,{NOW},'import:s','k')", (a1,)).startswith('ERR')
    and tryx(c1, f"REPLACE INTO balances(id,holding_id,day,amount,recorded_at,source) VALUES (1,?,'2026-01-31',5,{NOW},'ui')", (a1,)).startswith('ERR'))

# ---- exactness: why not REAL, TEXT or NUMERIC
t = sqlite3.connect(':memory:')
S.K('REAL drifts: 0.1 + 0.2 = 0.30000000000000004', t.execute('select 0.1 + 0.2').fetchone()[0] == 0.30000000000000004)
t.execute('CREATE TABLE d (x TEXT)'); t.executemany('INSERT INTO d VALUES (?)', [('9.5',), ('10.25',)])
S.K("TEXT decimals sort as strings ('10.25' before '9.5')", [r[0] for r in t.execute('select x from d order by x')] == ['10.25', '9.5'])
S.K('...and their sum is a float', t.execute("select sum(x) from (select '0.1' as x union all select '0.2')").fetchone()[0] == 0.30000000000000004)
t.execute('CREATE TABLE n (x NUMERIC)'); t.execute("INSERT INTO n VALUES ('1234567890123456.78')")
S.K('a NUMERIC column stores a long decimal as REAL (1234567890123456.78 -> 1234567890123456.8)', t.execute('select typeof(x), x from n').fetchone() == ('real', 1234567890123456.8))

# ---- links to holdings
c = fresh(); a = named(c, 'holding', 'Flat'); m = memo(c); e = thing(c, 'event')
S.K('a memo is about a holding', link(c, m, a, 'about') == 'OK')
S.K('an event is about a holding', link(c, e, a, 'about') == 'OK')
S.K('a memo wikilinks to a holding (it is a page)', link(c, m, a, 'wikilink') == 'OK')

# ---- §6.14 run literally
c = fresh(); ent(c, 'page'); ent(c, 'page'); P = dict(day='2026-09-29', amount=777, row_key='r1')
try: run_block(c, block('6.14'), P); r = 'OK'
except sqlite3.Error as ex: r = 'ERR ' + str(ex)
S.K('§6.14 run literally: one id for the holding, its page titled by its name, every balance on it', r == 'OK'
    and c.execute('select h.id, p.title from holdings h join pages p using(id)').fetchall() == [(P.get('holding_id'), 'Main checking')]
    and one(c, 'select count(*) from balances where holding_id=?', (P.get('holding_id'),)) == 4, (r, P))

# ---- net worth per currency vs an oracle
random.seed(7)
c = fresh(); c.execute("INSERT INTO currencies(code,name,subunits) VALUES ('MRU','Ouguiya',5), ('BTC','Bitcoin',100000000)")
accts = []
for k in range(14):
    cur = random.choice(['EUR', 'USD', 'JPY', 'BTC', 'GBP', 'MRU']); side = random.choice(['asset', 'asset', 'asset', 'liability'])
    o = dt.date(2012, 1, 1) + dt.timedelta(random.randrange(0, 900)); cl = o + dt.timedelta(random.randrange(400, 4000)) if random.random() < .35 else None
    i = named(c, 'holding', f'acct{k}', side=side, currency=cur, opened_day=o.isoformat(), closed_day=cl.isoformat() if cl else None)
    accts.append(dict(id=i, side=side, cur=cur, open=o, close=cl))
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (accts[3]['id'],)); accts[3]['dead'] = True
c.execute('BEGIN')
for a in accts:
    d = a['open']; end = a['close'] or dt.date(2024, 12, 31)
    while d <= end:
        if random.random() < .8:
            amt = random.randrange(-5_000_000, 90_000_000); balance(c, a['id'], d.isoformat(), amt); r = random.random()
            if r < .15: balance(c, a['id'], d.isoformat(), amt + random.randrange(1, 5000))
            elif r < .25:
                balance(c, a['id'], d.isoformat(), None)
                if random.random() < .5: balance(c, a['id'], d.isoformat(), amt + 7)
        d += dt.timedelta(random.choice([30, 31, 45, 90]))
c.execute('COMMIT')
def oracle(day):
    out, d = {}, dt.date.fromisoformat(day)
    for a in accts:
        if a.get('dead') or a['open'] > d or (a['close'] and a['close'] < d): continue
        eff = {}
        for bd, amt in c.execute('select day, amount from balances where holding_id=? order by id', (a['id'],)): eff[bd] = amt
        days = sorted(k for k, v in eff.items() if v is not None and k <= day)
        if not days: continue
        tot, n = out.get(a['cur'], (0, 0)); out[a['cur']] = (tot + (1 if a['side'] == 'asset' else -1) * eff[days[-1]], n + 1)
    return out
q616, q617 = block('6.15'), block('6.16')
rows = c.execute(q617, dict(from_day='2012-01-15', to_day='2024-12-20')).fetchall()
series = {}
for day, cur, net, n in rows: series.setdefault(day, {})[cur] = (net, n)
bad = [d for d in series if series[d] != oracle(d)]
S.K('§6.16 over 13 years of month-ends in 6 currencies equals the oracle, exact integers per currency', len(series) >= 150 and len({r[1] for r in rows}) >= 5 and not bad, (len(series), bad[:3]))
wrapped = 'SELECT currency, sum(net_minor), count(*) FROM (' + q616.strip().rstrip(';') + ') GROUP BY currency'
bad = [d for d in ('2015-06-30', '2016-02-29', '2019-12-31', '2021-07-04', '2024-12-31') if {cu: (n, k) for cu, n, k in c.execute(wrapped, dict(day=d))} != oracle(d)]
S.K('§6.15 wrapped as the text says equals the oracle on five days, month-ends or not', not bad, bad)
S.K('§6.15 names each holding by its page title', {r[0] for r in c.execute(q616, dict(day='2019-12-31'))} <= {f'acct{k}' for k in range(14)})
plan = ' | '.join(r[3] for r in c.execute('EXPLAIN QUERY PLAN ' + q616, dict(day='2019-12-31')))
S.K('§6.15: the CROSS JOIN seeks each holding\'s balance by index (no scan of balances)', 'SCAN b' not in plan and 'balances_series' in plan, plan)
stale = c.execute(block('6.17'), dict(day='2024-12-31')).fetchall()
S.K('§6.17 lists holdings with no balance in the last 35 days, by title', stale and all(r[0].startswith('acct') for r in stale), stale[:3])
S.done()
