"""E3 snapshot preserves identity; E4 fk vs integrity; E5 what verification detects; E6 stale WAL on restore.
Expectations (declared before the run):
E3  .backup and VACUUM INTO both keep application_id (0x4C494645), user_version, the 76 schema objects, FTS5 working.
    I do NOT know whether VACUUM INTO keeps WAL mode -> recorded, whichever way.
E4  integrity_check says 'ok' for a snapshot containing an FK violation; foreign_key_check reports it.
E5  (a) zeroed structure page: integrity_check fails or open fails; (b) one flipped byte inside a text cell in the table: integrity_check 'ok'
    (SQLite has no page checksums) but sha256 differs; (c) same flip in an index key: detected; (d) truncation: detected.
E6  A same-lineage snapshot restored over a db that still has its old -wal/-shm: the WAL is replayed, the restore is silently undone (count == S2).
    B: -shm removed, -wal kept: same.  C: both removed: count == S1 (the snapshot).  D: an OLDER snapshot with the newer -wal: result is not the snapshot
    (count != S0) or integrity fails.
"""
import subprocess, sqlite3, shutil, os, sys, hashlib, time
sys.path.insert(0, '.')
import mkdb
D = '/home/alex/.cache/lifelog-r9/e3456'; shutil.rmtree(D, ignore_errors=True); os.makedirs(D)
res = []
def check(name, cond, detail=''):
    res.append(bool(cond)); print(('PASS ' if cond else 'FAIL ') + name + (f' — {detail}' if detail else ''))
def cli(db, sql, ro=True):
    a = ['sqlite3'] + (['-readonly'] if ro else []) + [db, sql]
    r = subprocess.run(a, capture_output=True, text=True); return r.stdout.strip(), r.stderr.strip(), r.returncode
def sha(p): return hashlib.sha256(open(p, 'rb').read()).hexdigest()
src = f'{D}/life.db'; shutil.copy('r9/y1.db', src)
for e in ('-wal', '-shm'):
    if os.path.exists('r9/y1.db' + e): os.remove(src + e) if os.path.exists(src + e) else None
c = sqlite3.connect(src); c.execute('PRAGMA wal_checkpoint(TRUNCATE)'); c.close()

# E3
t0 = time.time(); subprocess.run(['sqlite3', src, f'.backup {D}/b.db'], check=True); tb = time.time() - t0
t0 = time.time(); subprocess.run(['sqlite3', src, f"VACUUM INTO '{D}/v.db'"], check=True); tv = time.time() - t0
for label, p in (('.backup', f'{D}/b.db'), ('VACUUM INTO', f'{D}/v.db')):
    aid = cli(p, 'pragma application_id')[0]; uv = cli(p, 'pragma user_version')[0]; jm = cli(p, 'pragma journal_mode')[0]
    n = cli(p, 'select count(*) from sqlite_master')[0]; hdr = open(p, 'rb').read(20)[18:20]
    fts = cli(p, "select count(*) from pages_fts where pages_fts match 'coffee'")[0]
    check(f'E3 {label}: application_id/user_version/objects kept', aid == '1279870533' and uv == '1' and n == '76', f'aid={aid} uv={uv} objects={n}')
    check(f'E3 {label}: FTS5 search works in the copy', int(fts) > 0, f'hits={fts}')
    print(f'   info {label}: journal_mode={jm} header(rd,wr version)={list(hdr)} size={os.path.getsize(p)//1024} KiB')
print(f'   timings on {os.path.getsize(src)//1024} KiB: .backup {tb:.2f}s, VACUUM INTO {tv:.2f}s')

# E4
shutil.copy(f'{D}/b.db', f'{D}/fk.db')
w = sqlite3.connect(f'{D}/fk.db'); w.execute('PRAGMA foreign_keys=OFF')
try:
    w.execute("INSERT INTO balances(account_id,day,amount,recorded_at,source) VALUES(999999,'2026-09-30',1,'2026-09-30T00:00:00.000Z','manual')"); w.commit(); inserted = True
except sqlite3.Error as e: inserted = False; print('   insert blocked:', e)
w.close()
ic = cli(f'{D}/fk.db', 'pragma integrity_check')[0]; fk = cli(f'{D}/fk.db', 'pragma foreign_key_check')[0]
check('E4 a writer that forgot foreign_keys=ON can store an orphan balance', inserted)
check('E4 integrity_check does NOT see it; foreign_key_check does', ic == 'ok' and fk.startswith('balances|'), f'integrity={ic!r} fk={fk!r}')

# E5
base = open(f'{D}/b.db', 'rb').read(); psz = int(cli(f'{D}/b.db', 'pragma page_size')[0]); pc = int(cli(f'{D}/b.db', 'pragma page_count')[0])
def variant(name, data):
    p = f'{D}/{name}.db'; open(p, 'wb').write(data); return p
