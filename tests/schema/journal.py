"""The journal (SCHEMA.md D5, D10, D15, D22): the day page and its CHECK, capture that appends to it (§6.1), the §6.2 day
view, the days that name someone or somewhere (§6.3), a task from a day page (§6.9), tasks and their CHECKs, completion on
the LOCAL day, and what stands in for recurrence and for events."""
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

# ---- tasks
c = fresh()
cols = lambda t: [r[1] for r in c.execute(f"pragma table_info('{t}')")]
S.K('tasks have no recurrence columns (D15)', not [x for x in cols('tasks') if x.startswith('repeat')])
S.K('tasks have no status column: open = completed_at IS NULL', 'status' not in cols('tasks'))
t = lambda: ent(c, 'task')
S.K('done without completed_day refused', tryx(c, f"INSERT INTO tasks(id,name,completed_at) VALUES (?,'x',{NOW})", (t(),)).startswith('ERR'))
S.K('done without completed_at refused', tryx(c, "INSERT INTO tasks(id,name,completed_day) VALUES (?,'x','2026-06-09')", (t(),)).startswith('ERR'))
S.K('a malformed completed_day refused', tryx(c, f"INSERT INTO tasks(id,name,completed_at,completed_day) VALUES (?,'x',{NOW},'2026-6-9')", (t(),)).startswith('ERR'))
a = t(); S.K('done with both accepted', tryx(c, "INSERT INTO tasks(id,name,completed_at,completed_day) VALUES (?,'ship it','2026-06-09T22:30:00.000Z','2026-06-10')", (a,)) == 'OK')
b = t(); c.execute("INSERT INTO tasks(id,name,due_day) VALUES (?,'open one','2026-06-01')", (b,))
S.K('completing an open task: the documented UPDATE', tryx(c, f"UPDATE tasks SET completed_at={NOW}, completed_day='2026-06-11' WHERE id=?", (b,)) == 'OK')
S.K('half un-completing it is refused', tryx(c, 'UPDATE tasks SET completed_at=NULL WHERE id=?', (b,)).startswith('ERR'))
plan = ' | '.join(r[3] for r in c.execute("EXPLAIN QUERY PLAN SELECT id FROM tasks WHERE completed_at IS NULL AND due_day <= '2026-06-01'"))
S.K('open tasks by due day use the partial index tasks_open', 'tasks_open' in plan, plan)

# ---- §6.2 day view
DV = block('6.2')
rows = c.execute(DV, dict(day='2026-06-10')).fetchall()
S.K('§6.2 lists the task done on LOCAL 2026-06-10 (its UTC instant is 06-09 22:30)', ('done', '2026-06-09T22:30:00.000Z', 'ship it') in rows)
S.K('...and not on 2026-06-09', ('done', '2026-06-09T22:30:00.000Z', 'ship it') not in c.execute(DV, dict(day='2026-06-09')).fetchall())
c = fresh()
day_page(c, '2026-09-29', 'a day of work'); day_page(c, '2026-09-28', 'yesterday')
page(c, 'Essay', day='2026-09-29', body='text'); page(c, 'Link target'); page(c, 'Yesterday essay', day='2026-09-28', body='x')
named(c, 'person', 'Sam')
tk = ent(c, 'task'); domain(c, 'task', tk, name='Overdue', due_day='2026-09-01')
dn = ent(c, 'task'); domain(c, 'task', dn, name='Later', due_day='2026-10-01')
c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); measure(c, 2, '2026-09-29', 71.2, taken_at='2026-09-29T06:00:00.000Z'); measure(c, 2, '2026-09-29', 70.0, supersedes_id=1)
rows = c.execute(DV, {'day': '2026-09-29'}).fetchall()
got = {(r[0], r[2]) for r in rows}
S.K('§6.2 shows the day page, the page written that day, the overdue task and the corrected reading',
    {('day page', 'a day of work'), ('page', 'Essay'), ('task', 'Overdue'), ('weight', '70.0 kg')} <= got, rows)
S.K('...the day page once, as the day page and not again as a page written that day', [r[0] for r in rows].count('day page') == 1 and ('page', '2026-09-29') not in got, rows)
S.K('...not another day\'s page, a link target, a person\'s page, a page of another day, a task due later or the superseded reading',
    not {r[2] for r in rows} & {'yesterday', 'Link target', 'Sam', 'Yesterday essay', 'Later', '71.2 kg'}, rows)
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

# ---- §6.9 a task from a day page
c = fresh(); dp = day_page(c, '2026-09-30', 'call the dentist'); P = dict(page_id=dp, due_day='2026-10-05')
try: run_block(c, block('6.9'), P); r = 'OK'
except sqlite3.Error as e: r = 'ERR ' + str(e)
S.K('§6.9 run literally: the task it RETURNed is spawned from the day page', r == 'OK'
    and c.execute("select t.id, l.to_id from tasks t join links l on l.from_id=t.id and l.kind='spawned'").fetchall() == [(P.get('task_id'), dp)], (r, P))

# ---- what stands in for recurrence (D15) and for events (D22)
c = fresh(); t1 = ent(c, 'task'); domain(c, 'task', t1, name='pay rent', due_day='2026-01-01')
t2 = ent(c, 'task'); domain(c, 'task', t2, name='pay rent', due_day='2026-02-01')
S.K('the next reminder is linked with the symmetric related (two edges)', link(c, t2, t1, 'related') == 'OK' and one(c, "select count(*) from links where kind='related'") == 2)
S.K('spawned task->task stays refused', link(c, t2, t1, 'spawned').startswith('ERR'))
c.execute("INSERT INTO metrics(name,unit) VALUES ('rent_paid','')"); mid = one(c, "select id from metrics where name='rent_paid'")
for d, v in (('2026-01-31', 1), ('2026-02-28', 0), ('2026-03-31', 1)): measure(c, mid, d, v)
S.K('"did I do it each month" is a 0/1 habit metric', one(c, 'select group_concat(value) from (select value from measurement_values where metric_id=? order by day)', (mid,)) == '1.0,0.0,1.0')
p = named(c, 'person', 'Ada', birth_day='1815-12-10')
S.K('birthdays are a query over people.birth_day', c.execute("SELECT p.title FROM people pe JOIN pages p USING(id) WHERE strftime('%m-%d', pe.birth_day) = '12-10'").fetchall() == [('Ada',)])
S.K('no event kinds are registered: attended and is-a are gone with events (D22)', one(c, "select count(*) from link_kinds where kind in ('attended','is-a')") == 0)
S.done()
