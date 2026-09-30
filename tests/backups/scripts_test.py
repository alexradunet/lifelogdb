"""Tests of nightly.sh / restore.sh (from drafts, or extracted from the doc with --doc). Expectations are declared in each label.
Usage: r9_scripts.py [--doc] [--mutate NAME]"""
import subprocess, sqlite3, shutil, os, sys, time, re, hashlib, datetime as dt, random
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, '..', 'lib')); sys.path.insert(0, HERE)
import docsql as extract
import tempfile, atexit
import mkdb
R = tempfile.mkdtemp(prefix='lifelog-backup-tests-', dir=os.environ.get('LIFELOG_TEST_DIR') or os.path.expanduser('~/.cache') if os.path.isdir(os.path.expanduser('~/.cache')) else None)   # a real disk, not tmpfs
atexit.register(lambda: shutil.rmtree(R, ignore_errors=True) if not os.environ.get('KEEP') else print('kept', R))
BASE = f'{R}/y5.db'; _c = mkdb.populate(BASE, 5); _c.execute('PRAGMA wal_checkpoint(TRUNCATE)'); _c.close()   # ~50 MB, five years of synthetic logging
MUT = sys.argv[sys.argv.index('--mutate') + 1] if '--mutate' in sys.argv else None
text = extract.doc_text()
sec = extract.section(text, r'^### 2\.8 ', r'^### 2\.9 ')
blocks = re.findall(r'```sh\n(.*?)\n```', sec, re.S)
NIGHTLY = [b for b in blocks if b.startswith('#!/bin/sh\n# nightly.sh')][0] + '\n'; RESTORE = [b for b in blocks if b.startswith('#!/bin/sh\n# restore.sh')][0] + '\n'
USE_DOC = True
MUTANTS = {   # (script, old, new)
 'backup_instead_of_vacuum': ('n', 'timeout 900 sqlite3 -readonly life.db "VACUUM INTO \'$snap.tmp\'"', 'timeout 20 sqlite3 -readonly life.db ".backup \'$snap.tmp\'"'),
 'no_orphan_check': ('n', "[ -z \"$(sqlite3 -readonly \"$snap.tmp\" 'SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages UNION SELECT id FROM events UNION SELECT id FROM tasks UNION SELECT id FROM people UNION SELECT id FROM places UNION SELECT id FROM accounts)')\" ]\n", ''),
 'no_fk_check':   ('n', '[ -z "$(sqlite3 -readonly "$snap.tmp" \'PRAGMA foreign_key_check\')" ]\n', ''),
 'no_integrity':  ('n', '[ "$(sqlite3 -readonly "$snap.tmp" \'PRAGMA integrity_check\')" = ok ]\n', ''),
 'verify_after_rename': ('n', None, None),
 'no_tmp_name':   ('n', "VACUUM INTO '$snap.tmp'", "VACUUM INTO '$snap'"),
 'prune_keeps_15': ('n', 'NR - 30', 'NR - 15'),
 'prune_no_monthly': ('n', ' && !first[i]', ''),
 'offbox_optional': ('n', 'sh -c "${OFFBOX_CMD:?set OFFBOX_CMD to the command that copies backups/ and dump/ off this machine}"', 'sh -c "${OFFBOX_CMD:-true}"'),
 'no_sha_check':  ('r', '(cd "$(dirname "$snap")" && sha256sum -c "$(basename "$snap").sha256")\n', ''),
 'no_wal_removal': ('r', 'for e in "" -wal -shm; do', 'for e in ""; do'),
 'no_wal_mode':   ('r', "[ \"$(sqlite3 \"$dir/life.db\" 'PRAGMA journal_mode=WAL')\" = wal ]\n", ''),
 'restore_no_fk': ('r', '[ -z "$(sqlite3 -readonly "$snap" \'PRAGMA foreign_key_check\')" ]\n', ''),
 'restore_delete_old': ('r', 'mv "$dir/life.db$e" "$dir/life.db.broken-$ts$e"', 'rm -f "$dir/life.db$e"'),
}
if MUT:
    k, old, new = MUTANTS[MUT]
    if MUT == 'verify_after_rename':      # verification after the rename instead of before: a bad copy keeps its real name
        a = NIGHTLY.index('# 2. Verify'); b = NIGHTLY.index('mv "$snap.tmp" "$snap"')
        block = NIGHTLY[a:b]; NIGHTLY = NIGHTLY[:a] + 'mv "$snap.tmp" "$snap"\n' + block.replace('"$snap.tmp"', '"$snap"') + NIGHTLY[b + len('mv "$snap.tmp" "$snap"\n'):]
    else:
        assert old in (NIGHTLY if k == 'n' else RESTORE), MUT
        if k == 'n': NIGHTLY = NIGHTLY.replace(old, new)
        else: RESTORE = RESTORE.replace(old, new)
