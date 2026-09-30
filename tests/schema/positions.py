"""Positions and place coordinates (SCHEMA.md D21, §6.20): a fix's CHECKs, NaN and infinity, append-only rows, import
idempotency, the indexes the §6.20 reads walk, a place's point, §6.20 run literally, its nearest place against a haversine
oracle, the antimeridian limit D21 states, and no math function in the SQL."""
import os, re, sys, math, random
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('positions')
INF, NAN = float('inf'), float('nan')

def fix(c, taken_at='2026-09-30T10:00:00.000Z', day='2026-09-30', lat=44.43, lon=26.10, source='ui', **cols):
    cols = dict(taken_at=taken_at, day=day, lat=lat, lon=lon, source=source, **cols)
    return tryx(c, f"INSERT INTO positions({','.join(cols)},recorded_at) VALUES ({','.join('?' * len(cols))},{NOW})", tuple(cols.values()))

# ---- a fix: time, zone, source
c = fresh()
S.K('a plain fix is accepted', fix(c) == 'OK')
for bad in ['2026-09-30T10:00:00Z', '2026-09-30 10:00:00.000', '2026-9-30T10:00:00.000Z', '2026-09-30T25:00:00.000Z', '']:
    S.K(f'taken_at {bad!r} is refused (round-trip CHECK)', fix(c, taken_at=bad).startswith('ERR'))
for bad in ['2026-9-30', '2026-02-30', '30.09.2026', '']:
    S.K(f'day {bad!r} is refused (round-trip CHECK)', fix(c, day=bad).startswith('ERR'))
S.K('a fix without taken_at is refused', tryx(c, f"INSERT INTO positions(day,lat,lon,recorded_at,source) VALUES ('2026-09-30',1,1,{NOW},'ui')").startswith('ERR'))
S.K('a fix without day is refused', tryx(c, f"INSERT INTO positions(taken_at,lat,lon,recorded_at,source) VALUES ('2026-09-30T10:00:00.000Z',1,1,{NOW},'ui')").startswith('ERR'))
S.K('a malformed recorded_at is refused', tryx(c, "INSERT INTO positions(taken_at,day,lat,lon,recorded_at,source) VALUES ('2026-09-30T10:00:00.000Z','2026-09-30',1,1,'2026-09-30 10:00','ui')").startswith('ERR'))
S.K('a zone with a space is refused', fix(c, tz='Europe/ Berlin').startswith('ERR'))
S.K('an IANA zone is accepted', fix(c, tz='Europe/Berlin') == 'OK')
S.K('source is required', tryx(c, f"INSERT INTO positions(taken_at,day,lat,lon,recorded_at) VALUES ('2026-09-30T10:00:00.000Z','2026-09-30',1,1,{NOW})").startswith('ERR'))
S.K("source 'UI' is refused (lowercase)", fix(c, source='UI').startswith('ERR'))
S.K("source 'import:gpslogger' is accepted", fix(c, source='import:gpslogger') == 'OK')

# ---- coordinates and accuracy
for v in (90, -90, 0, 45.5, '45.5'):
    S.K(f'latitude {v!r} is accepted', fix(c, lat=v) == 'OK')
for v in (180, -180, 26.1):
    S.K(f'longitude {v!r} is accepted', fix(c, lon=v) == 'OK')
for v in (90.000001, -90.000001, 1e999, -1e999, 'abc', ''):
    S.K(f'latitude {v!r} is refused', fix(c, lat=v).startswith('ERR'))
for v in (180.000001, -180.000001, 1e999, 'abc'):
    S.K(f'longitude {v!r} is refused', fix(c, lon=v).startswith('ERR'))
