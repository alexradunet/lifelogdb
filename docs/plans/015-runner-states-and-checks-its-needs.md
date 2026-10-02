# Plan 015: The suite runner states what it needs, checks it first, and never hangs

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- tests/run_all.py tests/schema/mutants.py tests/schema/writers.py tests/README.md AGENTS.md docs/contract/connections.md`
> If any in-scope file changed since this plan was written (plan 014 edits other lines of `AGENTS.md` — that is
> expected), compare the "Current state" excerpts against the live files; on a mismatch in the quoted lines, STOP.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: 014 (ordering only: both edit `AGENTS.md`)
- **Category**: dx
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

The repo's validation suites (`tests/`, run by `tests/run_all.py`) are the only proof that the database design in
`docs/` does what it says; `AGENTS.md` requires every suite green after every change. Today the setup text is wrong in
two ways that cost a new contributor (human or agent) time: it says the suites need SQLite ≥ 3.51.3, but
`tests/schema/evolution.py` runs `ALTER TABLE … DROP CONSTRAINT`, which needs **3.53**; and it says a run takes
"about 15 s" when it takes ~45 s (the mutant suite alone ~35 s). The runner also fails badly when its prerequisites are
missing (a bare `FileNotFoundError`/`IndexError` traceback when the `sqlite3` CLI is absent or lacks FTS5), has no
per-suite timeout (a deadlocked writer thread in `writers.py` hangs the run forever), and on Windows a failing
wikilink suite crashes with `UnicodeEncodeError` while printing its diagnostic, losing it. Standalone runs of
`mutants.py` leave ~54 MB of docs copies in the temp folder each time. One suite also asserts a compile-time default
of SQLite as if it were a property of SQLite.

## Current state

- `tests/run_all.py` — the runner. Relevant lines:
  ```python
  4:    python3 tests/run_all.py              # every suite (~15 s)
  ...
  64: def run(name, d, script, ok, env):
  65:     t = time.time()
  66:     p = subprocess.run([PY, script, env['DDL']], cwd=os.path.join(HERE, d), env=env, capture_output=True, text=True, stdin=subprocess.DEVNULL)
  ...
  81:         objs = subprocess.run(['sqlite3', ':memory:', '.read ' + ddl, 'SELECT count(*) FROM sqlite_master;'], capture_output=True, text=True).stdout.split()[-1]
  82:         ver = subprocess.run(['sqlite3', '--version'], capture_output=True, text=True).stdout.split()[0]
  83:         print(f'docs/schema/schema.sql: {n} lines, {objs} schema objects; SQLite {ver}\n', flush=True)
  84:         env = dict(os.environ, DDL=ddl, PYTHONDONTWRITEBYTECODE='1', TMPDIR=scratch, TEMP=scratch, TMP=scratch)
  ```
  `main()` starts at line 74; `PY` is the venv interpreter (`tests/.venv/Scripts/python.exe` on Windows,
  `tests/.venv/bin/python` elsewhere).
- `tests/schema/mutants.py:90-96` — runs one mutant; creates a temp dir and never removes it:
  ```python
  def run(suite, broken):
      d = tempfile.mkdtemp(prefix='mutant-'); root, ddl = os.path.join(d, 'docs'), os.path.join(d, 'ddl.sql')
      shutil.copytree(docsql.DOCS, root)
      ...
      p = subprocess.run([sys.executable, '-W', 'ignore', f'{suite}.py', ddl], cwd=HERE, env=dict(os.environ, DOCS=root, DDL=ddl, PYTHONDONTWRITEBYTECODE='1'),
                         capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=300)
      m = re.search(r': (\d+)/(\d+) met expectations\s*$', p.stdout.strip())
      return (int(m.group(1)), int(m.group(2))) if m else (0, -1), (p.stdout + p.stderr).strip()
  ```
- `tests/schema/writers.py:36`: `S.K('SQLite\'s default synchronous is FULL (2)', sqlite3.connect(mkdb()).execute('PRAGMA synchronous').fetchone()[0] == 2)`.
  SQLite's default `synchronous` is a compile-time option (`SQLITE_DEFAULT_SYNCHRONOUS`); a distribution may build
  with NORMAL. `AGENTS.md` ("Empiricism over intuition"): *"A suite must not depend on accidents of one SQLite build
  … it finds what it needs, or says plainly what it requires."* The doc side is `docs/contract/connections.md:13-14`:
  "refuses to run if … `synchronous` is not 2 (FULL, which is also SQLite's default, executed)".
- `tests/schema/writers.py:51`: the only version assertion in the suites (`>= (3, 51, 3)`) — that is the **writer**
  floor and is correct for what `writers.py` tests; leave it.
- Setup text:
  - `AGENTS.md:34`: `| the suites (`tests/`) | Python **≥ 3.12** (`Connection.setconfig`); its `sqlite3` module and the `sqlite3` CLI both on SQLite **≥ 3.51.3 with FTS5** (3.53 is used); network once, for the venv | `python3 tests/run_all.py` (about 15 s) |`
  - `tests/README.md:10`: `python3 tests/run_all.py              # every suite, ~15 s`
  - `tests/README.md:14-15`: `…runs SQLite\n≥ 3.51.3 built with FTS5 (3.53 was used); a distribution's build may lack FTS5 (`no such module: fts5`).`