res = []
def check(name, cond, detail=''):
    res.append(bool(cond)); print(('PASS ' if cond else 'FAIL ') + name + (f' — {detail}' if detail else ''), flush=True)
def sh(cmd, cwd, env=None, timeout=120):
    e = dict(os.environ); e.update(env or {})
    try:
        r = subprocess.run(['sh', '-c', cmd], cwd=cwd, env=e, capture_output=True, text=True, timeout=timeout, stdin=subprocess.DEVNULL); return r.returncode, (r.stdout + r.stderr).strip()
    except subprocess.TimeoutExpired: return 124, 'timeout'
def sq(db, sql, ro=True):
    r = subprocess.run(['sqlite3'] + (['-readonly'] if ro else []) + [db, sql], capture_output=True, stdin=subprocess.DEVNULL); return r.stdout.decode().strip()
def sandbox(name):
    d = f'{R}/s_{name}'; shutil.rmtree(d, ignore_errors=True); os.makedirs(f'{d}/life'); os.makedirs(f'{d}/offbox')
    open(f'{d}/life/nightly.sh', 'w').write(NIGHTLY); open(f'{d}/life/restore.sh', 'w').write(RESTORE)
    shutil.copy(BASE, f'{d}/life/life.db'); return d, f'{d}/life'
today = dt.date.today().strftime('%Y%m%d'); OFF = 'rsync -a backups dump ../offbox/'
def pairs_ok(db):
    c = sqlite3.connect(f'file:{db}?mode=ro', uri=True); p = c.execute("select count(*) from pages where body like 'live %'").fetchone()[0]; m = c.execute("select count(*) from measurements where source='live'").fetchone()[0]; c.close(); return p == m, p

# S1 happy path, with a writer hammering the live file
d, L = sandbox('s1')
w = subprocess.Popen([sys.executable, os.path.join(HERE, 'writer.py'), f'{L}/life.db', '1000', '0.02'], stdout=subprocess.PIPE, text=True); time.sleep(0.8)
t0 = time.time(); rc, out = sh('sh nightly.sh', L, {'OFFBOX_CMD': OFF}, timeout=90); took = time.time() - t0
open(f'{L}/life.db.stop', 'w').close(); nwr = int(w.communicate()[0])
check(f'S1 nightly.sh finishes (rc 0) on a 50 MB file while a writer commits ~45/s', rc == 0, f'rc={rc} in {took:.1f}s, writer did {nwr} commits; {out[:150]}')
snap = f'{L}/backups/life-{today}.db'
check('S1 snapshot + .sha256 exist; sha256sum -c passes', os.path.exists(snap) and sh(f'sha256sum -c life-{today}.db.sha256', f'{L}/backups')[0] == 0)
check('S1 the snapshot is consistent (pages == measurements of the live writer), integrity ok, fk clean, app id LIFE',
      os.path.exists(snap) and pairs_ok(snap)[0] and sq(snap, 'pragma integrity_check') == 'ok' and sq(snap, 'pragma foreign_key_check') == '' and sq(snap, 'pragma application_id') == '1279870533', str(pairs_ok(snap)) if os.path.exists(snap) else '')
