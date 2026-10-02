# Plan 012: The suites leave nothing in TEMP, pin their parser, and `fixes.md` goes

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 3e2fcf4..HEAD -- tests/run_all.py tests/README.md fixes.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live files before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (independent of 008–011; touches `tests/README.md` but not its mutant count)
- **Category**: dx
- **Planned at**: commit `3e2fcf4`, 2026-10-02

## Why this matters

`tests/README.md` says the suites build throwaway databases "and discard them". They do not: one
`run_all.py` run leaves about 110 directories (~25 MB) in the system temp folder — 81 `mutant-*`, a
`lifelog-ddl-*`, `integrity-*`, `lifelog-import-*` and unnamed `tmp*` ones. On the owner's machine 2,100+
`mutant-*` directories (420 MB) had piled up within two days. Separately, `run_all.py` installs
`markdown-it-py` unpinned, while the Python reference's behaviour (and a documented divergence in §2.4)
is tied to a specific version, so a new release could silently change what the suites check. And
`fixes.md` at the repo root is superseded: `plans/README.md` says it can go once plans 001–007 are done,
and all seven are DONE.

## Current state

- `tests/run_all.py` (90 lines). Relevant parts at `3e2fcf4`:
  ```python
  def ensure_venv(extra=()):
      ...
      need = ['markdown-it-py', *extra]
      have = subprocess.run([PY, '-m', 'pip', 'list', '--format=freeze'], capture_output=True, text=True).stdout.lower()
      missing = [p for p in need if p.lower() not in have]
      if missing:
          subprocess.run([PY, '-m', 'pip', 'install', '-q', *missing], check=True)
  ...
  def main():
      ensure_venv(['datasette'] if '--datasette' in ARGS else [])
      tmp = tempfile.mkdtemp(prefix='lifelog-ddl-'); ddl = os.path.join(tmp, 'ddl.sql')
      ...
      env = dict(os.environ, DDL=ddl, PYTHONDONTWRITEBYTECODE='1')
      results = [run(*s, env) for s in SUITES]
      ...
      print(f"\n{sum(results)}/{len(results)} suites passed")
      sys.exit(0 if all(results) else 1)
  ```
  Every suite runs as a child process with `env`; `mutants.py` starts its own children with
  `dict(os.environ, …)`, so they inherit it too.
- Temp directories are created without cleanup in `tests/schema/mutants.py:87`, `writers.py:9`,
  `pages.py:115`, `datasette_ro.py:8`, `tests/wikilinks/probes.py:99` and `run_all.py:76`
  (`integrity.py` and `imports.py` call `shutil.rmtree(..., ignore_errors=True)`, which on Windows
  leaves files a still-open SQLite connection holds).
- Python's `tempfile` takes its directory from the environment variables `TMPDIR`, `TEMP`, `TMP`
  (in that order, on every OS). So one directory set in `env` captures every temp file of every child.
- The venv holds `markdown-it-py==4.2.0` and `mdurl==0.1.2` (`tests/.venv/Scripts/python.exe -m pip list --format=freeze`).
- `fixes.md` — 109 lines, review findings already carried out by plans 001–007; referenced only by
  `plans/README.md` and the DONE plans.

## Commands you will need

| Purpose | Command (Git Bash, repo root) | Expected on success |
|---|---|---|
| All suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (elsewhere `python3 tests/run_all.py`) | `20/20 suites passed` |
| Count leftovers | `tests/.venv/Scripts/python.exe -c "import os,tempfile; d=tempfile.gettempdir(); print(sum(1 for n in os.listdir(d) if n.startswith(('mutant-','lifelog-ddl-','lifelog-import-','integrity-','lifelog-suites-'))))"` | a number |

## Scope

**In scope:** `tests/run_all.py`, `tests/README.md` (one sentence), delete `fixes.md`, `plans/README.md`
(status row).

