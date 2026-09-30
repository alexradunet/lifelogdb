"""Every SQL block of SCHEMA.md §6 prepares and runs on a seeded database, statement by statement with its ids carried
by RETURNING — on a normal connection and on a hardened one (SQLITE_DBCONFIG_DEFENSIVE + trusted_schema=OFF, §2.6)."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('cookbook')
BL = docsql.cookbook_blocks(DOC)
S.K('§6 has at least 21 SQL blocks, one per section from 6.1 to 6.21', len(BL) >= 21 and len(blocks()) == 21 and all(f'6.{i}' in blocks() for i in range(1, 22)), sorted(blocks()))

def seeded(hardened):
    c = fresh(hardened=hardened)
    me = memo(c, 'Shipped the schema. [[Lifelog]]', '2026-09-29'); wp = page(c, 'Lifelog'); link(c, me, wp, 'wikilink')
    pe = named(c, 'person', 'Sam', name='Sam'); ev = thing(c, 'event', title='Trip', start_day='2026-09-29'); ta = thing(c, 'task', title='do', due_day='2026-09-28')
    link(c, pe, ev, 'attended'); gh = page(c, 'Ana'); wo = page(c, 'Workout')
    pl = named(c, 'place', 'Japan', lat=35.68, lon=139.69)
    c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')")
    measure(c, 2, '2026-09-29', 71.2); measure(c, 2, '2026-09-28', 70.9)
    ac = named(c, 'holding', 'Seed', opened_day='2019-01-01'); balance(c, ac, '2026-05-31', 100000); balance(c, ac, '2026-08-31', 120000)
    us = named(c, 'holding', 'Brokerage', currency='USD', opened_day='2019-01-01'); balance(c, us, '2026-06-30', 5000000)
    P = dict(found_id=wp, target_id=wp, target_ids='[]', place_id=pl, mistaken_row_id=2, holding_id=ac, row_key='r1', amount=777, from_day='2026-01-15', to_day='2026-09-10',
             day='2026-09-29', page_id=wp, person_id=pe, entity_id=pe, handle_title='Bob Sample', handle_key='bob sample', ghost_id=gh, memo_id=me, task_id=ta,
             due_day='2026-10-05', query='schema', key='newpage', title='Newpage', metric_id=2, wrong_row_id=1, source='ui',
             taken_at='2026-09-29T03:00:00.000Z', at='2026-09-29T12:00:00.000Z', lat=35.681, lon=139.691, max_accuracy_m=100, m_per_deg_lon=90300.0, radius_m=500,
             event_id=ev, kind_id=wo)
    return c, P

for hardened in (False, True):
    tag = 'hardened' if hardened else 'plain'
    c, P = seeded(hardened); nprep = nfail = 0
    for h, sql in BL:                                   # every statement prepares against the DDL
        for st in statements(sql):
            if code(st).split()[0].upper() in ('BEGIN', 'COMMIT', 'SAVEPOINT', 'RELEASE', 'ROLLBACK'): continue
            nprep += 1
            try: c.execute('EXPLAIN ' + st, {k: None for k in re.findall(r':(\w+)', code(st))})
            except sqlite3.Error as e: nfail += 1; print(f'   {tag} prepare failed: {h}: {e}')
    S.K(f'{tag}: all {nprep} statements of §6 prepare', nfail == 0)
    for h, sql in BL:                                   # and every block runs, in document order, on one database
        try: run_block(c, sql, P); r = 'OK'
        except sqlite3.Error as e:
            r = 'ERR ' + str(e)
            try: c.execute('ROLLBACK')
            except sqlite3.Error: pass
        S.K(f'{tag}: §{h.split()[0]} runs', r == 'OK', r)
    S.K(f'{tag}: the database is clean after all of §6', integrity_ok(c))
S.done()
