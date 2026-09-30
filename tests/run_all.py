#!/usr/bin/env python3
"""Run every validation suite against the DDL, cookbook and scripts in SCHEMA.md.

    python3 tests/run_all.py              # fast suites (~1-2 min)
    python3 tests/run_all.py --slow       # + the nightly/restore script harness (~2 min, ~150 MB of disk in ~/.cache)
    python3 tests/run_all.py --mutants    # + break each script on purpose and require the harness to notice (~8 min)
    python3 tests/run_all.py --datasette  # + the read-only check of Datasette (installs it into tests/.venv)

Nothing here is a migration and nothing touches life.db: every suite builds throwaway databases from the DDL it extracts
from SCHEMA.md section 3. Needs: python3 (with venv + network once, for markdown-it-py), the sqlite3 CLI, git, rsync, GNU coreutils."""
import os, re, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.abspath(__file__)); ROOT = os.path.dirname(HERE)
ARGS = set(sys.argv[1:])
VENV = os.path.join(HERE, '.venv'); PY = os.path.join(VENV, 'bin', 'python')

def ensure_venv(extra=()):
    if not os.path.exists(PY):
        subprocess.run([sys.executable, '-m', 'venv', VENV], check=True)
    need = ['markdown-it-py', *extra]
    have = subprocess.run([PY, '-m', 'pip', 'list', '--format=freeze'], capture_output=True, text=True).stdout.lower()
    missing = [p for p in need if p.lower() not in have]
    if missing:
        subprocess.run([PY, '-m', 'pip', 'install', '-q', *missing], check=True)

def ratio(out, pat=r'(\d+)/(\d+)'):
    m = re.findall(pat, out)
    return bool(m) and m[-1][0] == m[-1][1] and int(m[-1][1]) > 0

SUITES = [   # (name, directory, argv, predicate, what it proves)
 ('regress',       'schema',    ['regress.py', '{DDL}'],      lambda o: ratio(o, r'(\d+)/(\d+)\s*$'), 'v1.4 behaviours: CHECKs, triggers, FKs, STRICT, FTS'),
 ('finprobes',     'schema',    ['finprobes.py', '{DDL}'],    lambda o: ratio(o, r'finance probes: (\d+)/(\d+)'), 'D18 money: currencies, accounts, balances, fx_rates'),
 ('r5probes',      'schema',    ['r5probes.py', '{DDL}'],     lambda o: ratio(o, r'round-5 probes: (\d+)/(\d+)'), 'imports, retractions, recorded_at, no-delete triggers, named CHECKs'),
 ('r6probes',      'schema',    ['r6probes.py', '{DDL}'],     lambda o: ratio(o, r'round-6 probes: (\d+)/(\d+)'), 'tz, completed_day, one place per event'),
 ('r7probes',      'schema',    ['r7probes.py', '{DDL}'],     lambda o: ratio(o, r'round-7 probes: (\d+)/(\d+)'), 'BEGIN IMMEDIATE race, pragmas, read-only readers, text fixes'),
 ('r10probes',     'schema',    ['r10probes.py', '{DDL}'],    lambda o: ratio(o, r'round-10 probes: (\d+)/(\d+)'), 'device-name titles, the 2075 test, the deferred features, imports'),
 ('r11probes',     'schema',    ['r11probes.py', '{DDL}'],    lambda o: ratio(o, r'round-11 probes: (\d+)/(\d+)'), 'notes and wiki merged into one page kind: day, titles, lookups, ghosts, day view'),
 ('r11 mutants',   'schema',    ['r11_mutants.py'],           lambda o: ratio(o, r'(\d+)/(\d+) broken documents'), 'each round-11 rule put back the old way must fail a probe'),
 ('expander',      'schema',    ['fuzz.py', '{DDL}'],         lambda o: re.search(r'compared (\d+) mismatches 0\s*$', o.strip().splitlines()[-1]) is not None, 'recurrence expander vs an independent oracle'),
 ('net worth',     'schema',    ['nw.py', '{DDL}'],           lambda o: 'real-mismatches=0' in o, 'net worth queries vs an exact-rational oracle'),
 ('cookbook',      'schema',    ['cookbook_doc.py', '{DDL}'], lambda o: 'cookbook failures: 0' in o, 'every SQL block of section 6 runs'),
 ('vectors',       'wikilinks', ['check_vectors.py'],         lambda o: ratio(o, r'(\d+)/(\d+) vectors'), 'the wikilink/tag extraction vectors of 2.5'),
 ('title fuzz',    'wikilinks', ['title_fuzz.py'],            lambda o: 'would block a save): 0' in o and 'stricter app): 0' in o, 'the app-side title predicate equals the DDL CHECK'),
 ('save contract', 'wikilinks', ['probes.py'],                lambda o: ratio(o, r'(\d+)/(\d+) probes passed'), 'the wikilink save procedure against the real DDL'),
 ('doc checks',    'wikilinks', ['docchecks.py'],             lambda o: ratio(o, r'(\d+)/(\d+) document checks'), 'the document text: vectors, 6.14 SQL, 6.5, stale phrases'),
]
SLOW = [('backup scripts', 'backups', ['scripts_test.py'], lambda o: ratio(o, r'(\d+)/(\d+)\s*$'), 'nightly.sh and restore.sh extracted from 2.8, under write load')]