**Out of scope:** the individual suites' `mkdtemp` calls (the runner fixes them all at once; a suite run
by hand still leaves its own temp directory, which the README will say); `SCHEMA.md`.

## Git workflow

Branch `advisor/012-suites-clean-up`; one commit; subject like
`tests: run_all.py runs the suites in one temp folder and removes it; markdown-it-py pinned`; `Ran: …`.

## Steps

### Step 1: Measure the leak

Run the "Count leftovers" command, then the suites, then the count again.

**Verify**: the second count is larger than the first by roughly 80–90 (the exact number varies).

### Step 2: One scratch folder per run

In `tests/run_all.py`:
- add `shutil` to the `import` line;
- at the start of `main()`, create `scratch = tempfile.mkdtemp(prefix='lifelog-suites-')`;
- create the DDL folder inside it: `tmp = tempfile.mkdtemp(prefix='lifelog-ddl-', dir=scratch)`;
- build `env` as `dict(os.environ, DDL=ddl, PYTHONDONTWRITEBYTECODE='1', TMPDIR=scratch, TEMP=scratch, TMP=scratch)`;
- after the results are printed and **before** `sys.exit`, remove it:
  ```python
  shutil.rmtree(scratch, ignore_errors=True)
  if os.path.exists(scratch):
      print(f'note: could not remove {scratch} (a file is still open)', flush=True)
  ```
  Wrap the body between creating `scratch` and the summary in `try: … finally:` so a crash also cleans up
  (the `sys.exit` stays after the `finally`).

**Verify**: run the leftovers count, the suites (`20/20 suites passed`), and the count again → the two
counts are **equal**.

### Step 3: Pin the parser

In `ensure_venv`, change `need = ['markdown-it-py', *extra]` to
`need = ['markdown-it-py==4.2.0', *extra]`. The membership test (`p.lower() not in have`) then checks the
`name==version` line of `pip list --format=freeze`, and `pip install` installs exactly that version when it
differs.

**Verify**: run the suites → `20/20 suites passed`, and
`tests/.venv/Scripts/python.exe -m pip list --format=freeze | grep -i markdown-it-py` → `markdown-it-py==4.2.0`.

### Step 4: `tests/README.md`

After the sentence that says the suites build throwaway databases and discard them, add:
"`run_all.py` gives every suite one temporary folder and removes it at the end; a suite run on its own
leaves its folder behind. The reference parser is pinned (`markdown-it-py==4.2.0`)."

**Verify**: `grep -n "removes it at the end" tests/README.md` → one match.

### Step 5: Delete `fixes.md`

`git rm fixes.md`. In `plans/README.md`, the context bullet "`fixes.md` is superseded by plans 001–007;
plan 012 deletes it." becomes "`fixes.md` was carried out by plans 001–007 and deleted by plan 012."

**Verify**: `git ls-files fixes.md` → empty; all suites → `20/20 suites passed` (`document.py` does not read it).

## Test plan

No new suite: the leftovers count before and after a run (Step 2) is the check, with the full suite run as
regression.

## Done criteria

- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] The leftovers count is the same before and after a full run
- [ ] `grep -n "markdown-it-py==4.2.0" tests/run_all.py` → one match
- [ ] `git ls-files fixes.md` → empty
- [ ] Only `tests/run_all.py`, `tests/README.md`, `fixes.md` (deleted) and `plans/README.md` changed

## STOP conditions

- After Step 2 the counts still differ by more than a handful: something writes outside `TMPDIR`/`TEMP`/`TMP`
  (for example `probes.py`'s `SCRATCH` variable); report which prefix grows.
- Pinning makes a suite fail: the venv had a different version than this plan assumes; report the version.

## Maintenance notes

- Bump the pin deliberately, together with any vector or §2.4 text that depends on the parser's behaviour.
- The owner may delete the old leftovers once: every `mutant-*`, `lifelog-ddl-*`, `lifelog-import-*` and
  `integrity-*` folder in the temp directory older than today is safe to remove. That is a manual action,
  not part of this plan.
