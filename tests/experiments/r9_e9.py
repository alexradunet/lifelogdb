"""E9 the CSV 'ability to leave' dump, one file per base table, produced by the sqlite3 CLI from a snapshot.
Declared expectations (before running):
 a) default -csv cannot tell NULL from '' (both empty); I do not know whether -nullvalue applies in csv mode.
 b) REAL values are printed with at most 15 significant digits by the CLI -> 0.30000000000000004 is written as 0.3 (lossy) — unless 3.53.4 changed this.
 c) newlines, quotes, commas, CRLF, emoji and leading/trailing spaces in text survive when read back with a real CSV parser.
 d) every base table appears, FTS shadow tables are not dumped, and record counts equal row counts.
"""
import subprocess, sqlite3, csv, io, os, sys, shutil, time, re
D = '/home/alex/.cache/lifelog-r9/e9'; shutil.rmtree(D, ignore_errors=True); os.makedirs(D)
res = []
def check(name, cond, detail=''):
    res.append(bool(cond)); print(('PASS ' if cond else 'FAIL ') + name + (f' — {detail}' if detail else ''), flush=True)
def cli(db, *args):
    r = subprocess.run(['sqlite3', '-readonly', *args[:-1], db, args[-1]], capture_output=True); return r.stdout.decode('utf-8'), r.stderr.decode(), r.returncode
src = f'{D}/life.db'; shutil.copy('/home/alex/.cache/lifelog-r9/y5.db', src)
c = sqlite3.connect(src, isolation_level=None); c.execute('PRAGMA foreign_keys=ON')
# awkward rows
c.execute('BEGIN IMMEDIATE')
pid = c.execute("select id from pages limit 1").fetchone()[0]
c.execute("INSERT INTO entities(type,created_at,updated_at) VALUES('person','2026-09-30T00:00:00.000Z','2026-09-30T00:00:00.000Z')"); a = c.execute('select last_insert_rowid()').fetchone()[0]
c.execute("INSERT INTO people(id,name,nickname,notes) VALUES(?, 'Null Person', NULL, NULL)", (a,))
c.execute("INSERT INTO entities(type,created_at,updated_at) VALUES('person','2026-09-30T00:00:00.000Z','2026-09-30T00:00:00.000Z')"); b = c.execute('select last_insert_rowid()').fetchone()[0]
c.execute("INSERT INTO people(id,name,nickname,notes) VALUES(?, 'Empty Person', '', '')", (b,))
weird = 'line1\nline2, with "quotes", a\r\nCRLF, emoji 😀, 日本語, leading  and trailing  ,\\N, NULL'
c.execute("INSERT INTO entities(type,created_at,updated_at) VALUES('page','2026-09-30T00:00:00.000Z','2026-09-30T00:00:00.000Z')"); w = c.execute('select last_insert_rowid()').fetchone()[0]
c.execute("INSERT INTO pages(id,kind,day,body) VALUES(?, 'memo','2026-09-30', ?)", (w, weird))
mid = c.execute("select id from metrics where name='weight'").fetchone()[0]
floats = [0.30000000000000004, 1e-7, 123456789.123456789, 1.0, 0.1, 5e-324, 1.7976931348623157e308]
for i, f in enumerate(floats):
    c.execute("INSERT INTO measurements(metric_id,day,value,recorded_at,source,import_id) VALUES(?, '2026-09-30', ?, '2026-09-30T00:00:00.000Z', 'csvtest', ?)", (mid, f, str(i)))
c.execute('COMMIT'); c.execute('PRAGMA wal_checkpoint(TRUNCATE)'); c.close()
snap = f'{D}/snap.db'; subprocess.run(['sqlite3', '-readonly', src, f"VACUUM INTO '{snap}'"], check=True)
tables = [r for r in cli(snap, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'pages_fts%' ORDER BY name")[0].split()]
allt = cli(snap, "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")[0].split()
print('   base tables:', len(tables), tables); print('   excluded:', [t for t in allt if t not in tables])
t0 = time.time(); sizes = {}
for t in tables:
    out = cli(snap, '-header', '-csv', f'SELECT * FROM "{t}"')[0]; open(f'{D}/{t}.csv', 'w', newline='', encoding='utf-8').write(out); sizes[t] = len(out)
print(f'   dump of {len(tables)} tables: {time.time()-t0:.2f}s, {sum(sizes.values())//1024} KiB')
def records(t): return list(csv.reader(io.StringIO(open(f'{D}/{t}.csv', newline='', encoding='utf-8').read(), newline='')))
mism = [(t, len(records(t)) - 1, int(cli(snap, f'SELECT count(*) FROM "{t}"')[0])) for t in tables if len(records(t)) - 1 != int(cli(snap, f'SELECT count(*) FROM "{t}"')[0])]
rows_ok = not mism; print('   count mismatches:', mism)
check('E9d every base table dumped, FTS shadow tables excluded, record counts == row counts', rows_ok and not any(t.startswith('pages_fts') for t in tables))
# a) NULL vs ''
hdr = records('people')[0]; ni = hdr.index('nickname'); pr = {r[hdr.index('name')]: r for r in records('people')[1:]}
check("E9a default -csv writes NULL and '' identically (both empty)", pr['Null Person'][ni] == '' and pr['Empty Person'][ni] == '', f"NULL->{pr['Null Person'][ni]!r} ''->{pr['Empty Person'][ni]!r}")
out = cli(snap, '-header', '-csv', '-nullvalue', r'\N', "SELECT name, nickname, notes FROM people WHERE name IN ('Null Person','Empty Person') ORDER BY name")[0]
check("E9a' -nullvalue applies in csv mode: NULL is written \\N, '' stays empty", out.split('\r\n')[1:3] == [r'Empty Person,,', r'Null Person,\N,\N'], repr(out))
# b) REAL precision
out = cli(snap, '-csv', "SELECT import_id, value FROM measurements WHERE source='csvtest' ORDER BY CAST(import_id AS INTEGER)")[0].split()
got = [float(l.split(',')[1]) for l in out]
lossy = [(f, g) for f, g in zip(floats, got) if f != g]
print('   REAL as written by the CLI:', [l.split(',')[1] for l in out])
check('E9b REAL values round-trip exactly through the CLI text', not lossy, f'lossy: {lossy}')
out2 = cli(snap, '-csv', "SELECT import_id, printf('%!.17g', value) FROM measurements WHERE source='csvtest' ORDER BY CAST(import_id AS INTEGER)")[0].split()
got2 = [float(l.split(',')[1]) for l in out2]
check("E9b' printf('%!.17g', value) does round-trip exactly", got2 == floats, str([l.split(',')[1] for l in out2]))
# c) awkward text
recs = records('pages'); bi = recs[0].index('body')
back = [r[bi] for r in recs[1:] if r[bi].startswith('line1')]
check('E9c newline, quotes, commas, CRLF, emoji, CJK, backslash-N and "NULL" inside text survive a real CSV parser', back == [weird], repr(back)[:120])
print(f'{sum(res)}/{len(res)}')
