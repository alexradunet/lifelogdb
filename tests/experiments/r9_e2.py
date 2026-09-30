"""E2: is a plain cp of a live WAL database torn? Declared: unsure. Main file alone = an OLDER state, normally consistent; a checkpoint in progress
could tear it; main+wal copied one after the other can disagree. Count what happens over many copies with constant checkpointing."""
import subprocess, sqlite3, shutil, os, sys, time
D = '/home/alex/.cache/lifelog-r9/e2'; shutil.rmtree(D, ignore_errors=True); os.makedirs(D)
def state(path):
    try:
        c = sqlite3.connect(f'file:{path}?mode=ro', uri=True)
        ic = c.execute('pragma integrity_check').fetchone()[0]
        p = c.execute("select count(*) from pages where body like 'live %'").fetchone()[0]
        m = c.execute("select count(*) from measurements where source='live'").fetchone()[0]
        c.close()
        if ic != 'ok': return 'integrity: ' + ic[:50]
        if p != m: return f'torn pair {p}/{m}'
        return 'ok'
    except sqlite3.Error as e: return 'error: ' + str(e)[:60]
def rm(dst):
    for ext in ('', '-wal', '-shm'):
        if os.path.exists(dst + ext): os.remove(dst + ext)
def run(label, ckpt, n, how):
    src = f'{D}/{label}.db'; shutil.copy('/home/alex/.cache/lifelog-r9/y5.db', src)
    w = subprocess.Popen(['venv/bin/python', 'r9writer.py', src, str(ckpt), '0.004'], stdout=subprocess.PIPE, text=True); time.sleep(0.6)
    out = {}
    for i in range(n):
        dst = f'{D}/{label}_c.db'; how(src, dst); r = state(dst); out[r.split(':')[0].split(' ')[0] + (' ' + r.split(' ')[1] if r.startswith('torn') else '')] = out.get(r.split(':')[0].split(' ')[0] + (' ' + r.split(' ')[1] if r.startswith('torn') else ''), 0) + 1; rm(dst)
    open(src + '.stop', 'w').close(); w.communicate(); rm(src); rm(src + '.stop')
    print(f'{label}: {n} copies -> {out}', flush=True)
def cpc(src, dst): subprocess.run(['cp', '--reflink=never', src, dst], check=True)
def cp_main(src, dst): cpc(src, dst)
def cp_both(src, dst):
    cpc(src, dst)
    if os.path.exists(src + '-wal'): cpc(src + '-wal', dst + '-wal')
def cp_both_rev(src, dst):      # wal first, then main
    if os.path.exists(src + '-wal'): cpc(src + '-wal', dst + '-wal')
    cpc(src, dst)
def cp_default(src, dst): subprocess.run(['cp', src, dst], check=True)
run('cp_default_main_ckpt20', 20, 100, cp_default)
run('main_only_ckpt20', 20, 150, cp_main)
run('main_only_ckpt1000', 1000, 150, cp_main)
run('main_then_wal_ckpt20', 20, 150, cp_both)
run('wal_then_main_ckpt20', 20, 150, cp_both_rev)