def run(name, d, argv, ok, what, env):
    t = time.time()
    p = subprocess.run([PY, *[a.replace('{DDL}', env['DDL']) for a in argv]], cwd=os.path.join(HERE, d), env=env, capture_output=True, text=True, stdin=subprocess.DEVNULL)
    out = (p.stdout + p.stderr).strip(); good = False
    try: good = bool(ok(out)) and (p.returncode == 0 or name in ('expander',))
    except Exception: good = False
    tail = out.splitlines()[-1][:90] if out else '(no output)'
    print(f"{'PASS' if good else 'FAIL'}  {name:<15} {time.time() - t:5.1f}s  {tail}", flush=True)
    if not good: print('\n'.join('      ' + l for l in out.splitlines()[-12:]), flush=True)
    return good

def main():
    ensure_venv(['datasette'] if '--datasette' in ARGS else [])
    tmp = tempfile.mkdtemp(prefix='lifelog-ddl-')
    ddl = os.path.join(tmp, 'ddl.sql')
    subprocess.run([sys.executable, os.path.join(HERE, 'lib', 'docsql.py'), 'ddl', ddl], check=True)
    n = sum(1 for _ in open(ddl))
    chk = subprocess.run(['sqlite3', ':memory:', '.read ' + ddl, 'SELECT count(*) FROM sqlite_master;'], capture_output=True, text=True).stdout.split()[-1]
    print(f'SCHEMA.md section 3: {n} lines, {chk} schema objects; SQLite {subprocess.run(["sqlite3", "--version"], capture_output=True, text=True).stdout.split()[0]}\n', flush=True)
    env = dict(os.environ, DDL=ddl, PYTHONDONTWRITEBYTECODE='1')
    suites = SUITES + (SLOW if ('--slow' in ARGS or '--mutants' in ARGS) else [])
    results = [run(*s, env) for s in suites]
    if '--datasette' in ARGS:
        results.append(run('datasette', 'schema', ['r7probes_ds.py', '{DDL}'], lambda o: ratio(o, r'(\d+)/(\d+) met expectations'), 'Datasette opens the file read-only', env))
    if '--mutants' in ARGS:
        t = time.time()
        p = subprocess.run(['sh', os.path.join(HERE, 'backups', 'mutants.sh')], env=dict(env, PYTHON=PY), capture_output=True, text=True, stdin=subprocess.DEVNULL)
        lines = [l for l in p.stdout.splitlines() if l.strip()]
        def caught(l):
            m = re.search(r'(\d+)/(\d+) \[mutant', l)
            return ('FileNotFoundError' in l or 'Traceback' in l or 'FAIL' in l) or (m is not None and m.group(1) != m.group(2))
        ncaught = sum(caught(l) for l in lines); good = len(lines) >= 14 and ncaught == len(lines)
        print(f"{'PASS' if good else 'FAIL'}  {'mutants':<15} {time.time() - t:5.1f}s  {ncaught}/{len(lines)} broken scripts were noticed", flush=True)
        if not good: print('\n'.join('      ' + l[:140] for l in lines if not caught(l)))
        results.append(good)
    print(f"\n{sum(results)}/{len(results)} suites passed")
    sys.exit(0 if all(results) else 1)

if __name__ == '__main__':
    main()