- The writer floor (3.51.3) stated in `docs/contract/connections.md`, `docs/schema/schema.sql` and `lifelog_meta.sqlite`
  is a different thing and is correct. Do **not** change it.

Conventions: the suites are plain Python 3.12 scripts, no test framework; helpers live in `tests/lib/kit.py`. Match
the existing terse style (short names, one-line statements). Every suite ends with `<name>: X/Y met expectations`.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| All suites (Windows) | `tests/.venv/Scripts/python.exe tests/run_all.py` | last line `20/20 suites passed`, exit 0 |
| All suites (elsewhere) | `python3 tests/run_all.py` | same |
| Mutants alone | `tests/.venv/Scripts/python.exe tests/schema/mutants.py` | `mutants: 70/70 met expectations` |

## Scope

**In scope**:
- `tests/run_all.py`
- `tests/schema/mutants.py` (the `run()` function only)
- `tests/schema/writers.py` (line 36 only)
- `docs/contract/connections.md` (the one parenthesis on line 14)
- `tests/README.md`, `AGENTS.md` (the setup lines quoted above only)
- `docs/plans/README.md` (status row)

**Out of scope**:
- The writer floor 3.51.3 anywhere in `docs/` — correct as is.
- The list of mutants in `mutants.py` and the noticed/caught logic — plan 016 changes those.
- Pinning `datasette` — not worth it now (optional suite).
- Parallelising the mutants — possible later; not needed for correctness.

## Git workflow

- Branch: `advisor/015-runner-needs`
- Commit message style: `tests: run_all.py checks its SQLite first, times out, and says ~45 s` with a body that
  says suites changed and what you ran (AGENTS.md requires naming what you ran).
- Do NOT push.

## Steps

### Step 1: A preflight in `run_all.py`

Add a function `preflight()` and call it at the top of `main()`, after `ensure_venv(...)` and before the scratch
folder is made. It must check, and on failure print one plain line saying what is missing and how to fix it, then
`sys.exit(2)`:
1. `shutil.which('sqlite3')` is not None — else: `need the sqlite3 CLI (>= 3.53, with FTS5) on PATH; on Windows: sqlite.org's tools zip`.
2. The CLI version (`sqlite3 --version`, first token) parsed as a tuple of ints is `>= (3, 53, 0)`.
3. The CLI has FTS5: `sqlite3 :memory: "CREATE VIRTUAL TABLE t USING fts5(x);"` returns exit 0 with empty stderr.
4. The venv interpreter's module: run `[PY, '-c', "import sqlite3; c=sqlite3.connect(':memory:'); c.execute('CREATE VIRTUAL TABLE t USING fts5(x)'); print(sqlite3.sqlite_version)"]`;
   exit 0 and version `>= (3, 53, 0)` — else: `the venv's Python sqlite3 module is SQLite <v>; need >= 3.53 with FTS5 (on Windows: a newer sqlite3.dll in the interpreter's DLLs folder)`.

Then make lines 81–82 use `check=True` (they can no longer fail silently after the preflight).

**Verify**:
- `tests/.venv/Scripts/python.exe tests/run_all.py` → starts with the usual `docs/schema/schema.sql: … SQLite 3.53.x` line and ends `20/20 suites passed`.
- `PATH=/nonexistent tests/.venv/Scripts/python.exe tests/run_all.py; echo $?` (in Git Bash; use the absolute path of the venv python) → one line naming the sqlite3 CLI, exit code `2`, no traceback.

### Step 2: A timeout per suite, and UTF-8 output for every child

In `run()`: pass `timeout=600` to `subprocess.run`; catch `subprocess.TimeoutExpired` and treat it as a failed suite
(print `FAIL  <name> … timed out after 600 s`, return False).
In `main()`'s `env = dict(...)` add `PYTHONUTF8='1'`. In `mutants.py`'s `run()` env, add `PYTHONUTF8='1'` too.