S.K('a fix at exactly 0, 0 is refused (an exporter\'s "no location", positions_not_null_island)', fix(c, lat=0, lon=0).startswith('ERR CHECK constraint failed: positions_not_null_island'))
S.K('so is -0.0, -0.0 (it equals 0)', fix(c, lat=-0.0, lon=-0.0).startswith('ERR'))
S.K('a fix on the equator or on the prime meridian alone is accepted', fix(c, lat=0, lon=26.1) == 'OK' and fix(c, lat=44.4, lon=0) == 'OK' and fix(c, lat=0.000001, lon=0) == 'OK')
S.K('a NaN latitude is refused (SQLite binds NaN as NULL; NOT NULL refuses it)', fix(c, lat=NAN).startswith('ERR NOT NULL'))
S.K('a NaN longitude is refused the same way', fix(c, lon=NAN).startswith('ERR NOT NULL'))
for v in (0, 12.5, None):
    S.K(f'accuracy_m {v!r} is accepted', fix(c, accuracy_m=v) == 'OK')
for v in (-1, 1e8, INF):
    S.K(f'accuracy_m {v!r} is refused', fix(c, accuracy_m=v).startswith('ERR'))

# ---- append-only
c = fresh(); fix(c, accuracy_m=10); first = c.execute('select * from positions').fetchall()
S.K('UPDATE of a coordinate is refused', tryx(c, 'UPDATE positions SET lat = 0').startswith('ERR'))
S.K('UPDATE of source is refused', tryx(c, "UPDATE positions SET source = 'cli'").startswith('ERR'))
S.K('DELETE is refused', tryx(c, 'DELETE FROM positions').startswith('ERR'))
S.K('INSERT OR REPLACE over a fix is refused (recursive_triggers=ON fires positions_no_delete)',
    tryx(c, f"INSERT OR REPLACE INTO positions(id,taken_at,day,lat,lon,recorded_at,source) VALUES (1,'2026-09-30T11:00:00.000Z','2026-09-30',1,1,{NOW},'ui')").startswith('ERR'))
S.K('the fix is unchanged after all of that', c.execute('select * from positions').fetchall() == first)

# ---- imports: ON CONFLICT DO NOTHING, taken_at as import_id when the export has none
c = fresh(); c.execute("ATTACH ':memory:' AS s")
c.execute('CREATE TABLE s.staging(taken_at, day, tz, lat, lon, acc)')
rnd = random.Random(7)
rows = [(f'2026-09-{d:02d}T{h:02d}:{m:02d}:00.000Z', f'2026-09-{d:02d}', 'Europe/Berlin' if m % 2 else '', f'{44.4 + rnd.random() / 10:.6f}',
         f'{26.0 + rnd.random() / 10:.6f}', '' if h % 5 == 0 else f'{rnd.uniform(3, 80):.1f}') for d in range(1, 11) for h in range(1, 21) for m in (0, 15, 30, 45)]
c.executemany('INSERT INTO s.staging VALUES (?,?,?,?,?,?)', rows)
IMPORT = f"""INSERT INTO positions(taken_at, day, tz, lat, lon, accuracy_m, recorded_at, source, import_id)
SELECT taken_at, day, NULLIF(tz, ''), lat, lon, NULLIF(acc, ''), {NOW}, :source, taken_at
  FROM s.staging WHERE true
ON CONFLICT(source, import_id) WHERE import_id IS NOT NULL DO NOTHING"""
def load(source='import:gps'):
    c.execute('BEGIN IMMEDIATE'); n = c.execute(IMPORT, dict(source=source)).rowcount; c.execute('COMMIT'); return n
S.K(f'the import inserts all {len(rows)} staged fixes', load() == len(rows))
S.K('running it a second time inserts nothing', load() == 0 and one(c, 'select count(*) from positions') == len(rows))
S.K("an empty CSV field became NULL, not '' (NULLIF)", one(c, 'select count(*) from positions where accuracy_m is null') > 0 and one(c, "select count(*) from positions where tz = ''") == 0)
S.K('the same import_id under another source is another fix (per-source namespace)', load('import:other') == len(rows))
c.execute("INSERT INTO s.staging VALUES ('2026-09-11T10:00:00.000Z','2026-09-11','','91','26','')")
before = one(c, 'select count(*) from positions')
try: load('import:third'); r = 'OK'
except sqlite3.Error as e: r = str(e); c.execute('ROLLBACK')
S.K('one impossible latitude fails the whole batch (DO NOTHING skips only duplicates)', r != 'OK' and one(c, 'select count(*) from positions') == before, r)
S.K('the unique index is partial: fixes without import_id never collide', fix(c) == 'OK' and fix(c) == 'OK')