csvs = sorted(os.listdir(f'{L}/dump')) if os.path.isdir(f'{L}/dump') else []
check('S1 dump/ has 15 CSVs, none for the FTS tables, no dump.tmp left', len(csvs) == 15 and not any('fts' in c for c in csvs) and not os.path.exists(f'{L}/dump.tmp'), f'{len(csvs)} files')
check('S1 off-box copy holds the snapshot, its hash and the dump', os.path.exists(f'{d}/offbox/backups/life-{today}.db') and os.path.exists(f'{d}/offbox/dump/pages.csv'))
check('S1 no .tmp file left', not [f for f in os.listdir(f'{L}/backups') if f.endswith('.tmp')])
# CSV record count == row count for a non-empty table, from the same snapshot
import csv, io
recs = list(csv.reader(io.StringIO(open(f'{L}/dump/pages.csv', newline='', encoding='utf-8').read(), newline='')))
check('S1 dump/pages.csv records == pages rows of the snapshot', len(recs) - 1 == int(sq(snap, 'select count(*) from pages')), f'{len(recs)-1}')

# S2 a night that fails verification changes nothing and alerts
d, L = sandbox('s2'); sh('sh nightly.sh', L, {'OFFBOX_CMD': OFF}); good = sorted(os.listdir(f'{L}/backups')); gd = sorted(os.listdir(f'{L}/dump'))
y = (dt.date.today() - dt.timedelta(days=1)).strftime('%Y%m%d'); shutil.move(f'{L}/backups/life-{today}.db', f'{L}/backups/life-{y}.db'); shutil.move(f'{L}/backups/life-{today}.db.sha256', f'{L}/backups/life-{y}.db.sha256')
before = {f: hashlib.sha256(open(f'{L}/backups/{f}', 'rb').read()).hexdigest() for f in os.listdir(f'{L}/backups')}
dump_before = hashlib.sha256(open(f'{L}/dump/pages.csv', 'rb').read()).hexdigest()
c = sqlite3.connect(f'{L}/life.db'); c.execute('PRAGMA foreign_keys=OFF')
c.execute("INSERT INTO balances(account_id,day,amount,recorded_at,source) VALUES(999999,'2026-09-30',1,'2026-09-30T00:00:00.000Z','manual')"); c.commit(); c.close()
shutil.rmtree(f'{d}/offbox'); os.makedirs(f'{d}/offbox')
rc, out = sh('sh nightly.sh', L, {'OFFBOX_CMD': OFF})
after = {f: hashlib.sha256(open(f'{L}/backups/{f}', 'rb').read()).hexdigest() for f in os.listdir(f'{L}/backups')}
check('S2 an orphan balance in the live file: nightly.sh exits non-zero', rc != 0, f'rc={rc}')
check('S2 ...and leaves no snapshot for that day, no .tmp, earlier snapshot byte-identical', after == before, f'before={sorted(before)} after={sorted(after)}')
check('S2 ...and the previous dump/ is untouched and nothing went off-box', hashlib.sha256(open(f'{L}/dump/pages.csv', 'rb').read()).hexdigest() == dump_before and not os.listdir(f'{d}/offbox'))

# S2b damage that VACUUM INTO copies faithfully (a flipped index key): only integrity_check on the copy stops it
d, L = sandbox('s2b'); b = bytearray(open(f'{L}/life.db', 'rb').read()); k = bytes(b).find(b'topic 12'); b[k + 7] ^= 1; open(f'{L}/life.db', 'wb').write(bytes(b))
live_ic = sq(f'{L}/life.db', 'pragma integrity_check'); rc, out = sh('sh nightly.sh', L, {'OFFBOX_CMD': OFF})
check('S2b a damaged index in the live file: VACUUM INTO copies it, integrity_check of the copy rejects it, nightly.sh exits non-zero, no snapshot',
      live_ic != 'ok' and rc != 0 and not [f for f in os.listdir(f'{L}/backups') if f.startswith('life-')] and not os.path.exists(f'{L}/dump'), f'live: {live_ic[:50]!r} rc={rc}')