**Verify**: full run → `20/20 suites passed`. (A quick manual check of the timeout path is optional: temporarily set
`timeout=0.01`, see every suite reported as timed out, revert.)

### Step 3: `mutants.py` removes each mutant's copy

In `run()` wrap the body after `mkdtemp` in `try: … finally: shutil.rmtree(d, ignore_errors=True)` so the return value
is computed before the folder goes.

**Verify**: in Git Bash, `before=$(ls -d "$TEMP"/mutant-* 2>/dev/null | wc -l); tests/.venv/Scripts/python.exe tests/schema/mutants.py | tail -1; after=$(ls -d "$TEMP"/mutant-* 2>/dev/null | wc -l); echo $before $after`
→ `mutants: 70/70 met expectations` and `after` equal to `before`.

### Step 4: Stop asserting the build's default `synchronous`

`writers.py:36`: replace the expectation with one about what the contract actually needs — that a connection set to
FULL reads back 2:
`c = sqlite3.connect(mkdb()); c.execute('PRAGMA synchronous=FULL'); S.K('synchronous=FULL reads back as 2 (what the writer checks)', c.execute('PRAGMA synchronous').fetchone()[0] == 2)`.
In `docs/contract/connections.md:14` change `(FULL, which is also SQLite's default,\nexecuted)` to `(FULL, executed; the default
of most builds, but set it anyway)`. Keep the word *executed* attached to what the suite now runs.

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/writers.py` → `writers: 23/23 met expectations`
(same count: one expectation replaced by one).

### Step 5: The setup text tells the truth

- `AGENTS.md:34`: `…both on SQLite **≥ 3.53 with FTS5** (writers need only 3.51.3; the suites run migrations, which need 3.53); network once, for the venv | `python3 tests/run_all.py` (about 45 s; Windows: `tests/.venv/Scripts/python.exe tests/run_all.py`) |`
- `tests/README.md:10`: `~15 s` → `~45 s, most of it the mutants`.
- `tests/README.md:14-15`: `≥ 3.51.3 built with FTS5 (3.53 was used)` → `≥ 3.53 built with FTS5 (the suites run migrations; a writer needs only 3.51.3)`, and add a sentence after it: `run_all.py checks both first and says what is missing.`
- `tests/run_all.py:4`: `(~15 s)` → `(~45 s)`.
- In `tests/README.md` (the Windows paragraph, around line 19), add: `The runner sets PYTHONUTF8=1 for every suite, so a failing suite's non-ASCII diagnostic prints instead of crashing.`

**Verify**: `grep -rn "15 s" AGENTS.md tests/README.md tests/run_all.py` → no output.

### Step 6: Full run

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`; `git status --short` shows only in-scope files.

## Test plan

The runner is itself the test harness, so its new behaviour is verified by the manual commands in Steps 1–3 (missing
CLI → exit 2 with one line; mutant temp folders not left behind). No new suite is added. `writers.py` keeps its count
(one expectation replaced).

## Done criteria

- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`, exit 0
- [ ] With the `sqlite3` CLI off `PATH`, the runner exits `2` with a one-line message and no traceback
- [ ] `grep -n "timeout=600" tests/run_all.py` → one match; `grep -n "PYTHONUTF8" tests/run_all.py tests/schema/mutants.py` → one match each
- [ ] `grep -n "rmtree" tests/schema/mutants.py` → one match
- [ ] `grep -rn "15 s\|≥ 3.51.3 with FTS5\|≥ 3.51.3 built with FTS5" AGENTS.md tests/README.md tests/run_all.py` → no output
- [ ] `grep -n "SQLite's default" docs/contract/connections.md tests/schema/writers.py` → no output
- [ ] Only in-scope files modified; `docs/plans/README.md` row 015 updated

## STOP conditions

- The venv's `sqlite3` module on your machine is older than 3.53 (the preflight would then fail the run you need to
  verify with). Report the versions instead of lowering the floor.
- `writers.py`'s expectation count is not 23/23 after Step 4.
- Any step seems to need a change to a mutant's text or the caught/missed logic (that is plan 016).

## Maintenance notes

- If the floor for migrations ever moves (a newer `ALTER TABLE` feature), update `preflight()` and the two setup lines together.
- Plan 016 adds mutants; each costs ~0.5 s. If the full run passes ~90 s, consider running the mutants in a
  `concurrent.futures.ThreadPoolExecutor` (each already has its own temp folder).