# ---- §6.20: the indexes the reads walk
B = block('6.20'); ST = statements(B)
S.K('§6.20 has four statements: record, at a moment, a day, which place', len(ST) == 4, len(ST))
def plan(st): return ' '.join(r[3] for r in c.execute('EXPLAIN QUERY PLAN ' + code(st), {k: None for k in re.findall(r':(\w+)', code(st))}))
c.execute('ANALYZE')
if len(ST) == 4:
    S.K('"where was I at :at" walks positions_time', 'positions_time' in plan(ST[1]), plan(ST[1]))
    S.K("a day's track walks positions_day", 'positions_day' in plan(ST[2]), plan(ST[2]))
S.K('§6.20 uses no math function (sin, cos, radians, sqrt …): it runs on every SQLite',
    not re.search(r'\b(sin|cos|tan|asin|acos|atan2?|radians|degrees|sqrt|pow|power|exp|ln|log)\s*\(', code(B), re.I))

# ---- a place's point
c = fresh(); home = named(c, 'place', 'Home')
S.K('a place without a point is accepted', one(c, 'select lat is null and lon is null from places where id = ?', (home,)) == 1)
S.K('a latitude without a longitude is refused (places_coords_pair)', tryx(c, 'UPDATE places SET lat = 44.4 WHERE id = ?', (home,)).startswith('ERR'))
S.K('a point can be set, and moved (it describes the place)', tryx(c, 'UPDATE places SET lat = 44.4, lon = 26.1 WHERE id = ?', (home,)) == 'OK'
    and tryx(c, 'UPDATE places SET lat = 44.41, lon = 26.12 WHERE id = ?', (home,)) == 'OK')
S.K('a point can be removed, both halves at once', tryx(c, 'UPDATE places SET lat = NULL, lon = NULL WHERE id = ?', (home,)) == 'OK')
for lat, lon in ((91, 0), (0, 181), (INF, 0), (NAN, 26.1)):
    S.K(f'a place at ({lat}, {lon}) is refused', tryx(c, 'UPDATE places SET lat = ?, lon = ? WHERE id = ?', (lat, lon, home)).startswith('ERR'))
try: named(c, 'place', 'Half', lat=10.0); r = 'OK'
except sqlite3.Error as e: r = 'ERR ' + str(e)
S.K('a new place with only a latitude is refused', r.startswith('ERR'), r)

# ---- §6.20 run literally
c = fresh(); pl = named(c, 'place', 'Office', lat=44.4268, lon=26.1025)
P = dict(taken_at='2026-09-29T22:30:00.000Z', day='2026-09-30', lat=44.4270, lon=26.1027, at='2026-09-30T08:00:00.000Z', max_accuracy_m=100,
         m_per_deg_lon=111320 * math.cos(math.radians(44.4270)), radius_m=200)
