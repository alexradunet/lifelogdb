"""The journal (SCHEMA.md D5, D10, D15): events and tasks and their CHECKs, the §6.2 day view and the §6.3 inbox run from
the document, completion on the LOCAL day, one place per event, and what stands in for recurrence."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('journal')

# ---- events
c = fresh()
def ev(**kw):
    cols = dict(id=ent(c, 'event'), name='t', start_day='2026-06-01', **kw)
    return tryx(c, f"INSERT INTO events({','.join(cols)}) VALUES ({','.join('?' * len(cols))})", tuple(cols.values()))
S.K('an event may be day-precise only', ev() == 'OK')
S.K('end_day before start_day refused', ev(end_day='2026-05-31').startswith('ERR'))
S.K('end_at before start_at refused', ev(start_at='2026-06-01T10:00:00.000Z', end_at='2026-06-01T09:00:00.000Z').startswith('ERR'))
S.K('a place that does not exist refused', ev(place_id=9999).startswith('ERR'))
S.K('an event\'s place must be a place, not another named entity', ev(place_id=named(c, 'person')).startswith('ERR'))
cols = lambda t: [r[1] for r in c.execute(f"pragma table_info('{t}')")]
S.K('events and tasks have no recurrence columns (D15)', not [x for x in cols('events') + cols('tasks') if x.startswith('repeat')])
S.K('tasks have no status column: open = completed_at IS NULL', 'status' not in cols('tasks'))

# ---- tasks
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
memo(c, 'a memo', day='2026-09-29'); page(c, 'Essay', day='2026-09-29', body='text'); page(c, 'Link target'); page(c, 'Yesterday essay', day='2026-09-28', body='x')
named(c, 'person', 'Sam')
e = ent(c, 'event'); domain(c, 'event', e, name='Trip', start_day='2026-09-28', end_day='2026-09-30')
tk = ent(c, 'task'); domain(c, 'task', tk, name='Overdue', due_day='2026-09-01')
dn = ent(c, 'task'); domain(c, 'task', dn, name='Later', due_day='2026-10-01')
c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); measure(c, 2, '2026-09-29', 71.2, taken_at='2026-09-29T06:00:00.000Z'); measure(c, 2, '2026-09-29', 70.0, supersedes_id=1)
rows = c.execute(DV, {'day': '2026-09-29'}).fetchall()
got = {(r[0], r[2]) for r in rows}
S.K('§6.2 shows the memo, the page written that day, the running event, the overdue task and the corrected reading',
    {('memo', 'a memo'), ('page', 'Essay'), ('event', 'Trip'), ('task', 'Overdue'), ('weight', '70.0 kg')} <= got, rows)
S.K('...not a link target, a person\'s page, a page of another day, a task due later or the superseded reading',
    not {r[2] for r in rows} & {'Link target', 'Sam', 'Yesterday essay', 'Later', '71.2 kg'}, rows)
S.K('...undated items first, then chronological', [r[1] is None for r in rows] == sorted([r[1] is None for r in rows], reverse=True))
pid = one(c, "select id from pages where title='Essay'")
c.execute("UPDATE entities SET created_at='2026-09-29T08:00:00.000Z' WHERE id=?", (pid,)); c.execute("UPDATE pages SET body='text 2' WHERE id=?", (pid,))
S.K('an edited page is flagged "(edited)"', ('page (edited)', 'Essay') in {(r[0], r[2]) for r in c.execute(DV, {'day': '2026-09-29'})})

# ---- §6.3 inbox and §6.9 triage
c = fresh(); m1, m2 = memo(c, 'first'), memo(c, 'second'); junk = memo(c, 'junk')
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (junk,))
S.K('§6.3 lists untriaged live memos', sorted(r[0] for r in c.execute(block('6.3'))) == [m1, m2])
run_block(c, block('6.9'), dict(memo_id=m1, due_day='2026-10-05'))
S.K('after §6.9 the triaged memo leaves the inbox', [r[0] for r in c.execute(block('6.3'))] == [m2])
plan = ' | '.join(r[3] for r in c.execute('EXPLAIN QUERY PLAN ' + block('6.3')))
S.K('the inbox is served by the partial index pages_inbox', 'pages_inbox' in plan, plan)

# ---- one place per event; where did I live
c = fresh(); pe = named(c, 'person'); pl = named(c, 'place', 'Berlin')
e = ent(c, 'event'); domain(c, 'event', e, name='Living in Berlin', start_day='2015-03-01', end_day='2018-08-31', place_id=pl)
Q = "SELECT p.title FROM events ev JOIN entities e ON e.id=ev.id AND e.deleted_at IS NULL JOIN pages p ON p.id=ev.place_id WHERE ev.start_day <= :day AND coalesce(ev.end_day, ev.start_day) >= :day"
S.K("'where did I live on 2015-06-01' is a dated event with a place", c.execute(Q, dict(day='2015-06-01')).fetchall() == [('Berlin',)])
S.K('...and is empty after it ended', c.execute(Q, dict(day='2019-01-01')).fetchall() == [])

# ---- what stands in for recurrence (D15)
c = fresh(); t1 = ent(c, 'task'); domain(c, 'task', t1, name='pay rent', due_day='2026-01-01')
t2 = ent(c, 'task'); domain(c, 'task', t2, name='pay rent', due_day='2026-02-01')
S.K('the next reminder is linked with the symmetric related (two edges)', link(c, t2, t1, 'related') == 'OK' and one(c, "select count(*) from links where kind='related'") == 2)
S.K('spawned task->task stays refused', link(c, t2, t1, 'spawned').startswith('ERR'))
c.execute("INSERT INTO metrics(name,unit) VALUES ('rent_paid','')"); mid = one(c, "select id from metrics where name='rent_paid'")
for d, v in (('2026-01-31', 1), ('2026-02-28', 0), ('2026-03-31', 1)): measure(c, mid, d, v)
S.K('"did I do it each month" is a 0/1 habit metric', one(c, 'select group_concat(value) from (select value from measurement_values where metric_id=? order by day)', (mid,)) == '1.0,0.0,1.0')
p = named(c, 'person', 'Ada', birth_day='1815-12-10')
S.K('birthdays are a query over people.birth_day', c.execute("SELECT p.title FROM people pe JOIN pages p USING(id) WHERE strftime('%m-%d', pe.birth_day) = '12-10'").fetchall() == [('Ada',)])
S.done()
