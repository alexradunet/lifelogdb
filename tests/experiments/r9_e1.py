"""E1 (redone): does the nightly snapshot COMPLETE while another process writes, and is it consistent when it does?
Declared expectations: .backup completes at low write rates and consistent; at ~150 commits/s it does not complete (observed in the first, discarded run);
VACUUM INTO completes at every rate and is always consistent (single read transaction)."""
import subprocess, sqlite3, shutil, os, sys, time
sys.path.insert(0, '.')
D = 'r9/e1'; shutil.rmtree(D, ignore_errors=True); os.makedirs(D)
TIMEOUT = 40
def state(path):
    c = sqlite3.connect(f'file:{path}?mode=ro', uri=True)
    ic = c.execute('pragma integrity_check').fetchone()[0]; fk = c.execute('pragma foreign_key_check').fetchall()
    p = c.execute("select count(*) from pages where body like 'live %'").fetchone()[0]
    m = c.execute("select count(*) from measurements where source='live'").fetchone()[0]
    o = c.execute("select count(*) from entities e where type='page' and not exists (select 1 from pages p where p.id=e.id)").fetchone()[0]
    c.close(); return ic == 'ok' and not fk and o == 0 and p == m, p
def trial(label, gap, sql_fmt):
    src = f'{D}/{label}.db'; shutil.copy('r9/y1.db', src)
    w = subprocess.Popen(['venv/bin/python', 'r9writer.py', src, '1000', str(gap)], stdout=subprocess.PIPE, text=True)
    time.sleep(0.6); dst = f'{D}/{label}_snap.db'; t0 = time.time()
    try:
        subprocess.run(['sqlite3', src, sql_fmt.format(dst=dst)], check=True, capture_output=True, timeout=TIMEOUT); took = time.time() - t0; done = True
    except subprocess.TimeoutExpired:
        took = time.time() - t0; done = False
    open(src + '.stop', 'w').close(); n = int(w.communicate()[0]); rate = n / max(1e-9, time.time() - t0 + 0.6)
    if done:
        ok, live = state(dst); print(f'{label:26s} writer {n/ (took+0.6):6.0f} commits/s  snapshot done in {took:5.2f}s  consistent={ok} (live rows {live})', flush=True)
    else:
        print(f'{label:26s} writer {n/(took+0.6):6.0f} commits/s  snapshot DID NOT COMPLETE in {TIMEOUT}s', flush=True)
    for f in os.listdir(D):
        if f.startswith(label): os.remove(f'{D}/{f}')
    return done
print('--- .backup')
for gap in (1.0, 0.2, 0.05, 0.01, 0.004): trial(f'backup_gap{gap}', gap, '.backup {dst}')
print('--- VACUUM INTO')
for gap in (0.05, 0.004, 0.0): trial(f'vacuum_gap{gap}', gap, "VACUUM INTO '{dst}'")
print('--- idle baseline')
src = f'{D}/idle.db'; shutil.copy('r9/y1.db', src); t0 = time.time(); subprocess.run(['sqlite3', src, f'.backup {D}/idle_snap.db'], check=True); print(f'idle .backup {time.time()-t0:.2f}s')
