"""The import path of SCHEMA.md §2.8, run from the document's own SQL on 1 000 CSV rows: idempotent, all-or-nothing,
and the three traps (WHERE true, the partial index's WHERE, CAST) are real."""
import csv, os, re, shutil, subprocess, sys, tempfile
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('imports')

sec = section('### 2.8 ', '## 3. ')
IMP = [b for b in sql_blocks(sec) if "ATTACH 'scratch.db' AS s;" in b]
S.K('§2.8 has the import block', len(IMP) == 1)
if not IMP: S.done()
IMP = IMP[0]
SRC = re.search(r"'(import:[a-z0-9_:.-]+)'", IMP)
S.K('the import block names its importer as an import:<name> source', SRC is not None)
SRC = SRC.group(1) if SRC else 'import:scale'
work = tempfile.mkdtemp(prefix='lifelog-import-')
def life(path):
    c = fresh(path); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); return c
def stage(rows):
    with open(f'{work}/weights.csv', 'w', newline='') as f:
        w = csv.writer(f); w.writerow(('id', 'day', 'taken_at', 'tz', 'value')); w.writerows(rows)
    if os.path.exists(f'{work}/scratch.db'): os.remove(f'{work}/scratch.db')
    subprocess.run(['sqlite3', f'{work}/scratch.db', '.import --csv weights.csv staging'], cwd=work, check=True, stdin=subprocess.DEVNULL)
def run(c, sql=None):
    cwd = os.getcwd(); os.chdir(work)
    try: c.executescript(sql or IMP); return 'OK'
    except sqlite3.Error as e:
        for st in ('ROLLBACK', 'DETACH s'):
            try: c.execute(st)
            except sqlite3.Error: pass
        return 'ERR ' + str(e)
    finally: os.chdir(cwd)
def n(c): return c.execute('select count(*) from measurements where source=?', (SRC,)).fetchone()[0]
rows = [(f'w{i}', f'2025-{1 + i % 12:02d}-{1 + i % 28:02d}', '' if i % 5 == 0 else f'2025-{1 + i % 12:02d}-{1 + i % 28:02d}T07:30:00.000Z',
         '' if i % 5 == 0 else 'Europe/Berlin', f'{60 + (i % 50) / 10}') for i in range(1000)]
c = life(f'{work}/life.db'); stage(rows)
r = run(c); S.K('the document\'s block loads 1 000 rows', r == 'OK' and n(c) == 1000, (r, n(c)))
S.K('empty CSV cells became NULL through NULLIF (200 rows without taken_at and tz)', c.execute('select count(*) from measurements where source=? and taken_at is null and tz is null', (SRC,)).fetchone()[0] == 200)
S.K('running it again inserts nothing', run(c) == 'OK' and n(c) == 1000)
S.K('the values are REAL, converted by the STRICT column', c.execute("select count(*) from measurements where source=? and typeof(value)='real'", (SRC,)).fetchone()[0] == 1000)
stage(rows[:10] + [(f'new{i}', '2026-01-02', '', '', '70.5') for i in range(10)])
S.K('10 duplicate keys and 10 new rows: exactly the 10 new ones are inserted', run(c) == 'OK' and n(c) == 1010, n(c))
before = n(c)
for label, bad in (("a value 'abc'", ('bad', '2026-02-01', '', '', 'abc')), ('an empty value cell (not stored as 0.0)', ('empty', '2026-02-01', '', '', '')),
                   ('a malformed day (not swallowed by DO NOTHING)', ('day', '2026-2-1', '', '', '71'))):
    stage([(f'b{i}{label[:3]}', '2026-02-01', '', '', '71') for i in range(5)] + [bad]); r = run(c)
    S.K(f'{label} in the batch: the whole batch is rejected, nothing of it inserted', r.startswith('ERR') and n(c) == before, (r[:80], n(c)))
stage([('t1', '2026-03-01', '', '', '70')])
S.K("without NULLIF an empty taken_at ('' is not NULL) fails its CHECK", 'CHECK' in run(c, IMP.replace("NULLIF(taken_at, '')", 'taken_at')))
S.K('without WHERE true the statement is rejected (the ON is read as a join\'s)', re.search(r'JOIN|syntax', run(c, IMP.replace('  FROM s.staging WHERE true', '  FROM s.staging'))) is not None)
S.K('a conflict target without the index\'s WHERE does not match the partial unique index', 'does not match' in run(c, IMP.replace(' WHERE import_key IS NOT NULL DO NOTHING', ' DO NOTHING')))
S.K("CAST hides garbage: 'abc' -> 0.0, '' -> 0.0, '12.5kg' -> 12.5", c.execute("select cast('abc' as real), cast('' as real), cast('12.5kg' as real)").fetchone() == (0.0, 0.0, 12.5))
stage([('cast1', '2026-03-05', '', '', 'abc')])
S.K("with CAST the bad value 'abc' would be stored as 0.0 (the trap, shown)", run(c, IMP.replace("NULLIF(tz, ''), value,", "NULLIF(tz, ''), CAST(value AS REAL),")) == 'OK'
    and c.execute("select value from measurements where import_key='cast1'").fetchone() == (0.0,))
orphan = sql_blocks(section('### 2.5 ', '### 2.6 '))[0].splitlines()[2].split(';')[0]
S.K('afterwards: integrity_check ok, foreign_key_check empty, no orphan entities row', integrity_ok(c) and c.execute(orphan).fetchall() == [])
pc = c.execute('SELECT source, count(*), min(day), max(day) FROM measurements GROUP BY source').fetchall()
S.K('the per-source count of §2.8 answers', any(r[0] == SRC and r[1] >= 1010 for r in pc), pc)
shutil.rmtree(work, ignore_errors=True)
S.done()