# S2c an entities row with no domain row (a writer died between its two inserts): no constraint forbids it, the nightly check does
d, L = sandbox('s2c'); c = sqlite3.connect(f'{L}/life.db'); c.execute("INSERT INTO entities(type,created_at,updated_at) VALUES('page','2026-09-30T00:00:00.000Z','2026-09-30T00:00:00.000Z')"); c.commit(); c.close()
rc, out = sh('sh nightly.sh', L, {'OFFBOX_CMD': OFF})
check('S2c an orphan entities row in the live file: integrity and foreign-key checks pass, the orphan check stops the night, no snapshot',
      rc != 0 and not [f for f in os.listdir(f'{L}/backups') if f.startswith('life-')] and not os.path.exists(f'{L}/dump'), f'rc={rc}')

# S3 off-box command missing or failing: the snapshot is kept, the exit code is non-zero
d, L = sandbox('s3'); rc1, out1 = sh('sh nightly.sh', L, {'OFFBOX_CMD': ''}); 
rc2, out2 = sh('sh nightly.sh', L, {'OFFBOX_CMD': 'false'})
rc3, out3 = sh('env -u OFFBOX_CMD sh nightly.sh', L)
check('S3 OFFBOX_CMD unset/empty/failing → non-zero exit (the job cannot "succeed" without an off-box copy)', rc1 != 0 and rc2 != 0 and rc3 != 0, f'rc={rc1},{rc2},{rc3}; {out3[-110:]}')
check('S3 ...but the verified local snapshot is there', os.path.exists(f'{L}/backups/life-{today}.db'))

# S4 prune vs an independent oracle
d, L = sandbox('s4'); os.makedirs(f'{L}/backups')
days = [dt.date(2023, 1, 1) + dt.timedelta(days=i) for i in range(1200) if (i % 7 != 3 or i > 1150)]   # gaps, like real life
names = [f'life-{x.strftime("%Y%m%d")}.db' for x in days if x.strftime('%Y%m%d') < today]
for n in names:
    open(f'{L}/backups/{n}', 'w').close(); open(f'{L}/backups/{n}.sha256', 'w').close()
open(f'{L}/backups/life-20240101.db.FAILED', 'w').close(); open(f'{L}/backups/notes.txt', 'w').close()
rc, out = sh('sh nightly.sh', L, {'OFFBOX_CMD': 'true'})
allnames = sorted(names + [f'life-{today}.db'])
keep = set(allnames[-30:]); seen = set()
for n in allnames:
    m = n[5:11]
    if m not in seen: keep.add(n); seen.add(m)
kept = {f for f in os.listdir(f'{L}/backups') if re.fullmatch(r'life-\d{8}\.db', f)}
check('S4 prune: kept set == (newest 30) ∪ (first snapshot of each month), independent oracle', rc == 0 and kept == keep, f'rc={rc} kept={len(kept)} oracle={len(keep)} extra={sorted(kept-keep)[:3]} missing={sorted(keep-kept)[:3]}')
check('S4 prune: pruned snapshots lose their .sha256 too; unrelated files untouched',
      all(os.path.exists(f'{L}/backups/{n}.sha256') == (n in keep) for n in names[:400]) and os.path.exists(f'{L}/backups/life-20240101.db.FAILED') and os.path.exists(f'{L}/backups/notes.txt'))

