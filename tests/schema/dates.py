"""Time (SCHEMA.md §2.1, D10): instants, local days and their round-trip CHECKs, why `IS` and not `=`, the zone, and
created_at written by the app."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('dates')
c = fresh()

# ---- instants
for bad in ('2026-06-09 10:00:00.000', '2026-06-09T10:00:00Z', '2026-06-09T10:00:00.000', '2026-06-09T10:00:00.000+02:00', '2026-06-09T25:00:00.000Z', ''):
    S.K(f'instant {bad!r} rejected on entities.created_at', tryx(c, "INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page',?,'2026-06-09T10:00:00.000Z','ui')", (bad,)).startswith('ERR'))
S.K('a well-formed instant is accepted', tryx(c, "INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page','2026-06-09T10:00:00.000Z','2026-06-09T10:00:00.000Z','ui')") == 'OK')
S.K('created_at has no default and is NOT NULL (app-written)', [(r[3], r[4]) for r in c.execute("pragma table_info('entities')") if r[1] == 'created_at'] == [(1, None)])
S.K("strftime('%f') renders SS.SSS (three digits, rounded)", one(c, "select strftime('%f','2026-06-09 21:14:03.482999')") == '03.483')
S.K('whole-second input is widened to .000Z: one fixed width', one(c, "select strftime('%Y-%m-%dT%H:%M:%fZ','2026-06-09T21:14:03Z')") == '2026-06-09T21:14:03.000Z')
S.K('CURRENT_TIMESTAMP is second-precision and non-ISO', re.fullmatch(r'\d{4}-\d\d-\d\d \d\d:\d\d:\d\d', one(c, 'select CURRENT_TIMESTAMP')) is not None)
S.K('same-second instants order by their fraction as plain text', one(c, "select '2026-06-09T21:14:03.482Z' < '2026-06-09T21:14:03.483Z'") == 1)

# ---- days, on every day column
import itertools; _t = itertools.count()
def _page():
    i = ent(c, 'page'); n = next(_t); return dict(id=i, title=f'Day test {n}', title_key=f'day test {n}')
DAYCOLS = [('tasks', 'due_day', lambda: dict(id=ent(c, 'task'), name='x')), ('pages', 'day', _page),
           ('measurements', 'day', lambda: dict(metric_id=1, value=1, source='ui', created_at='2026-01-01T00:00:00.000Z')),
           ('people', 'birth_day', None), ('holdings', 'opened_day', None), ('balances', 'day', None)]
h = named(c, 'holding')
for table, col, base in DAYCOLS:
    for bad in ('2026-9-3', '2026-02-31', 'banana', '2026-13-01', '20260903', '2026-09-03T00:00'):
        if table == 'people': r = tryx(c, 'UPDATE people SET birth_day=? WHERE id=?', (bad, named(c, 'person')))
        elif table == 'holdings': r = tryx(c, 'UPDATE holdings SET opened_day=? WHERE id=?', (bad, h))
        elif table == 'balances': r = balance(c, h, bad, 1)
        else:
            cols = dict(base(), **{col: bad}); r = tryx(c, f"INSERT INTO {table}({','.join(cols)}) VALUES ({','.join('?' * len(cols))})", tuple(cols.values()))
        S.K(f'{table}.{col} rejects {bad!r}', r.startswith('ERR'), r)
# the IS in the round-trip: with = the same CHECK accepts garbage
t = sqlite3.connect(':memory:')
t.execute("CREATE TABLE eq (d TEXT CHECK (date(d) = d)) STRICT"); t.execute("CREATE TABLE isx (d TEXT CHECK (date(d) IS d)) STRICT")
S.K("date('2026-9-3') is NULL", t.execute("select date('2026-9-3') is null").fetchone()[0] == 1)
S.K('a CHECK with = accepts 2026-9-3 (NULL passes a CHECK)', tryx(t, "INSERT INTO eq VALUES ('2026-9-3')") == 'OK')
S.K('the same CHECK with IS rejects it', tryx(t, "INSERT INTO isx VALUES ('2026-9-3')").startswith('ERR'))
S.K('every day and instant CHECK in §3 uses IS, never =', not re.search(r"(?:date|strftime)\([^)]*\)\s*=\s*\w", code(DDL)), re.findall(r"(?:date|strftime)\([^)]*\)\s*=\s*\w+", code(DDL)))

# ---- zone
for tz in ['Europe/Berlin', 'UTC', 'America/Argentina/Buenos_Aires', 'Etc/GMT+5', 'America/Port-au-Prince', None, 'x' * 64]:
    S.K(f'entities.tz {str(tz)[:20]!r} accepted', tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,tz,source) VALUES ('page',{NOW},{NOW},?,'ui')", (tz,)) == 'OK')
    S.K(f'measurements.tz {str(tz)[:20]!r} accepted', measure(c, 1, '2026-06-09', 3, taken_at='2026-06-09T22:30:00.000Z', tz=tz) == 'OK')
for tz in ['', 'Europe Berlin', 'x' * 65, 'Europe/Berlin\n', 'ünï/x', 'a;b', 'Europe/Berlin ']:
    S.K(f'entities.tz {tz[:14]!r} rejected', tryx(c, f"INSERT INTO entities(entity_type,created_at,updated_at,tz,source) VALUES ('page',{NOW},{NOW},?,'ui')", (tz,)).startswith('ERR'))
    S.K(f'measurements.tz {tz[:14]!r} rejected', measure(c, 1, '2026-06-09', 3, tz=tz).startswith('ERR'))

# ---- §6.1 records the zone
c = fresh(); P = {}
run_block(c, block('6.1'), P)
S.K('§6.1 run literally stores the day page with its zone', one(c, "select tz from entities where id=?", (P.get('page_id'),)) == 'Europe/Berlin')
S.done()
