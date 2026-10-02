"""Habits (D24): a unitless metric with active periods. The CHECKs and triggers of habit_periods (days, order,
no overlap on insert or update, unitless only, never deleted); cookbook/habits run literally (start, stop, the day's habits done /
not done / not recorded, completion over a period); the cookbook/day-view day view lists the day's habits and not their check-ins
again; a day without a check-in is never assumed."""
import os, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('habits')

def metric(c, name, unit=''):
    return c.execute('INSERT INTO metrics(name,unit) VALUES (?,?) RETURNING id', (name, unit)).fetchone()[0]

# ---- the table's rules
c = fresh(); vd, kg = metric(c, 'vitamin_d'), metric(c, 'weight_x', 'kg')
S.K('a period on a unitless metric is accepted', habit(c, vd, '2026-10-01', '2026-10-31') == 'OK')
for bad in ('2026-9-3', '2026-02-31', 'today'):
    S.K(f'start_day {bad!r} refused', habit(c, vd, bad).startswith('ERR'))
S.K('an end before the start refused (habit_periods_order)', habit(c, vd, '2027-01-10', '2027-01-09').startswith('ERR'))
S.K('a period on a metric with a unit refused: a habit is 0/1', 'unitless' in habit(c, kg, '2026-10-01'))
S.K('an overlapping period refused', 'overlap' in habit(c, vd, '2026-10-31', '2026-11-05'))
S.K('an open period overlapping a closed one refused', 'overlap' in habit(c, vd, '2026-09-01'))
S.K('a period right after the last one accepted (back to back)', habit(c, vd, '2026-11-01', '2026-11-30') == 'OK')
S.K('an open period after them accepted', habit(c, vd, '2027-01-01') == 'OK')
S.K('...and a second open one refused', 'overlap' in habit(c, vd, '2027-06-01'))
S.K('the same start twice is refused by UNIQUE', 'UNIQUE' in habit(c, vd, '2027-01-01'))
S.K('...and ON CONFLICT DO NOTHING makes it a no-op (the trigger leaves it to UNIQUE)',
    tryx(c, "INSERT INTO habit_periods(metric_id,start_day,source) VALUES (?, '2027-01-01', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (vd,)) == 'OK')
S.K('a re-run with a changed end_day is a no-op until the UPDATE follows',
    tryx(c, "INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES (?, '2026-10-01', '2026-10-15', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (vd,)) == 'OK'
    and one(c, "select end_day from habit_periods where start_day='2026-10-01'") == '2026-10-31')
S.K('cookbook/habits documents the idempotent re-run', 'ON CONFLICT(metric_id, start_day) DO NOTHING' in doc_page('cookbook/habits.md'))
# a restarted habit: the re-send of the closed first period must carry its end_day (cookbook/habits)
c2 = fresh(); rs = metric(c2, 'stretching')
S.K('a closed period, then a later open one (a restarted habit)',
    habit(c2, rs, '2026-01-01', '2026-01-31') == 'OK' and habit(c2, rs, '2026-03-01') == 'OK')
S.K('cookbook/habits re-send of the closed period WITH its end_day is a no-op beside the later period',
    tryx(c2, "INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES (?, '2026-01-01', '2026-01-31', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (rs,)) == 'OK'
    and one(c2, 'select count(*) from habit_periods where metric_id=?', (rs,)) == 2)
S.K('...re-sent WITHOUT its end_day it is an open period, and the insert trigger refuses the overlap',
    'overlap' in tryx(c2, "INSERT INTO habit_periods(metric_id,start_day,source) VALUES (?, '2026-01-01', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (rs,)))
S.K('cookbook/habits says a re-sent period carries its end_day', 'carrying the `end_day` it was sent with' in doc_page('cookbook/habits.md'))
S.K('moving a period onto another refused (update)', 'overlap' in tryx(c, "UPDATE habit_periods SET end_day='2026-11-15' WHERE start_day='2026-10-01'"))
S.K('...moving it into a gap accepted', tryx(c, "UPDATE habit_periods SET end_day='2026-10-30' WHERE start_day='2026-10-01'") == 'OK')
S.K('moving a period to a metric with a unit refused (update)', 'unitless' in tryx(c, "UPDATE habit_periods SET metric_id=? WHERE start_day='2026-10-01'", (kg,)))
S.K('a period is never deleted', tryx(c, 'DELETE FROM habit_periods').startswith('ERR') and one(c, 'select count(*) from habit_periods') == 3)
S.K("a period's source never changes", 'never changed' in tryx(c, "UPDATE habit_periods SET source='cli' WHERE start_day='2026-10-01'"))
S.K("...a full-row update that keeps it accepted", tryx(c, "UPDATE habit_periods SET source='ui', end_day=end_day WHERE start_day='2026-10-01'") == 'OK')
S.K('mood is not a habit: it has no period', one(c, "select count(*) from habit_periods h join metrics m on m.id=h.metric_id where m.name='mood'") == 0)
S.K('the database is clean', integrity_ok(c))

# ---- cookbook/habits run literally
B = statements(block('habits'))
S.K('cookbook/habits has a start, a stop, the day\'s habits and the completion query', len(B) == 4, [code(b)[:30] for b in B])
start, stop, today, completion = (B + ['select 1'] * 4)[:4]
c = fresh(); vd, wbc = metric(c, 'vitamin_d'), metric(c, 'water_before_coffee')
c.execute(start, {'metric': 'vitamin_d', 'day': '2026-10-01'}); c.execute(start, {'metric': 'water_before_coffee', 'day': '2026-10-03'})
for day, v in (('2026-10-01', 1), ('2026-10-02', 0), ('2026-10-04', 1), ('2026-09-30', 1)):
    measure(c, vd, day, v)
measure(c, wbc, '2026-10-03', 1); measure(c, wbc, '2026-10-03', 1)       # two check-ins on one day count once
day = lambda d: c.execute(today, {'day': d}).fetchall()
S.K('cookbook/habits the habits of a day: done', day('2026-10-01') == [('vitamin_d', 'done')], day('2026-10-01'))
S.K('...not done (an explicit 0)', day('2026-10-02') == [('vitamin_d', 'not done')], day('2026-10-02'))
S.K('...not recorded (no check-in): never assumed', ('vitamin_d', 'not recorded') in day('2026-10-03') and ('water_before_coffee', 'done') in day('2026-10-03'), day('2026-10-03'))
S.K('...and before a habit started, it is not a habit of that day (even with a reading)', day('2026-09-30') == [], day('2026-09-30'))
c.execute(stop, {'metric': 'vitamin_d', 'day': '2026-10-04'})
S.K('cookbook/habits stop ends the open period on that day, inclusive', day('2026-10-04') == [('vitamin_d', 'done'), ('water_before_coffee', 'not recorded')]
    and ('vitamin_d', 'done') not in day('2026-10-05') and day('2026-10-05') == [('water_before_coffee', 'not recorded')], (day('2026-10-04'), day('2026-10-05')))
S.K('...and a restart is a new period', tryx(c, start, {'metric': 'vitamin_d', 'day': '2026-10-10'}) == 'OK' and day('2026-10-10')[0] == ('vitamin_d', 'not recorded'))
comp = c.execute(completion, {'from_day': '2026-09-28', 'to_day': '2026-10-11'}).fetchall()
S.K('cookbook/habits completion counts only active days: done, not done, not recorded',
    comp == [('vitamin_d', 4 + 2, 2, 1, 3), ('water_before_coffee', 9, 1, 0, 8)], comp)
c.execute("INSERT INTO measurements(metric_id,day,value,source,supersedes_id,created_at) VALUES (?, '2026-10-02', 1, 'ui', ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))",
          (vd, one(c, "select id from measurements where day='2026-10-02'")))
S.K('a corrected check-in counts as corrected (measurement_values)', day('2026-10-02') == [('vitamin_d', 'done')], day('2026-10-02'))

# ---- cookbook/day-view lists the day's habits, and not their check-ins a second time
DV = block('day-view')
day_page(c, '2026-10-01', 'a day'); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); measure(c, one(c, "select id from metrics where name='weight'"), '2026-10-01', 71.0)
rows = c.execute(DV, {'day': '2026-10-01'}).fetchall()
got = {(r[0], r[2]) for r in rows}
S.K('cookbook/day-view shows the habit with its state and the other readings', {('habit', 'vitamin_d: done'), ('weight', '71.0 kg')} <= got, rows)
S.K('...and not the habit\'s check-in again as a reading', not [r for r in rows if r[0] == 'vitamin_d'], rows)
rows = c.execute(DV, {'day': '2026-09-30'}).fetchall()
S.K('...outside every period a 0/1 reading is just a reading, and no habit is listed', ('vitamin_d', '1.0 ') in {(r[0], r[2]) for r in rows}
    and not [r for r in rows if r[0] == 'habit'], rows)
S.done()