# S5 restore over a crash image with a stale -wal/-shm
d, L = sandbox('s5')
w = sqlite3.connect(f'{L}/life.db', isolation_level=None); w.execute('PRAGMA wal_autocheckpoint=0'); w.execute('PRAGMA foreign_keys=ON')
rc, out = sh('sh nightly.sh', L, {'OFFBOX_CMD': 'true'}); n1 = int(sq(f'{L}/backups/life-{today}.db', 'select count(*) from pages'))
w.execute('BEGIN IMMEDIATE')
for _ in range(700):
    w.execute("INSERT INTO entities(type,created_at,updated_at) VALUES('page','2026-09-30T00:00:00.000Z','2026-09-30T00:00:00.000Z')"); w.execute("INSERT INTO pages(id,kind,day,body) VALUES(last_insert_rowid(),'memo','2026-09-30',?)", ('after ' + 'z' * 400,))
w.execute('COMMIT'); n2 = w.execute('select count(*) from pages').fetchone()[0]
crash = f'{d}/crash'; os.makedirs(crash)
for e in ('', '-wal', '-shm'): shutil.copy(f'{L}/life.db{e}', f'{crash}/life.db{e}')
w.close(); shutil.rmtree(f'{L}/backups/../dump', ignore_errors=True)
for e in ('', '-wal', '-shm'):
    if os.path.exists(f'{L}/life.db{e}'): os.remove(f'{L}/life.db{e}')
for e in ('', '-wal', '-shm'): shutil.copy(f'{crash}/life.db{e}', f'{L}/life.db{e}')
check(f'S5 setup: the crash image really has a -wal and shows {n2} pages while the snapshot has {n1}', os.path.getsize(f'{L}/life.db-wal') > 0 and n2 > n1)
rc, out = sh(f'sh restore.sh backups/life-{today}.db', L)
check('S5 restore.sh exits 0', rc == 0, out[-200:])
check(f'S5 the restored life.db is the snapshot ({n1} pages), in WAL mode, integrity ok', sq(f'{L}/life.db', 'select count(*) from pages', ro=False) == str(n1) and sq(f'{L}/life.db', 'pragma journal_mode', ro=False) == 'wal' and sq(f'{L}/life.db', 'pragma integrity_check', ro=False) == 'ok', sq(f'{L}/life.db', 'select count(*) from pages', ro=False))
broken = [f for f in os.listdir(L) if f.startswith('life.db.broken-')]
bdb = [f for f in broken if not f.endswith(('-wal', '-shm'))]
check('S5 the old file AND its -wal/-shm were kept together as evidence, and still open with the newer data', len(bdb) == 1 and any(f.endswith('-wal') for f in broken) and sq(f'{L}/{bdb[0]}', 'select count(*) from pages', ro=False) == str(n2), f'{sorted(broken)} -> {sq(f"{L}/{bdb[0]}", "select count(*) from pages", ro=False) if bdb else None}')

# S6 restore refuses a bad snapshot and does not touch life.db
d, L = sandbox('s6'); sh('sh nightly.sh', L, {'OFFBOX_CMD': 'true'}); good = f'{L}/backups/life-{today}.db'
live_hash = hashlib.sha256(open(f'{L}/life.db', 'rb').read()).hexdigest()
def tamper(name, fn, rehash):
    p = f'{L}/backups/life-{name}.db'; b = bytearray(open(good, 'rb').read()); fn(b); open(p, 'wb').write(bytes(b))
    h = hashlib.sha256(bytes(b)).hexdigest() if rehash else hashlib.sha256(open(good, 'rb').read()).hexdigest()
    open(p + '.sha256', 'w').write(f'{h}  life-{name}.db\n'); return p
i = open(good, 'rb').read().find(b'Topic 12')
cases = [('flip content, hash NOT updated (bit rot on disk or in transit)', tamper('20200101', lambda b: b.__setitem__(i + 7, b[i + 7] ^ 1), False), 'sha256'),
         ('zeroed page, hash updated (damaged before hashing)', tamper('20200102', lambda b: b.__setitem__(slice(39 * 4096, 40 * 4096), bytes(4096)), True), 'integrity')]
