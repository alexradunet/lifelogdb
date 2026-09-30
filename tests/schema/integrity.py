"""The four integrity checks of SCHEMA.md §2.5, taken literally from the document and run on the LIVE file: a clean file,
a zeroed page, a truncated file, a flipped index entry, a flipped value (undetected, as the text says), a balance written
with foreign_keys=OFF, an entities row with no domain row, a person with a page but no people row, a drifted FTS index."""
import os, re, shutil, subprocess, sys, tempfile
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('integrity')

blk = sql_blocks(section('### 2.5 ', '### 2.6 '))
sts = [re.sub(r'\s*--.*$', '', l).strip() for l in (blk[0].splitlines() if blk else [])]
sts = [x for x in sts if x]
S.K('§2.5 has exactly the four checks: integrity_check, foreign_key_check, the orphan query, the FTS5 integrity-check',
    len(sts) == 4 and sts[0].lower().startswith('pragma integrity_check') and sts[1].lower().startswith('pragma foreign_key_check')
    and sts[2].upper().startswith('SELECT ID FROM ENTITIES') and "'integrity-check', 1" in sts[3], sts)
if len(sts) != 4: S.done()
ORPHAN = sts[2].rstrip(';')

D = tempfile.mkdtemp(prefix='integrity-'); base = f'{D}/base.db'
c = fresh(base); c.execute('BEGIN IMMEDIATE')
for i in range(3000):                        # enough pages that the table and pages_title span many pages
    if i % 2: page(c, f'Title {i:05d}', day='2026-09-30', body=f'BODYMARK{i:05d} ' + 'lorem ipsum ' * 20)
    else: memo(c, f'BODYMARK{i:05d} ' + 'dolor sit amet ' * 20)
for t in ('event', 'task', 'person', 'place', 'holding'): thing(c, t)      # one row of every domain type, so a query that
c.execute('COMMIT'); c.execute('PRAGMA wal_checkpoint(TRUNCATE)'); c.close()  # forgets one table reports a false orphan
PS = sqlite3.connect(base).execute('pragma page_size').fetchone()[0]
def sq(p, sql):
    r = subprocess.run(['sqlite3', '-readonly', p, sql], capture_output=True, text=True, stdin=subprocess.DEVNULL); return (r.stdout + r.stderr).strip()
def checks(p): return sq(p, sts[0]).splitlines()[:1], sq(p, sts[1]), sq(p, ORPHAN)
def cp(name): p = f'{D}/{name}.db'; shutil.copy(base, p); return p
def flip(p, j, off=0):
    with open(p, 'r+b') as f: f.seek(j + off); ch = f.read(1)[0]; f.seek(j + off); f.write(bytes([ch ^ 1]))
ic, fk, orph = checks(base)
S.K('a clean file with a row of every type: integrity ok, foreign_key_check empty, orphan query empty', ic == ['ok'] and fk == '' and orph == '', (ic, fk, orph))
p = cp('zero'); root = int(sq(p, "SELECT rootpage FROM sqlite_master WHERE name='pages'"))
with open(p, 'r+b') as f: f.seek((root - 1) * PS); f.write(b'\0' * PS)
S.K('a zeroed table page: integrity_check is not ok', checks(p)[0] != ['ok'])
p = cp('trunc')
with open(p, 'r+b') as f: f.truncate(os.path.getsize(p) - 3 * PS)
S.K('a file truncated by three pages: integrity_check is not ok', checks(p)[0] != ['ok'])
p = cp('idx'); data = open(p, 'rb').read()
j = next((m.start() for m in re.finditer(rb'title 012\d\d', data) if re.fullmatch(rb'Title \d{5}', data[m.start() - 11:m.start()]) is None), None)
if j is not None: flip(p, j, 6)
S.K('a flipped byte in a title_key inside the pages_title index: integrity_check is not ok', j is not None and checks(p)[0] != ['ok'])
p = cp('val'); j = open(p, 'rb').read().find(b'lorem ipsum lorem')
if j >= 0: flip(p, j)
n = sq(p, "SELECT count(*) FROM pages WHERE body LIKE '%korem ipsum lorem%' OR body LIKE '%morem ipsum lorem%'")
S.K('a flipped byte inside a body: the text changed and integrity_check is STILL ok', j >= 0 and n == '1' and checks(p)[0] == ['ok'], (j, n))
p = cp('fk'); c = sqlite3.connect(p, isolation_level=None); c.execute('PRAGMA foreign_keys=OFF')
ins = tryx(c, "INSERT INTO balances(holding_id,day,amount,recorded_at,source) VALUES (99999,'2026-09-30',100,'2026-09-30T10:00:00.000Z','ui')"); c.close()
ic, fk, orph = checks(p)
S.K('a balance for no holding, written with foreign_keys=OFF (STRICT does not enforce FKs): only foreign_key_check sees it', ins == 'OK' and ic == ['ok'] and fk != '' and orph == '', (ins, ic, fk, orph))
p = cp('orph'); c = sqlite3.connect(p, isolation_level=None); oid = ent(c, 'page'); c.close()
S.K('an entities row with no domain row: only the orphan query sees it', checks(p) == (['ok'], '', str(oid)), (checks(p), oid))
p = cp('half'); c = sqlite3.connect(p, isolation_level=None); c.execute('PRAGMA foreign_keys=ON'); hid = ent(c, 'person')
c.execute("INSERT INTO pages(id,entity_type,kind,title,title_key) VALUES (?, 'person', 'page', 'Half person', 'half person')", (hid,)); c.close()
S.K('a person with a page but no people row: only the orphan query sees it', checks(p) == (['ok'], '', str(hid)), (checks(p), hid))
shutil.rmtree(D, ignore_errors=True)

# ---- the fourth check: the FTS index against pages (it writes, so a writer connection)
c = fresh(); memo(c, 'alpha beta'); page(c, 'Gamma')
S.K('on a clean file every statement of §2.5 runs without error', all(tryx(c, s) == 'OK' for s in sts))
c.execute("INSERT INTO pages_fts(rowid, title, body) VALUES (999, 'ghost', 'drifted')")
S.K('a drifted FTS index: PRAGMA integrity_check still says ok', c.execute('PRAGMA integrity_check').fetchall() == [('ok',)])
S.K('...and the FTS5 integrity-check of §2.5 fails', tryx(c, sts[3]).startswith('ERR'))
c.execute("INSERT INTO pages_fts(pages_fts) VALUES('rebuild')")
S.K("...until 'rebuild' repairs it", tryx(c, sts[3]) == 'OK')
S.done()
