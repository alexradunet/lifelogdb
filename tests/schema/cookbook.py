"""Every SQL block of the cookbook (docs/cookbook/) prepares and runs on a seeded database, statement by statement with its ids carried
by RETURNING — on a normal connection and on a hardened one (SQLITE_DBCONFIG_DEFENSIVE + trusted_schema=OFF, contract/connections.md)."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('cookbook')
BL = docsql.cookbook_blocks()
S.K('the cookbook has at least 16 SQL blocks, one or more per recipe, 16 recipes', len(BL) >= 16 and len(blocks()) == 16 and list(blocks()) == docsql.cookbook_order(), sorted(blocks()))

def seeded(hardened):
    c = fresh(hardened=hardened)
    dp = day_page(c, '2026-09-28', 'Shipped the schema with [[Sam]] in [[Japan]]. [[Lifelog]]'); wp = page(c, 'Lifelog'); link(c, dp, wp, 'wikilink')
    pe = named(c, 'person', 'Sam', name='Sam')
    gh = page(c, 'Ana')
    pl = named(c, 'place', 'Japan'); link(c, dp, pe, 'wikilink'); link(c, dp, pl, 'wikilink'); link(c, dp, pl, 'at')
    c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); c.execute("INSERT INTO metrics(name,unit) VALUES ('vitamin_d','')")
    measure(c, 2, '2026-09-29', 71.2); measure(c, 2, '2026-09-28', 70.9)
    P = dict(found_id=wp, target_id=wp, target_ids='[]', place_id=pl, day_page_id=dp, mistaken_row_id=2, from_day='2026-01-15', to_day='2026-09-10',
             day='2026-09-29', page_id=wp, person_id=pe, entity_id=pe, handle_title='Bob Sample', handle_key='bob sample', ghost_id=gh,
             due_day='2026-10-05', query='schema', key='newpage', title='Newpage', metric_id=2, wrong_row_id=1, source='ui',
             import_key='notes/sourdough.md', metric='vitamin_d')
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
    S.K(f'{tag}: all {nprep} statements of the cookbook prepare', nfail == 0)
    for h, sql in BL:                                   # and every block runs, in document order, on one database
        try: run_block(c, sql, P); r = 'OK'
        except sqlite3.Error as e:
            r = 'ERR ' + str(e)
            try: c.execute('ROLLBACK')
            except sqlite3.Error: pass
        S.K(f'{tag}: {h} runs', r == 'OK', r)
    S.K(f'{tag}: the database is clean after the whole cookbook', integrity_ok(c))
S.done()