fkp = f'{L}/backups/life-20200103.db'; shutil.copy(good, fkp); c = sqlite3.connect(fkp); c.execute('PRAGMA foreign_keys=OFF')
c.execute("INSERT INTO balances(account_id,day,amount,recorded_at,source) VALUES(999999,'2026-09-30',1,'2026-09-30T00:00:00.000Z','manual')"); c.commit(); c.close()
open(fkp + '.sha256', 'w').write(f'{hashlib.sha256(open(fkp, "rb").read()).hexdigest()}  life-20200103.db\n'); cases.append(('orphan balance, hash valid', fkp, 'fk'))
for label, p, why in cases:
    rc, out = sh(f'sh restore.sh {os.path.relpath(p, L)}', L)
    check(f'S6 restore refuses: {label}', rc != 0 and hashlib.sha256(open(f'{L}/life.db', 'rb').read()).hexdigest() == live_hash and not [f for f in os.listdir(L) if 'broken' in f], f'rc={rc} {out[-90:]!r}')
# S6b drill: restore from the OFF-BOX copy into an empty scratch directory
d2, L2 = sandbox('s6b'); sh('sh nightly.sh', L2, {'OFFBOX_CMD': OFF}); scratch = f'{d2}/drill'; os.makedirs(scratch)
rc, out = sh(f'sh restore.sh ../offbox/backups/life-{today}.db {scratch}', L2)
check('S6b drill: restore.sh from the off-box copy into an empty directory works (no old life.db to move)', rc == 0 and sq(f'{scratch}/life.db', 'pragma journal_mode', ro=False) == 'wal', out[-150:])

# S7 .gitignore
g = f'{R}/s_git'; shutil.rmtree(g, ignore_errors=True); os.makedirs(f'{g}/export'); os.makedirs(f'{g}/backups'); os.makedirs(f'{g}/dump')
if True:
    sec1 = extract.section(text, r'^### 2\.1 ', r'^### 2\.2 ')
    GI = re.search(r'`\.gitignore`[^\n]*\n[^\n]*\n\n```\n(.*?)\n```', sec1, re.S).group(1) + '\n'
else: GI = 'life.db\nlife.db-wal\nlife.db-shm\nlife.db.broken-*\nbackups/\ndump/\ndump.tmp/\n'
open(f'{g}/.gitignore', 'w').write(GI); os.makedirs(f'{g}/dump.tmp')
for f in ('life.db', 'life.db-wal', 'life.db-shm', 'life.db.broken-1', 'life.db.broken-1-wal', 'backups/life-1.db', 'backups/life-1.db.tmp', 'dump/balances.csv', 'dump.tmp/pages.csv', 'export/a.md'): open(f'{g}/{f}', 'w').write('x')
sh('git init -q . && git add -A', g); out = sh('git status --porcelain', g)[1]
check('S7 with the .gitignore of §2.1, `git add -A` stages only export/ and .gitignore', sorted(l[3:] for l in out.splitlines()) == ['.gitignore', 'export/a.md'], out.replace('\n', ' | '))
# S8 rsync semantics
d, L = sandbox('s8'); os.makedirs(f'{d}/mirror'); os.makedirs(f'{d}/src'); open(f'{d}/src/life-1.db', 'w').write('1'); open(f'{d}/src/life-2.db', 'w').write('2')
sh('rsync -a src/ mirror/', d); os.remove(f'{d}/src/life-1.db'); shutil.rmtree(f'{d}/src/x', ignore_errors=True)
sh('rsync -a src/ mirror/', d); keeps = os.path.exists(f'{d}/mirror/life-1.db'); sh('rsync -a --delete src/ mirror/', d); gone = not os.path.exists(f'{d}/mirror/life-1.db')
check('S8 rsync -a keeps a file deleted locally; rsync -a --delete removes it from the off-box copy too', keeps and gone)
print(f'{sum(res)}/{len(res)}' + (f' [mutant {MUT}]' if MUT else ''))
