#!/usr/bin/env python3
"""Run every validation suite against the DDL, the cookbook and the text of the docs (docs/).

    python3 tests/run_all.py              # every suite (~45 s)
    python3 tests/run_all.py --datasette  # + Datasette opens the file read-only (installs it into tests/.venv)
    python3 tests/run_all.py --mermaid    # + render every mermaid diagram (needs mmdc and a Chromium: see schema/render_diagrams.py)

Nothing here is a migration and nothing touches life.db: every suite builds throwaway databases from the DDL in
from docs/schema/schema.sql. Needs: python3 (with venv + network once, for markdown-it-py) and the sqlite3 CLI."""
import os, re, shutil, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.abspath(__file__))
ARGS = set(sys.argv[1:])
VENV = os.path.join(HERE, '.venv')

def venv_python():
    for rel in (('bin', 'python'), ('Scripts', 'python.exe')):  # POSIX, Windows
        p = os.path.join(VENV, *rel)
        if os.path.exists(p):
            return p
    return os.path.join(VENV, 'bin', 'python')

PY = venv_python()

def ensure_venv(extra=()):
    global PY
    if not os.path.exists(PY):
        subprocess.run([sys.executable, '-m', 'venv', VENV], check=True)
        PY = venv_python()
    need = ['markdown-it-py==4.2.0', *extra]
    have = subprocess.run([PY, '-m', 'pip', 'list', '--format=freeze'], capture_output=True, text=True).stdout.lower()
    missing = [p for p in need if p.lower() not in have]
    if missing:
        subprocess.run([PY, '-m', 'pip', 'install', '-q', *missing], check=True)

def preflight():
    def bad(msg): print(msg, flush=True); sys.exit(2)
    vt = lambda v: tuple(int(x) for x in re.findall(r'\d+', v)[:3])
    if not shutil.which('sqlite3'): bad("need the sqlite3 CLI (>= 3.53, with FTS5) on PATH; on Windows: sqlite.org's tools zip")
    ver = subprocess.run(['sqlite3', '--version'], capture_output=True, text=True).stdout.split()
    if not ver or vt(ver[0]) < (3, 53, 0): bad(f"the sqlite3 CLI is SQLite {ver[0] if ver else '?'}; need >= 3.53 with FTS5")
    f = subprocess.run(['sqlite3', ':memory:', 'CREATE VIRTUAL TABLE t USING fts5(x);'], capture_output=True, text=True)
    if f.returncode != 0 or f.stderr.strip(): bad("the sqlite3 CLI has no FTS5; need >= 3.53 built with FTS5 (--enable-fts5)")
    p = subprocess.run([PY, '-c', "import sqlite3; c=sqlite3.connect(':memory:'); c.execute('CREATE VIRTUAL TABLE t USING fts5(x)'); print(sqlite3.sqlite_version)"], capture_output=True, text=True)
    v = p.stdout.strip()
    if p.returncode != 0 or not v or vt(v) < (3, 53, 0):
        bad(f"the venv's Python sqlite3 module is SQLite {v or '(no FTS5 or unusable)'}; need >= 3.53 with FTS5 (on Windows: a newer sqlite3.dll in the interpreter's DLLs folder)")

def ratio(out, pat):
    m = re.findall(pat, out)
    return bool(m) and m[-1][0] == m[-1][1] and int(m[-1][1]) > 0

MET = lambda o: ratio(o, r'(\d+)/(\d+) met expectations')
SUITES = [   # (name, directory, script, predicate)
 ('dates',            'schema',    'dates.py',         MET),
 ('identity',         'schema',    'identity.py',      MET),
 ('named entities',   'schema',    'named.py',         MET),
 ('pages',            'schema',    'pages.py',         MET),
 ('links',            'schema',    'links.py',         MET),
 ('facts',            'schema',    'facts.py',         MET),
 ('habits',           'schema',    'habits.py',        MET),
 ('journal',          'schema',    'journal.py',       MET),
 ('writers',          'schema',    'writers.py',       MET),
 ('integrity',        'schema',    'integrity.py',     MET),
 ('imports',          'schema',    'imports.py',       MET),
 ('evolution',        'schema',    'evolution.py',     MET),
 ('cookbook',         'schema',    'cookbook.py',      MET),
 ('document',         'schema',    'document.py',      MET),
 ('diagrams',         'schema',    'diagrams.py',      MET),
 ('mutants',          'schema',    'mutants.py',       MET),
 ('vectors',          'wikilinks', 'check_vectors.py', lambda o: ratio(o, r'(\d+)/(\d+) vectors')),
 ('title fuzz',       'wikilinks', 'title_fuzz.py',    lambda o: 'would block a save): 0' in o and 'stricter app): 0' in o),
 ('save contract',    'wikilinks', 'probes.py',        lambda o: ratio(o, r'(\d+)/(\d+) probes passed')),
 ('doc save contract', 'wikilinks', 'docchecks.py',    MET),
]

def run(name, d, script, ok, env):
    t = time.time()
    try:
        p = subprocess.run([PY, script, env['DDL']], cwd=os.path.join(HERE, d), env=env, capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=600)
    except subprocess.TimeoutExpired:
        print(f'FAIL  {name:<18} {time.time() - t:5.1f}s  timed out after 600 s', flush=True)
        return False
    out = (p.stdout + p.stderr).strip()
    good = p.returncode == 0 and bool(ok(out))
    tail = out.splitlines()[-1][:90] if out else '(no output)'
    print(f"{'PASS' if good else 'FAIL'}  {name:<18} {time.time() - t:5.1f}s  {tail}", flush=True)
    if not good: print('\n'.join('      ' + l for l in out.splitlines()[-15:]), flush=True)
    return good

def main():
    ensure_venv(['datasette'] if '--datasette' in ARGS else [])
    preflight()
    scratch = tempfile.mkdtemp(prefix='lifelog-suites-')
    try:
        tmp = tempfile.mkdtemp(prefix='lifelog-ddl-', dir=scratch); ddl = os.path.join(tmp, 'ddl.sql')
        subprocess.run([sys.executable, os.path.join(HERE, 'lib', 'docsql.py'), 'ddl', ddl], check=True)
        n = sum(1 for _ in open(ddl))
        objs = subprocess.run(['sqlite3', ':memory:', '.read ' + ddl, 'SELECT count(*) FROM sqlite_master;'], capture_output=True, text=True, check=True).stdout.split()[-1]
        ver = subprocess.run(['sqlite3', '--version'], capture_output=True, text=True, check=True).stdout.split()[0]
        print(f'docs/schema/schema.sql: {n} lines, {objs} schema objects; SQLite {ver}\n', flush=True)
        env = dict(os.environ, DDL=ddl, PYTHONDONTWRITEBYTECODE='1', PYTHONUTF8='1', TMPDIR=scratch, TEMP=scratch, TMP=scratch)
        results = [run(*s, env) for s in SUITES]
        if '--datasette' in ARGS: results.append(run('datasette', 'schema', 'datasette_ro.py', MET, env))
        if '--mermaid' in ARGS: results.append(run('mermaid', 'schema', 'render_diagrams.py', lambda o: ratio(o, r'(\d+)/(\d+) diagrams rendered'), env))
        print(f"\n{sum(results)}/{len(results)} suites passed")
    finally:
        shutil.rmtree(scratch, ignore_errors=True)
        if os.path.exists(scratch):
            print(f'note: could not remove {scratch} (a file is still open)', flush=True)
    sys.exit(0 if all(results) else 1)

if __name__ == '__main__':
    main()