fix(c, taken_at='2026-09-30T07:00:00.000Z', lat=44.5, lon=26.2, accuracy_m=800)          # later, but too coarse
fix(c, taken_at='2026-09-30T09:00:00.000Z', lat=44.6, lon=26.3, accuracy_m=5)            # after :at
fix(c, taken_at='2026-09-29T21:00:00.000Z', day='2026-09-29', lat=44.0, lon=26.0)        # the day before, locally
out = run_block(c, B, P)
S.K('§6.20 runs literally and records the fix, id by RETURNING', out and P.get('position_id') == one(c, "select id from positions where taken_at = '2026-09-29T22:30:00.000Z'"))
if len(out) == 4:
    S.K('where was I at 08:00: the pin of 22:30 the evening before (the 07:00 fix is too coarse, the 09:00 one later)', out[1] == [('2026-09-29T22:30:00.000Z', 44.4270, 26.1027, 12.0)], out[1])
    S.K('the local day 2026-09-30 starts with the pin taken at 22:30 UTC the day before, in time order, coarse fix left out',
        [r[0] for r in out[2]] == ['2026-09-29T22:30:00.000Z', '2026-09-30T09:00:00.000Z'], out[2])
    S.K('the pin was at the Office, about 27 m away', out[3] and out[3][0][:2] == (pl, 'Office') and 20 < math.sqrt(out[3][0][2]) < 35, out[3])
c.execute(f"UPDATE entities SET deleted_at = {NOW} WHERE id = ?", (pl,))
S.K('a tombstoned place is never the answer', run_block(c, ST[3] if len(ST) == 4 else B, P)[-1] == [])

# ---- the nearest place against a haversine oracle, inside a city
def hav(a, b, c_, d):
    p1, p2, dl = math.radians(a), math.radians(c_), math.radians(d - b)
    h = math.sin((p2 - p1) / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 2 * 6371008.8 * math.asin(math.sqrt(h))
c = fresh(); rnd = random.Random(2026); live = {}
for i in range(60):
    la, lo = 44.38 + rnd.random() * 0.1, 26.03 + rnd.random() * 0.14
    p = named(c, 'place', f'Spot {i}', lat=la, lon=lo); live[p] = (la, lo)
for i in range(10):
    la, lo = 44.38 + rnd.random() * 0.1, 26.03 + rnd.random() * 0.14
    p = named(c, 'place', f'Gone {i}', lat=la, lon=lo); c.execute(f'UPDATE entities SET deleted_at = {NOW} WHERE id = ?', (p,))
for i in range(5): named(c, 'place', f'Nowhere {i}')
agree = total = found = 0; wrong = []; err = 0.0
for _ in range(400):
    la, lo = 44.38 + rnd.random() * 0.1, 26.03 + rnd.random() * 0.14
    P = dict(lat=la, lon=lo, m_per_deg_lon=111320 * math.cos(math.radians(la)), radius_m=600)
    got = run_block(c, ST[3], P)[0] if len(ST) == 4 else None
    ds = sorted((hav(la, lo, *v), k) for k, v in live.items())
    want = [ds[0][1]] if ds[0][0] <= 600 else []
    edge = abs(ds[0][0] - 600) < 6                                  # within 1 % of the radius: either answer is right
    if got is not None and ([r[0] for r in got] == want or (edge and [r[0] for r in got] in ([], [ds[0][1]]))): agree += 1
    else: wrong.append((la, lo, got, ds[:2]))
    if got: err = max(err, abs(math.sqrt(got[0][2]) / hav(la, lo, *live[got[0][0]]) - 1))
    total += 1; found += bool(want)
S.K(f'the §6.20 place is the haversine-nearest live place within 600 m for all 400 fixes ({found} near a place; at the radius itself, within 1 %, either answer)',
    agree == total and found > 50, wrong[:2])
S.K(f'and its distance is within 0.5 % of the great-circle distance (largest error {err:.3%})', 0 < err < 0.005)

# ---- the known limit D21 states: the antimeridian
c = fresh(); fj = named(c, 'place', 'Taveuni', lat=-16.8, lon=179.999)
P = dict(lat=-16.8, lon=-179.999, m_per_deg_lon=111320 * math.cos(math.radians(-16.8)), radius_m=1000)
d = hav(-16.8, 179.999, -16.8, -179.999)
S.K(f'as D21 states: a place {d:.0f} m away across ±180° is not found', d < 1000 and len(ST) == 4 and run_block(c, ST[3], P)[0] == [])
S.done()