# (a) zero a structure page: page 3 of the file (an interior/table root page area)
pa = variant('zero', base[:2 * psz] + b'\x00' * psz + base[3 * psz:])
out, err, rc = cli(pa, 'pragma integrity_check')
check('E5a a zeroed page is detected by integrity_check (or the open fails)', out != 'ok', (out or err)[:80].replace('\n', ' | '))
# (b) flip a byte inside a text cell in the table: 'Topic 12' -> 'Topic 13' (first occurrence)
i = base.find(b'Topic 12'); assert i > 0
flipped = bytearray(base); flipped[i + 7] = ord('3'); pb = variant('flip_text', bytes(flipped))
out = cli(pb, 'pragma integrity_check')[0]
def hp(p): return hashlib.sha256(subprocess.run(['sqlite3', '-readonly', '-csv', p, 'select * from pages order by id'], capture_output=True, stdin=subprocess.DEVNULL).stdout).hexdigest()
h_orig, h_flip = hp(f'{D}/b.db'), hp(pb)
check('E5b a flipped byte in a text cell is NOT detected (integrity ok, yet the content of `pages` differs)', out == 'ok' and h_orig != h_flip, f'integrity={out!r}; pages table hash differs={h_orig != h_flip}')
check('E5b ...but sha256 of the file differs', sha(pb) != sha(f'{D}/b.db'))
# (c) same flip in the index key copy 'topic 12' (lowercase, in pages_title index / title_key)
j = base.find(b'topic 12'); assert j > 0
fl2 = bytearray(base); fl2[j + 7] = ord('3'); pc_ = variant('flip_key', bytes(fl2))
out = cli(pc_, 'pragma integrity_check')[0]
check('E5c a flipped byte in an indexed key (title_key) is detected', out != 'ok', out[:90].replace('\n', ' | '))
# (d) truncation
pd_ = variant('trunc', base[:len(base) // 2]); out, err, rc = cli(pd_, 'pragma integrity_check')
check('E5d a truncated file is detected', out != 'ok' or rc != 0, (out or err)[:90].replace('\n', ' | '))

# E6 stale WAL on restore
def make_crash_image(tag, s0_then_checkpoint):
    """returns (dir, n_s0, n_s1, n_s2). life.db@S1 + -wal/-shm holding S1->S2 frames, as a crashed app would leave them."""
    d = f'{D}/{tag}'; os.makedirs(d); live = f'{d}/life.db'; shutil.copy(f'{D}/b.db', live)
    w = sqlite3.connect(live, isolation_level=None); w.execute('PRAGMA wal_autocheckpoint=0'); w.execute('PRAGMA foreign_keys=ON')
    n0 = w.execute("select count(*) from pages").fetchone()[0]
    def more(k, label):
        w.execute('BEGIN IMMEDIATE')
        for _ in range(k):
            w.execute("INSERT INTO entities(type,created_at,updated_at) VALUES('page','2026-09-30T00:00:00.000Z','2026-09-30T00:00:00.000Z')")
            w.execute("INSERT INTO pages(id,kind,day,body) VALUES(last_insert_rowid(),'memo','2026-09-30',?)", (label + ' ' + 'y' * 500,))
        w.execute('COMMIT')
    snap0 = f'{d}/snap0.db'; subprocess.run(['sqlite3', live, f'.backup {snap0}'], check=True)       # S0
    more(1500, 'phase1'); n1 = w.execute('select count(*) from pages').fetchone()[0]
    if s0_then_checkpoint: w.execute('PRAGMA wal_checkpoint(TRUNCATE)')                               # main file now at S1
    snap1 = f'{d}/snap1.db'; subprocess.run(['sqlite3', live, f'.backup {snap1}'], check=True)       # S1 snapshot
    more(800, 'phase2'); n2 = w.execute('select count(*) from pages').fetchone()[0]                    # S2 lives in the WAL
    img = f'{d}/crash'; os.makedirs(img)
    for e in ('', '-wal', '-shm'): shutil.copy(live + e, f'{img}/life.db{e}')                          # the crash image
    w.close(); return d, img, n0, n1, n2
def count(p):
    out, err, rc = cli(p, 'select count(*) from pages', ro=False); ic = cli(p, 'pragma integrity_check', ro=False)[0]
    return out, ic[:70].replace('\n', ' | ')
d, img, n0, n1, n2 = make_crash_image('A', True)
print(f'   states: S0 pages={n0}, S1={n1}, S2={n2}')
for variant_, rm in (('A stale -wal and -shm kept', []), ('B -shm removed, -wal kept', ['-shm']), ('C both removed', ['-wal', '-shm'])):
    t = f'{d}/try_{variant_[0]}'; shutil.copytree(img, t)
    for e in rm: os.remove(f'{t}/life.db{e}')
    shutil.copy(f'{d}/snap1.db', f'{t}/life.db')            # the naive restore: cp snapshot over life.db
    cnt, ic = count(f'{t}/life.db')
    want = str(n1) if variant_[0] == 'C' else str(n2)
    check(f'E6{variant_[0]} restore of the S1 snapshot, {variant_.split(" ",1)[1]}: pages={cnt} (snapshot has {n1}, stale WAL would show {n2})',
          cnt == want, f'integrity={ic}')
# D: older snapshot (S0) over a db whose main file moved on to S1, with WAL frames S1->S2 kept
d, img, n0, n1, n2 = make_crash_image('D', True)
t = f'{d}/try_D'; shutil.copytree(img, t); shutil.copy(f'{d}/snap0.db', f'{t}/life.db')
cnt, ic = count(f'{t}/life.db')
check(f'E6D restore of the OLDER snapshot (S0={n0} pages) next to a newer -wal: result is not the snapshot or is damaged', cnt != str(n0) or ic != 'ok', f'pages={cnt} integrity={ic}')
print(f'{sum(res)}/{len(res)}')
