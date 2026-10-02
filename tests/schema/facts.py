"""Measurements and metrics (contract/deletion-and-corrections, D6, D7): the registry, append-only rows, supersede chains and retraction,
finite values and NaN, the read view and its index, and why never OR IGNORE / OR REPLACE."""
import os, sys, datetime as dt
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('facts')

# ---- metrics
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); W_ = one(c, "select id from metrics where name='weight'")
S.K('mood is seeded (D6)', one(c, "select count(*) from metrics where name='mood'") == 1)
measure(c, W_, '2026-06-01', 70)
S.K('metrics.unit cannot change', tryx(c, "UPDATE metrics SET unit='lb' WHERE name='weight'").startswith('ERR'))
S.K('a no-op SET unit=unit with a note edit passes', tryx(c, "UPDATE metrics SET unit=unit, note='body weight' WHERE name='weight'") == 'OK')
S.K('a name typo can be fixed', tryx(c, "UPDATE metrics SET name='body_weight' WHERE name='weight'") == 'OK')
for nm in ['Blood Pressure', 'bp sys', 'bp-sys', '', 'Weight', 'MOOD', 'x(y)', 'ünï']:
    S.K(f'metric name {nm!r} rejected', tryx(c, "INSERT INTO metrics(name,unit) VALUES (?, 'x')", (nm,)).startswith('ERR'))
for nm in ['bp_sys', 'x1', '_a', 'steps_walked']:
    S.K(f'metric name {nm!r} accepted', tryx(c, "INSERT INTO metrics(name,unit) VALUES (?, 'x')", (nm,)) == 'OK')

# ---- append-only, supersede, retract
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); w = one(c, "select id from metrics where name='weight'")
def sup(v, s, metric=None, id_=None):
    cols = dict(supersedes_id=s) if id_ is None else dict(supersedes_id=s, id=id_)
    return measure(c, metric or w, '2026-06-01', v, **cols)
measure(c, w, '2026-06-01', 70)
S.K('a measurement without created_at is refused', tryx(c, "INSERT INTO measurements(metric_id,day,value,source) VALUES (1,'2026-06-01',3,'ui')").startswith('ERR'))
S.K('UPDATE of a value refused', tryx(c, 'UPDATE measurements SET value=1').startswith('ERR'))
S.K('UPDATE of supersedes_id or captured_with_id refused', tryx(c, 'UPDATE measurements SET supersedes_id=NULL').startswith('ERR') and tryx(c, 'UPDATE measurements SET captured_with_id=NULL').startswith('ERR'))
S.K('DELETE refused', tryx(c, 'DELETE FROM measurements').startswith('ERR'))
S.K('a correction supersedes a row', sup(71, 1) == 'OK')
S.K('a second correction of the same row is refused', sup(72, 1).startswith('ERR'))
S.K('the correction can be corrected (a chain)', sup(73, 2) == 'OK')
S.K('a correction of another metric is refused', sup(3, 3, metric=1).startswith('ERR'))
S.K('a dangling supersedes_id is refused', sup(3, 999).startswith('ERR'))
S.K('a row cannot supersede itself', sup(3, 50, id_=50).startswith('ERR'))
S.K('the view shows only the last of the chain', c.execute('select value from measurement_values where metric_id=?', (w,)).fetchall() == [(73.0,)])
S.K('a first reading with NULL value is refused', measure(c, w, '2026-06-02', None).startswith('ERR'))
measure(c, 1, '2026-06-01', 3); tap = one(c, 'select max(id) from measurements')          # a mood tap logged by mistake
S.K('retract the mis-tap with a NULL correction', measure(c, 1, '2026-06-01', None, supersedes_id=tap) == 'OK')
S.K('the tap and its retraction are hidden', one(c, 'select count(*) from measurement_values where metric_id=1') == 0)
S.K('correcting the retraction brings a value back', measure(c, 1, '2026-06-01', 4, supersedes_id=tap + 1) == 'OK' and c.execute('select value from measurement_values where metric_id=1').fetchall() == [(4.0,)])
S.K('nothing was deleted: all six rows remain', one(c, 'select count(*) from measurements') == 6)
measure(c, w, '2026-06-05', 70.1); measure(c, w, '2026-06-05', 70.4)
S.K('two independent readings on one day are both returned', one(c, "select count(*) from measurement_values where day='2026-06-05'") == 2)

# ---- the correction story of contract/deletion-and-corrections, executed
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); w = one(c, "select id from metrics where name='weight'")
shown = []
for v, s in ((71.2, None), (70.8, 1), (None, 2), (71.4, 3)):
    measure(c, w, '2026-09-30', v, supersedes_id=s); shown.append([r[0] for r in c.execute('select value from measurement_values where metric_id=?', (w,))])
S.K('corrected, retracted, restored: the view shows 71.2, 70.8, nothing, 71.4', shown == [[71.2], [70.8], [], [71.4]], shown)

