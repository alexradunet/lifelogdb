# 076 — Exercise remaining validation gaps

- **Date:** 2026-10-07
- **Status:** IN PROGRESS
- **Baseline:** `6f2bfe8` (committed and pushed).
- **Authorization:** owner requested continuing the remaining validation gaps after plan 075, while retaining commit/push authorization.
- **Scope:** pinned static analysis, full race checks, minimum SQLite compatibility, actual Windows execution, lifetime/stress evidence and bounded storage failure probes. No migrations or private data.

## Completion criteria

Run the checks the available environment supports; fix reproducible findings with regressions. Use reviewed pinned development tools in isolated scratch storage and leave application dependencies unchanged unless a finding requires an explained update. Record commands, tool/engine versions, results and the boundary of every simulated failure. Real power-loss proof requires a suitable separate test environment and must not be inferred from process interruption, capacity limits or I/O errors.

Keep large generated databases outside git on explicitly selected scratch disk. Lifetime previews need a justified storage/time budget; metadata-only workload results must be labeled. Windows execution must use Windows, not a cross-compiled executable. Any CI workflow added for this purpose is explicit and minimal.

## Work and evidence

### Static analysis and storage failures

Reviewed development tools were installed in `/tmp/lifelog-analysis-tools`, outside application dependency files:
Staticcheck **2026.2.1 / v0.8.1** and govulncheck **v1.8.0**, using Go **1.27.1**.
The [Staticcheck release notes](https://staticcheck.dev/changes/2026.2/) establish Go 1.27 support;
the [pinned release](https://github.com/dominikh/go-tools/releases/tag/2026.2.1) and
[govulncheck source tag](https://go.googlesource.com/vuln/+/refs/tags/v1.8.0) provide reproducible tool versions.
No application dependency or persistent tool configuration changed.

All 17 Staticcheck findings were fixed: unused private wrappers and assignments, redundant checks and repeated type assertions.
Both independent NULL-key insert attempts remain in the identity test. Markdown traversal retains its node and line-break boundaries.
The final Staticcheck run passed without suppressions. Govulncheck's default text mode exited successfully with
“No vulnerabilities found”; this checks known reachable vulnerabilities, not arbitrary unknown defects.

The Linux `TestFilesystemWriteFailure` regression uses subprocess-local `RLIMIT_FSIZE` and an independent `EFBIG` control.
It requires the structured SQLite `SQLITE_IOERR_WRITE` result, then checks partial-transaction rollback, unchanged
body/names/revision/timestamp/FTS, reopen and successful retry. The snapshot case verifies partial-destination removal,
unchanged source, retry and restored contents. These cases passed before production changes; no storage-wrapper defect
was found. This is real filesystem write-error injection, not ENOSPC, xSync failure, torn writes or power loss.

### Compatibility and scale

SQLite **3.51.3** passed all **39 subject suites and 404 mutants** in an isolated module snapshot, with a narrow
driver adapter and explicit newer-engine evolution exclusions. The appendix preserves the reproduction and its limits.

A manual Windows workflow runs generation, generated-copy verification, vet and the full baseline on `windows-2025`.
Actions are pinned to reviewed commits with read-only repository permissions. Actual Windows results are pending dispatch
after this workflow reaches the default branch; adding the workflow alone is not platform coverage.

The existing production-writer scale runner was built at `6f2bfe8`, Go 1.27.1, modernc.org/sqlite v1.60.1 / SQLite 3.53.4.
Its dirty flag comes from pre-existing untracked Python caches; tracked source was clean when built. Runs use seed 2075,
generator version 1, five samples and automatically cleaned `/var/tmp` storage. Hardware: AMD Ryzen 7 7840HS,
Linux amd64, SKHynix HFS001TEJ9X115N NVMe, btrfs on encrypted `/dev/mapper/root`. Other validation ran concurrently;
these warm samples are correctness evidence and indicative costs, not isolated comparative benchmarks.

- **Lifetime metadata passed:** 18,262 days, 365,240 original readings, 21,485 corrections, 11,782 retractions and
  54,786 files. Build 251.034 seconds; live database 407,543,808 bytes, WAL 13,015,112 bytes, SHM 32,768 bytes.
  Historical-series samples 231.6–241.3 ms; snapshot 1.043 seconds; restore-copy/open 1.021 seconds.
  Logical digest `b1bf076df011f4591427dbc931d33c62713531db436cb45fee42b1284f9eb502`.
- **Small previews passed:** 14 unique valid JPEGs, 6,783,325 preview bytes; build 0.801 seconds.
  Logical digest `db17e1a0f197a1b2d68e3cad84030bbcac19cb2cc61cfcb4d5698293cac586b2`.
- **Metadata stress passed:** 1,460,960 original readings, 85,939 corrections, 47,128 retractions, 109,572 files,
  16,000 notes and four times the text sizes. Build 980.335 seconds; live database 3,268,665,344 bytes,
  WAL 193,916,072 bytes. Snapshot 9.960 seconds; restore-copy/open 10.430 seconds.
  Logical digest `e604db2dc8678d37ffb735b9d278b8b16926b7dbafdc909d937fd04bba9d353c`.
- **Lifetime previews:** starting with the same 50-year workload and representative unique JPEG payloads,
  over 650 GiB initially free and automatic cleanup of all copies.

Completed scale runs verified independent metric identities/values/days/order, exact file/preview totals, search/backlinks,
malformed-batch rollback, all four integrity groups and the same known answers after snapshot restore.
Metadata-only runs make no preview-capacity claim. The runner's keyed replay measures core recording, not workspace import throughput.

## Validation and remaining limits

Passed on Linux:

- `go generate ./...` and `go vet ./...`; generated schema unchanged.
- `TMPDIR=/var/tmp go test -count=1 ./...`: all packages, including the new write-failure tests and 404 mutants;
  contract package 93.085 seconds.
- `TMPDIR=/var/tmp CGO_ENABLED=1 go test -race -count=1 -timeout=20m ./...` at `6f2bfe8`:
  all packages passed; contract package 930.209 seconds.
- `TMPDIR=/var/tmp go test -count=1 -shuffle=on ./...`: all packages passed with the final Go edits;
  contract package 162.732 seconds while concurrent validation and scale work ran.
- `staticcheck ./...` using v0.8.1: no findings; `govulncheck ./...` using v1.8.0: no known vulnerabilities.
- Write-failure tests repeated 20 times and under the race detector; all affected package tests and identity/wikilinks/files suites.
- SQLite floor matrix and supporting fixtures/seeds, as detailed below.
- Document suite and `git diff --check`.

A second full race run with the final Go edits passed every application package but hit the 20-minute timeout
in the contract package. No data race was reported; the dump shows active SQLite work in five subject suites.
That run overlapped shuffled tests, the stress fixture and other host work. This is an incomplete check, not a pass;
the contract race run is being investigated and will be repeated with adequate resources/time.

Final race, Windows execution and lifetime preview results will be recorded before closing this plan.
Real power-loss behavior, sync faults and torn writes require a separate fault-capable environment and remain untested.
No real owner data was accessed. This work cannot establish freedom from every bug or every form of file tampering.

### Minimum SQLite engine reproduction

The canonical schema was checked on Linux amd64 using Go 1.27.1 and actual
SQLite 3.51.3, source ID
`737ae4a34738ffa0c3ff7f9bb18df914dd1cad163f28fd6b6e114a344fe6d618`.
The isolated dependency pins were `modernc.org/sqlite v1.46.2`, `libc v1.70.0`,
and `memory v1.11.0`. The [upstream driver changelog](https://gitlab.com/cznic/sqlite/-/raw/v1.46.2/CHANGELOG.md)
and [SQLite release record](https://sqlite.org/releaselog/3_51_3.html) identify this
engine. Production dependency files were unchanged.

The floor driver predates `_defensive` DSN support, so the scratch-only adapter
below calls SQLite's `SQLITE_DBCONFIG_DEFENSIVE` before connection pragmas. Its
23 `lib/` engine files were SHA256-identical to the downloaded official module.
The engine probe checks source identity and required settings, including actual
FTS shadow-table refusal. This validates the SQLite engine with the adapter,
not an unmodified old driver or an application dependency downgrade.

SQLite 3.53-only evolution exercises are explicitly excluded: DROP/ADD
CONSTRAINT, enum/partial-date CHECK widening and ALTER COLUMN SET NOT NULL.
The 18 compatible evolution assertions remain. The 404 mutant definitions,
clean baselines and witness matcher are unchanged. Both evolution mutants keep
their owning witnesses: “every CHECK in schema is named” and “type change refuses
a retained incoming typed edge.”

The recorded run passed 39 suites / 8,941 expectations and all 404 mutants in
90.713 seconds. Fixture ownership/path tests, mutant-witness tests, and three
graph-fuzz seed cases also passed (0.284 seconds). This is not a generated fuzz
campaign. `TestCollationHelper` is run by the pages suite as a subprocess and
normally skips standalone invocation. Mermaid rendering was not rerun for an
engine-only matrix. The initial incomplete snapshot omitted linked untracked
issue pages; after copying the complete docs tree, the matrix above passed.

To repeat, use Go 1.27.1, Python 3 and git, with authorized access to the pinned
Go module archives. Save the following two scripts as `snapshot.py` and
`prepare.py` in a fresh temporary directory. Both scripts write only there.

```python
# snapshot.py
from pathlib import Path
import subprocess, shutil, json, hashlib, sys
base=Path(__file__).resolve().parent
root=Path(sys.argv[1]).resolve()
dest=base/'repo'
if dest.exists():
 raise SystemExit('refusing to overwrite an existing scratch repo')
names=subprocess.check_output(['git','ls-files','-z'],cwd=root).decode().split('\0')
for name in names:
 if not name: continue
 src=root/name
 if not src.is_file(): continue
 out=dest/name;out.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src,out)
# Include untracked current process documents to keep index links valid.
shutil.copytree(root/'docs',dest/'docs',dirs_exist_ok=True)
(base/'snapshot.json').write_text(json.dumps({
 'commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root).decode().strip(),
 'schema_sha256':hashlib.sha256((root/'docs/schema/schema.sql').read_bytes()).hexdigest()
},indent=2)+'\n')
```

```python
# prepare.py
from pathlib import Path
import shutil, hashlib, json
base=Path(__file__).resolve().parent

def replace_once(text, old, new):
 if text.count(old) != 1:
  raise SystemExit('source drift: expected one occurrence of '+repr(old))
 return text.replace(old,new,1)

def omit_between(text, first, after):
 if text.count(first) != 1 or text.count(after) != 1:
  raise SystemExit('source drift: evolution boundary missing or duplicated')
 start,end=text.index(first),text.index(after)
 if start >= end:
  raise SystemExit('source drift: evolution boundaries out of order')
 return text[:start]+text[end:]

src=base/'modcache/modernc.org/sqlite@v1.46.2'; driver=base/'sqlite-driver'
if driver.exists():
 raise SystemExit('refusing to overwrite an existing scratch driver')
shutil.copytree(src,driver)
for directory in [driver, *[p for p in driver.rglob('*') if p.is_dir()]]:
 directory.chmod(0o755)
p=driver/'sqlite.go';p.chmod(0o644);s=(src/'sqlite.go').read_text();needle='func applyQueryParams(c *conn, query string) error {\n'
s=replace_once(s,needle,needle+'\tif err := applyFloorDefensive(c, query); err != nil { return err }\n');p.write_text(s)
(driver/'floor_defensive.go').write_text('''package sqlite

import (
 "fmt"
 "net/url"
 "strconv"
 "modernc.org/libc"
 "modernc.org/libc/sys/types"
 sqlite3 "modernc.org/sqlite/lib"
)

// Scratch compatibility adapter only. SQLite engine sources remain v1.46.2.
// The test suites use the later driver's _defensive DSN option. Apply the same
// SQLite C API before pragmas; this is not a shipped-driver compatibility test.
func applyFloorDefensive(c *conn, query string) error {
 q, err := url.ParseQuery(query)
 if err != nil { return err }
 values, found := q["_defensive"]
 if !found { return nil }
 if len(values) != 1 { return fmt.Errorf("_defensive must occur once") }
 enabled, err := strconv.ParseBool(values[0])
 if err != nil { return err }
 if !enabled { return nil }
 bp := libc.Xmalloc(c.tls, types.Size_t(16))
 if bp == 0 { return fmt.Errorf("defensive adapter allocation failed") }
 defer libc.Xfree(c.tls, bp)
 rc := sqlite3.Xsqlite3_db_config(c.tls, c.db, sqlite3.SQLITE_DBCONFIG_DEFENSIVE,
  libc.VaList(bp, int32(1), uintptr(0)))
 if rc != sqlite3.SQLITE_OK { return fmt.Errorf("defensive config: %d", rc) }
 return nil
}
''')
p=base/'repo/go.mod';s=p.read_text()
if any(line.lstrip().startswith('replace ') for line in s.splitlines()):
 raise SystemExit('review existing module replacements before repeating')
for old,new in [('modernc.org/sqlite v1.60.1','modernc.org/sqlite v1.46.2'),
                ('modernc.org/libc v1.77.1','modernc.org/libc v1.70.0'),
                ('modernc.org/memory v1.12.1','modernc.org/memory v1.11.0')]:
 s=replace_once(s,old,new)
s+='\nreplace modernc.org/sqlite => '+str(driver)+'\n';p.write_text(s)
(base/'repo/tests/floor_engine_test.go').write_text('''package tests

import (
 "path/filepath"
 "strings"
 "testing"
)

func TestFloorEngine(t *testing.T) {
 d := realDocs()
 s := &S{name:"floor-engine",d:d,ddl:d.DDL(),dir:t.TempDir()}
 defer s.close()
 c := s.freshWith(F{Path:filepath.Join(s.dir,"floor.db"),Hardened:true})
 got := c.str("SELECT sqlite_version()")
 source := c.str("SELECT sqlite_source_id()")
 t.Logf("SQLite %s source %s",got,source)
 if got != "3.51.3" || !strings.Contains(source,"737ae4a34738ffa0c3ff7f9bb18df914dd1cad163f28fd6b6e114a344fe6d618") {
  t.Fatalf("wrong engine: %s %s",got,source)
 }
 if c.tryx("DELETE FROM entities_fts_data") == "OK" { t.Fatal("DEFENSIVE was not enabled") }
 if c.n("PRAGMA trusted_schema") != 0 { t.Fatal("trusted_schema must be OFF") }
 if c.n("PRAGMA synchronous") != 2 { t.Fatal("synchronous must be FULL") }
 if c.n("PRAGMA foreign_keys") != 1 || c.n("PRAGMA recursive_triggers") != 1 { t.Fatal("foreign_keys and recursive_triggers must be ON") }
 t.Log("Explicit floor exclusions: ALTER TABLE DROP/ADD CONSTRAINT and ALTER COLUMN SET NOT NULL probes require SQLite 3.53; compatible evolution assertions remain enabled.")
}
''')
official={p.relative_to(src/'lib'):hashlib.sha256(p.read_bytes()).hexdigest()
          for p in (src/'lib').rglob('*') if p.is_file()}
actual={p.relative_to(driver/'lib'):hashlib.sha256(p.read_bytes()).hexdigest()
        for p in (driver/'lib').rglob('*') if p.is_file()}
if len(official) != 23 or actual != official:
 raise SystemExit('engine file inventory or content differs')
(base/'engine-files-verification.json').write_text(json.dumps({
 'engine_file_count':len(actual),'unchanged':True},indent=2)+'\n')
print('Prepared scratch driver adapter; all 23 engine files are unchanged.')

# Keep all floor-compatible evolution assertions, excluding only syntax added in
# SQLite 3.53. The canonical schema requires 3.51.3; those future migration
# exercises intentionally have the newer engine requirement in tests/README.md.
p=base/'repo/tests/evolution_test.go'
original=base/'evolution_test.original.go'
if original.exists():
 raise SystemExit('refusing to overwrite original evolution source')
shutil.copyfile(p, original)
s=original.read_text()
s=replace_once(s,'\terr := func(r string) bool { return strings.HasPrefix(r, "ERR") }\n','')
s=omit_between(s,'\ttype nt struct{ name, table string }',
               '\t// ---- architecture/non-goals CJK search:')
cjk='\t// ---- architecture/non-goals CJK search: the tokenizer switch is one transaction on a derived index\n\tc = s.fresh()'
s=replace_once(s,cjk,cjk.replace('\tc =','\tc :='))
s=omit_between(s,'\t// ---- an entity uid is additive',
               '\t// ---- comments: inside a statement kept')
s=replace_once(s,'\tt = s.connect("")','\tt := s.connect("")')
s=replace_once(s,'func evolution(s *S) {','func evolution(s *S) {\n\t// FLOOR MATRIX EXCLUSION: DROP/ADD CONSTRAINT and ALTER COLUMN SET NOT NULL require SQLite 3.53.\n\t// Named CHECK inventory, reader compatibility, tokenizer, link-kind/type guards and comments still run.')
p.write_text(s)
```

The preparation script above adds fail-fast source-drift checks to the original
experiment. It was executed in a fresh scratch directory and reproduced all four
Go files (driver hook, defensive adapter, floor engine probe and evolution
exclusions) byte-for-byte against the passing experiment. The three dependency
pins and all 23 engine-file hashes matched; missing/duplicate replacement anchors
and missing/duplicate/reversed exclusion boundaries were independently rejected.
The full matrix was not rerun for these script-only checks.

Run these commands, substituting the fresh temporary directory and source path:

```sh
set -eu
FLOOR_DIR=/tmp/lifelog-sqlite-floor-repeat
REPO_DIR=/path/to/lifelog
GO=/path/to/go1.27.1/bin/go
python3 "$FLOOR_DIR/snapshot.py" "$REPO_DIR"
GOMODCACHE="$FLOOR_DIR/modcache" "$GO" mod download -json modernc.org/sqlite@v1.46.2 modernc.org/libc@v1.70.0 modernc.org/memory@v1.11.0
python3 "$FLOOR_DIR/prepare.py"
cd "$FLOOR_DIR/repo"
GOCACHE="$FLOOR_DIR/gocache" GOMODCACHE="$FLOOR_DIR/modcache" "$GO" test -mod=mod ./tests -run '^Test(FloorEngine|Suites|Mutants)$' -count=1 -v > "$FLOOR_DIR/matrix.log" 2>&1
GOCACHE="$FLOOR_DIR/gocache" GOMODCACHE="$FLOOR_DIR/modcache" "$GO" test -mod=mod ./tests -run '^(TestCanonicalIdentityFixtureOwnershipAndRollback|TestCollationHelper|TestSchemaFixtureUsesLiteralFilename|TestMutantWitness|FuzzGraphTransitions)$' -count=1 -v > "$FLOOR_DIR/additional.log" 2>&1
```

The original experiment used a local module proxy for already-cached project
modules; the commands above use the configured Go proxy. Preserve download
checksums, source snapshot/schema hash, engine identity and logs with the run.
Original schema SHA256:
`2db05691d5322796e339c16630148995a1857550beaf23e32f21449cb8e64ab8`;
source HEAD `6f2bfe8f676786046c708a12cc38a45c0867a2b9`.

Limits: Linux amd64, fresh-init canonical schema and synthetic contract cases;
no older engines, alternative compile options, Windows minimum-engine run or
full application-package matrix. SQLite 3.53-only evolution remains validated
by the ordinary current-engine baseline, not by this floor experiment.
