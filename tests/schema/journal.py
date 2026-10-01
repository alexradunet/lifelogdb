"""The journal (SCHEMA.md D5, D15, D16, D22, D23): the day page and its CHECK, capture that appends to it (§6.1), the §6.2
day view, the days that name someone or somewhere (§6.3), where the owner was (§6.9), and what stands in for recurrence,
events and tasks."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
import wikisave as W
S = Suite('journal')

# ---- the day page (pages_day_page)
c = fresh()
def add(title, day):
    c.execute('SAVEPOINT a')
    try:
        i = ent(c, 'page'); c.execute('INSERT INTO pages(id,title,title_key,day) VALUES (?,?,?,?)', (i, title, title_key(title), day))
        c.execute('RELEASE a'); return i
    except sqlite3.Error:
        c.execute('ROLLBACK TO a'); c.execute('RELEASE a'); return None
S.K('a page titled with a day, whose day is its title, is accepted', add('2026-09-29', '2026-09-29') is not None)
S.K('a page titled with a day but no day is refused (pages_day_page)', add('2026-09-28', None) is None)
S.K('...and one whose day is another day', add('2026-09-27', '2026-09-26') is None)
S.K('a second page for the same day is refused (the title is unique)', add('2026-09-29', '2026-09-29') is None)
S.K('a title that only looks like a day is an ordinary page (2026-02-30, 2026-9-3)', add('2026-02-30', None) is not None and add('2026-9-3', None) is not None)
S.K('an ordinary page may have a day of its own', add('Trip report', '2026-09-29') is not None)
cols = lambda t: [r[1] for r in c.execute(f"pragma table_info('{t}')")]
S.K('pages have no kind and no inbox column (D5)', 'kind' not in cols('pages') and 'triaged_at' not in cols('pages'))
S.K('there is no events table (D22)', one(c, "select count(*) from sqlite_schema where name='events'") == 0)

# ---- §6.1 capture: append to the day page, create it on the first write
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')")
P = {}
def sync(st):                                                    # the §6.13 link sync, inside §6.1's transaction
    if st.upper().startswith('UPDATE PAGES SET BODY'):
        W.sync_wikilinks(c, P['page_id'], one(c, 'select body from pages where id=?', (P['page_id'],)))
try: run_block(c, block('6.1'), P, sync); r = 'OK'
except sqlite3.Error as e: r = 'ERR ' + str(e)
row = c.execute('select title, day, body from pages where id=?', (P.get('page_id'),)).fetchone()
S.K('§6.1 run literally on a new day creates the day page with the entry', r == 'OK' and row == ('2026-09-29', '2026-09-29', 'Shipped the schema doc. Review pending. [[Lifelog]]'), (r, row))
S.K('...links it to what the entry names, and attaches the mood to it', one(c, "select count(*) from links l join pages p on p.id=l.to_id where l.from_id=? and p.title='Lifelog'", (P.get('page_id'),)) == 1
    and c.execute('select captured_with_id from measurements').fetchall() == [(P.get('page_id'),)])
sel = [s for s in statements(block('6.1')) if code(s).upper().startswith('SELECT')]
S.K('§6.1 finds the day page by its key, the day itself', sel and c.execute(sel[0]).fetchall() == [(P.get('page_id'), None)])
plan = ' | '.join(r[3] for r in c.execute('EXPLAIN QUERY PLAN ' + sel[0])) if sel else ''
S.K('...a search on pages_title', 'pages_title' in plan, plan)
upd = [s for s in statements(block('6.1')) if code(s).upper().startswith('UPDATE PAGES')]
c.execute(upd[0], {'page_id': P['page_id']})
S.K('a second capture that day appends after a blank line, in the same page', one(c, 'select body from pages where id=?', (P['page_id'],)).count('Review pending.') == 2
    and '[[Lifelog]]\n\nShipped' in one(c, 'select body from pages where id=?', (P['page_id'],)) and one(c, 'select count(*) from pages where day=?', ('2026-09-29',)) == 1)
c = fresh(); a, _ = W.capture(c, 'first', '2026-09-30'); b, _ = W.capture(c, 'met [[Ana]]', '2026-09-30'); d, _ = W.capture(c, 'next day', '2026-10-01')
S.K('capture: one page per day, entries in order', a == b != d and one(c, 'select body from pages where id=?', (a,)) == 'first\n\nmet [[Ana]]')
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (d,)); e, _ = W.capture(c, 'again', '2026-10-01')
S.K('capture on a tombstoned day page revives it, never a second page for the day', e == d and one(c, 'select deleted_at from entities where id=?', (d,)) is None)
gp, _ = W.save_page(c, 'see [[2026-12-02]]')
g = one(c, "select id from pages where title='2026-12-02'")
S.K('a link that names a day before anything was written makes that day\'s page, with its day', c.execute('select day, body from pages where id=?', (g,)).fetchone() == ('2026-12-02', ''))
S.K('...and the first capture of that day writes into it', W.capture(c, 'it came', '2026-12-02')[0] == g and one(c, 'select body from pages where id=?', (g,)) == 'it came')

# ---- §6.2 day view
DV = block('6.2')
c = fresh()
d29 = day_page(c, '2026-09-29', 'a day of work'); d28 = day_page(c, '2026-09-28', 'yesterday')
office, home = named(c, 'place', 'Office'), named(c, 'place', 'Home'); link(c, d29, office, 'at'); link(c, d28, home, 'at')
page(c, 'Essay', day='2026-09-29', body='text'); page(c, 'Link target'); page(c, 'Yesterday essay', day='2026-09-28', body='x')
named(c, 'person', 'Sam')
c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); measure(c, 2, '2026-09-29', 71.2, taken_at='2026-09-29T06:00:00.000Z'); measure(c, 2, '2026-09-29', 70.0, supersedes_id=1)
rows = c.execute(DV, {'day': '2026-09-29'}).fetchall()
got = {(r[0], r[2]) for r in rows}
S.K('§6.2 shows the day page, the page written that day, where I was and the corrected reading',
    {('day page', 'a day of work'), ('page', 'Essay'), ('at', 'Office'), ('weight', '70.0 kg')} <= got, rows)
S.K('...the day page once, as the day page and not again as a page written that day', [r[0] for r in rows].count('day page') == 1 and ('page', '2026-09-29') not in got, rows)
S.K('...not another day\'s page or place, a link target, a person\'s page, a page of another day or the superseded reading',
    not {r[2] for r in rows} & {'yesterday', 'Home', 'Link target', 'Sam', 'Yesterday essay', '71.2 kg'}, rows)
S.K('...undated items first, the day page leading', rows[0][0] == 'day page' and [r[1] is None for r in rows] == sorted([r[1] is None for r in rows], reverse=True))
pid = one(c, "select id from pages where title='Essay'")
c.execute("UPDATE entities SET created_at='2026-09-29T08:00:00.000Z' WHERE id=?", (pid,)); c.execute("UPDATE pages SET body='text 2' WHERE id=?", (pid,))
S.K('an edited page is flagged "(edited)"', ('page (edited)', 'Essay') in {(r[0], r[2]) for r in c.execute(DV, {'day': '2026-09-29'})})

# ---- §6.3 the days that name someone or somewhere
c = fresh(); ana = named(c, 'person', 'Ana', name='Ana'); par = named(c, 'place', 'Lakeside')
d1, _ = W.capture(c, 'with [[Ana]] at [[Lakeside]]', '2026-07-31'); d2, _ = W.capture(c, 'called [[Ana]]', '2026-08-02')
d3, _ = W.capture(c, 'alone', '2026-08-03'); es, _ = W.save_page(c, 'an essay about [[Ana]]', 'Friends')
Q = block('6.3')
S.K('§6.3 lists the day pages that link Ana, newest first, and not an essay that names her', [r[0] for r in c.execute(Q, {'entity_id': ana})] == ['2026-08-02', '2026-07-31'])
d4, _ = W.capture(c, 'cina cu Ana', '2026-08-04'); link(c, d4, ana, 'about'); link(c, d1, ana, 'about')
S.K('...also a day that names her without brackets, by an about link; a day with both is listed once',
    [r[0] for r in c.execute(Q, {'entity_id': ana})] == ['2026-08-04', '2026-08-02', '2026-07-31'])
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (d4,))
S.K('§6.3 lists the days at a place the same way', [r[0] for r in c.execute(Q, {'entity_id': par})] == ['2026-07-31'])
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (d2,))
S.K('...and drops a tombstoned day', [r[0] for r in c.execute(Q, {'entity_id': ana})] == ['2026-07-31'])
plan = ' | '.join(r[3] for r in c.execute('EXPLAIN QUERY PLAN ' + Q, {'entity_id': ana}))
S.K('§6.3 is served by links_to', 'links_to' in plan, plan)

# ---- §6.9 where was I: at links from the day page
c = fresh(); par, cor, spa = named(c, 'place', 'Lakeside'), named(c, 'place', 'Northgate'), named(c, 'place', 'Southpark')
d7, _ = W.capture(c, 'am fost in northgate, apoi la southpark', '2026-08-07'); d31, _ = W.capture(c, 'seara la lakeside', '2026-07-31')
st = statements(block('6.9'))
run_block(c, st[0], dict(day_page_id=d31, place_id=par))
S.K('§6.9 records an at link from the day page to the place, with its note', one(c, "select note from links where from_id=? and to_id=? and kind='at'", (d31, par)) == 'evening')
S.K('...and again is a no-op (ON CONFLICT DO NOTHING)', tryx(c, st[0], dict(day_page_id=d31, place_id=par)) == 'OK' and one(c, "select count(*) from links where kind='at'") == 1)
link(c, d7, cor, 'at'); link(c, d7, spa, 'at'); d8, _ = W.capture(c, 'iar la lakeside', '2026-08-08'); link(c, d8, par, 'at')
S.K('§6.9 where was I on 2026-08-07: both places, by title', [r[0] for r in c.execute(st[1], {'day': '2026-08-07'})] == ['Northgate', 'Southpark'])
S.K('§6.9 the days at Lakeside, newest first', [r[0] for r in c.execute(st[2], {'place_id': par})] == ['2026-08-08', '2026-07-31'])
S.K('an at link to a person is refused: at points at a place', link(c, d7, named(c, 'person', 'Ana'), 'at').startswith('ERR'))
S.K('an at link from a person is refused: at comes from a page', link(c, named(c, 'person', 'Ion'), par, 'at').startswith('ERR'))
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (spa,))
S.K('...and a tombstoned place is not where I was', [r[0] for r in c.execute(st[1], {'day': '2026-08-07'})] == ['Northgate'])

# ---- what stands in for recurrence (D15), events (D22) and tasks (D23)
c = fresh()
S.K('there is no tasks table and no task link kind (D23)', one(c, "select count(*) from sqlite_schema where name='tasks'") == 0
    and one(c, "select count(*) from link_kinds where kind in ('spawned','subtask')") == 0)
c.execute("INSERT INTO metrics(name,unit) VALUES ('rent_paid','')"); mid = one(c, "select id from metrics where name='rent_paid'")
for d, v in (('2026-01-31', 1), ('2026-02-28', 0), ('2026-03-31', 1)): measure(c, mid, d, v)
S.K('"did I do it each month" is a 0/1 habit metric', one(c, 'select group_concat(value) from (select value from measurement_values where metric_id=? order by day)', (mid,)) == '1.0,0.0,1.0')
p = named(c, 'person', 'Ada', birth_day='1815-12-10')
S.K('birthdays are a query over people.birth_day', c.execute("SELECT p.title FROM people pe JOIN pages p USING(id) WHERE strftime('%m-%d', pe.birth_day) = '12-10'").fetchall() == [('Ada',)])
S.K('no event kinds are registered: attended and is-a are gone with events (D22)', one(c, "select count(*) from link_kinds where kind in ('attended','is-a')") == 0)
S.done()