# ---- finite values, NaN
c = fresh()
S.K('+Infinity refused', measure(c, 1, '2026-01-01', float('inf')).startswith('ERR'))
S.K('-Infinity refused', measure(c, 1, '2026-01-01', float('-inf')).startswith('ERR'))
S.K('the largest finite double, -0.0 and ordinary values accepted', all(measure(c, 1, '2026-01-01', v) == 'OK' for v in (1.7976931348623157e308, -0.0, 70.5)))
S.K('NaN binds as NULL: refused as a first reading', measure(c, 1, '2026-01-01', float('nan')).startswith('ERR'))
first = one(c, 'select max(id) from measurements')
S.K('...but on a correction it is a retraction the DB cannot tell apart (why the app never binds NaN)',
    measure(c, 1, '2026-01-01', float('nan'), supersedes_id=first) == 'OK' and c.execute('select value from measurements where supersedes_id=?', (first,)).fetchone() == (None,))

# ---- import idiom: ON CONFLICT DO NOTHING, never OR IGNORE, never OR REPLACE
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('steps','n')"); st = one(c, "select id from metrics where name='steps'")
imp = f"INSERT INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES ({st},?,?,?,?,{NOW}) ON CONFLICT(source,import_key,metric_id) WHERE import_key IS NOT NULL DO NOTHING"
S.K('first import row', tryx(c, imp, ('2026-06-09', 8000, 'import:apple', '1001')) == 'OK')
c.execute(imp, ('2026-06-09', 8000, 'import:apple', '1001'))
S.K('the same source, id and metric again inserts nothing', c.execute('select changes()').fetchone()[0] == 0 and one(c, 'select count(*) from measurements where import_key=\'1001\'') == 1)
S.K('the same import_key from another importer is kept', tryx(c, imp, ('2026-06-10', 9000, 'import:garmin', '1001')) == 'OK' and one(c, "select count(*) from measurements where import_key='1001'") == 2)
S.K('ON CONFLICT DO NOTHING still raises on a malformed day', tryx(c, imp, ('2026-6-9', 1, 'import:apple', '2002')).startswith('ERR'))
n = one(c, 'select count(*) from measurements')
S.K('OR IGNORE swallows a malformed day silently (the documented hole)', tryx(c, f"INSERT OR IGNORE INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES ({st},'2026-6-9',1,'import:apple','2002',{NOW})") == 'OK'
    and one(c, 'select count(*) from measurements') == n)
S.K('OR IGNORE also swallows a CHECK violation (a NULL first value)', tryx(c, f"INSERT OR IGNORE INTO measurements(metric_id,day,value,source,created_at) VALUES ({st},'2026-06-11',NULL,'ui',{NOW})") == 'OK'
    and one(c, 'select count(*) from measurements') == n)
c0 = fresh(rt=False); c0.execute("INSERT INTO metrics(name,unit) VALUES ('w','kg')"); measure(c0, 2, '2026-01-01', 70, source='import:s', import_key='k')
S.K('with recursive_triggers=OFF, OR REPLACE rewrites history (why the pragma is mandatory)',
    tryx(c0, f"INSERT OR REPLACE INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES (2,'2026-01-01',99,'import:s','k',{NOW})") == 'OK'
    and c0.execute('select value from measurements').fetchall() == [(99.0,)])
c1 = fresh(); c1.execute("INSERT INTO metrics(name,unit) VALUES ('w','kg')"); measure(c1, 2, '2026-01-01', 70, source='import:s', import_key='k')
S.K('with recursive_triggers=ON the same REPLACE is refused and history is intact',
    tryx(c1, f"INSERT OR REPLACE INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES (2,'2026-01-01',99,'import:s','k',{NOW})").startswith('ERR')
    and c1.execute('select value from measurements').fetchall() == [(70.0,)])

# ---- cookbook/metric-series: the 90 days ending on :day
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); w = one(c, "select id from metrics where name='weight'")
for d_ in ('2026-07-04', '2026-07-05', '2026-10-02', '2026-10-03'): measure(c, w, d_, 70)
S.K('cookbook/metric-series: the 90 days ending on :day, none after it', [r[0] for r in c.execute(block('metric-series'), {'day': '2026-10-02'})] == ['2026-07-05', '2026-10-02'])

# ---- the read view is index-served
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('w','kg')"); c.execute('BEGIN')
c.executemany(f"INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES (2,?,?,'ui',{NOW})",
              [((dt.date(2000, 1, 1) + dt.timedelta(days=i % 9000)).isoformat(), i * 0.001) for i in range(20000)])
c.execute('COMMIT')
plan = ' | '.join(r[3] for r in c.execute('EXPLAIN QUERY PLAN SELECT count(*) FROM measurement_values'))
S.K('measurement_values uses measurements_one_correction for its NOT EXISTS', 'measurements_one_correction' in plan, plan)
S.K('...and counts 20 000 rows', one(c, 'select count(*) from measurement_values') == 20000)
S.done()
