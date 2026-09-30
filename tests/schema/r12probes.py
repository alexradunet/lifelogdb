"""Round-12 probes (run:  python3 r12probes.py DDLFILE).  Expected outcome is in each label.
Round 12 narrowed SCHEMA.md to the schema and its reliability: the markdown export, the snapshot / restore / dump contract
and their tests are withdrawn (section 7).
A  The contract as data lost its two keys: `lifelog_meta` has 23 rows, none called export or backups; the 2075 table has
   20 numbered questions.
B  The three integrity checks of section 2.8, taken literally from the document text and run on the LIVE file (no snapshot):
   E1 clean, E2 zeroed page, E3 truncated file, E4 flipped index entry, E5 flipped value (undetected, as the text says),
   E6 orphan balance written with foreign_keys=OFF, E7 an entities row without a domain row.
C  The live text (sections 1-7) no longer specifies an export folder, a dump, snapshots, an off-box copy or their scripts.
D  tests/ carries no backup harness and no reference to the withdrawn scripts."""
import os, re, shutil, sqlite3, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
DDL = open(sys.argv[1], encoding='utf-8').read()
doc = docsql.doc_text()
live = doc[:doc.index('## 8. References')]
res = []
def K(label, cond, detail=''):
    res.append(bool(cond))
    if not cond: print('  FAIL', label, detail)

# ---- A
c = sqlite3.connect(':memory:'); c.executescript(DDL)
meta = dict(c.execute('select key, value from lifelog_meta').fetchall())
K('A lifelog_meta has 24 rows (23 + sqlite and provenance, round 15, minus fx_rates)', len(meta) == 24, len(meta))
K('A no export or backups key', not ({'export', 'backups'} & set(meta)), sorted({'export', 'backups'} & set(meta)))
K('A no lifelog_meta value mentions export/, dump/, backups/, restore or VACUUM INTO', not [k for k, v in meta.items() if re.search(r'export/|dump/|backups/|restore|VACUUM INTO', v, re.I)])
sec = doc[doc.index('### 2.11 '):doc.index('## 3. The schema')]
rows = re.findall(r'^\|\s*(\d+)\s*\|([^|]+)\|([^|]+)\|([^|]+)\|\s*$', sec, re.M)
K('A the 2075 table has 21 questions numbered 1..21', [int(r[0]) for r in rows] == list(range(1, 22)), [r[0] for r in rows])

# ---- B  the block of section 2.8, executed literally
s28 = doc[doc.index('### 2.8 '):doc.index('### 2.9 ')]
blk = re.search(r'```sql\n(.*?)\n```', s28, re.S)
K('B section 2.8 has one sql block with the checks', blk is not None and blk.group(1).count(';') >= 3)
stmts = [re.sub(r'\s*--.*$', '', l).strip() for l in (blk.group(1).splitlines() if blk else [])]
stmts = [x for x in stmts if x]
K('B the block is exactly four statements: integrity_check, foreign_key_check, the orphan query, the FTS5 integrity-check (round 15)',
  len(stmts) == 4 and stmts[0].lower().startswith('pragma integrity_check') and stmts[1].lower().startswith('pragma foreign_key_check') and stmts[2].upper().startswith('SELECT ID FROM ENTITIES')
  and "'integrity-check', 1" in stmts[3], stmts)
D = tempfile.mkdtemp(prefix='r12-'); NOW = "'2026-09-30T10:00:00.000Z'"; base = f'{D}/base.db'
c = sqlite3.connect(base, isolation_level=None); c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA recursive_triggers=ON'); c.executescript(DDL)
c.execute('BEGIN IMMEDIATE')
def ent(t): c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES ('{t}',{NOW},{NOW})"); return c.execute('select last_insert_rowid()').fetchone()[0]
for i in range(3000):                                   # 3 000 pages so that the table and pages_title span many pages
    pid = ent('page')
    if i % 2: c.execute("INSERT INTO pages(id,kind,title,title_key,day,body) VALUES (?, 'page', ?, ?, '2026-09-30', ?)", (pid, f'Title {i:05d}', f'title {i:05d}', f'BODYMARK{i:05d} ' + 'lorem ipsum ' * 20))
    else:     c.execute("INSERT INTO pages(id,kind,day,body) VALUES (?, 'memo', '2026-09-30', ?)", (pid, f'BODYMARK{i:05d} ' + 'dolor sit amet ' * 20))
c.execute("INSERT INTO events(id,title,start_day) VALUES (?, 'ev', '2026-09-30')", (ent('event'),))          # one row of every other domain type,
c.execute("INSERT INTO tasks(id,title) VALUES (?, 'tk')", (ent('task'),))                                      # so a query that forgets one of the six
c.execute("INSERT INTO people(id,name) VALUES (?, 'pe')", (ent('person'),))                                    # tables reports a false orphan on a clean file
c.execute("INSERT INTO places(id,name) VALUES (?, 'pl')", (ent('place'),))
c.execute("INSERT INTO holdings(id,name,side,currency) VALUES (?, 'ac', 'asset', 'EUR')", (ent('holding'),))
c.execute('COMMIT'); c.execute('PRAGMA wal_checkpoint(TRUNCATE)'); c.close()
PS = sqlite3.connect(base).execute('pragma page_size').fetchone()[0]
def sq(p, sql):
    r = subprocess.run(['sqlite3', '-readonly', p, sql], capture_output=True, text=True, stdin=subprocess.DEVNULL); return (r.stdout + r.stderr).strip()
def checks(p):
    """the first three checks (read-only); the FTS5 check writes, so it is proven in r15probes.py"""
    if len(stmts) != 4: return None, None, None
    return sq(p, stmts[0]).splitlines()[:1], sq(p, stmts[1]), sq(p, stmts[2].rstrip(';'))
def cp(name): p = f'{D}/{name}.db'; shutil.copy(base, p); return p
def scan(p, pat, want_index):
    """offset of a match that is (want_index) an index cell, i.e. NOT directly preceded by the row's own title in the table"""
    data = open(p, 'rb').read()
    for m_ in re.finditer(pat, data):
        j = m_.start()
        if (re.fullmatch(rb'Title \d{5}', data[j - 11:j]) is not None) != want_index: return j
def flip(p, j, off=0):
    with open(p, 'r+b') as f: f.seek(j + off); ch = f.read(1)[0]; f.seek(j + off); f.write(bytes([ch ^ 1]))
ic, fk, orph = checks(base)
K('B E1 clean file with a row of every domain type: integrity ok, foreign_key_check empty, orphan query empty', ic == ['ok'] and fk == '' and orph == '', (ic, fk, orph))
p = cp('zero'); root = int(sq(p, "SELECT rootpage FROM sqlite_master WHERE name='pages'"))
with open(p, 'r+b') as f: f.seek((root - 1) * PS); f.write(b'\0' * PS)
ic, fk, orph = checks(p); K('B E2 zeroed table page: integrity_check is not ok', ic != ['ok'], ic)
p = cp('trunc')
with open(p, 'r+b') as f: f.truncate(os.path.getsize(p) - 3 * PS)
ic, fk, orph = checks(p); K('B E3 file truncated by 3 pages: integrity_check is not ok', ic != ['ok'], ic)
p = cp('idx'); j = scan(p, rb'title 012\d\d', True)
if j is not None: flip(p, j, 6)
ic, fk, orph = checks(p); K('B E4 flipped byte in a title_key inside the pages_title index: integrity_check is not ok', j is not None and ic != ['ok'], (j, ic))
p = cp('val'); j = open(p, 'rb').read().find(b'lorem ipsum lorem')
if j >= 0: flip(p, j)
ic, fk, orph = checks(p); n = sq(p, "SELECT count(*) FROM pages WHERE body LIKE '%korem ipsum lorem%' OR body LIKE '%morem ipsum lorem%'")
K('B E5 flipped byte inside a body value: the text changed and integrity_check is STILL ok', j >= 0 and n == '1' and ic == ['ok'], (j, ic, n))
p = cp('fk'); c = sqlite3.connect(p, isolation_level=None); c.execute('PRAGMA foreign_keys=OFF')
try: c.execute("INSERT INTO balances(holding_id,day,amount,recorded_at,source) VALUES (99999,'2026-09-30',100,'2026-09-30T10:00:00.000Z','manual')"); ins = 'inserted'
except sqlite3.Error as e: ins = f'refused: {e}'
c.close(); ic, fk, orph = checks(p)
K('B E6 orphan balance written with foreign_keys=OFF: integrity ok, foreign_key_check reports it, orphan query empty', ins == 'inserted' and ic == ['ok'] and fk != '' and orph == '', (ins, ic, fk, orph))
p = cp('orph'); c = sqlite3.connect(p, isolation_level=None)
c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES ('page',{NOW},{NOW})"); oid = c.execute('select last_insert_rowid()').fetchone()[0]; c.close()
ic, fk, orph = checks(p); K('B E7 entities row without a domain row: integrity ok, foreign_key_check empty, orphan query reports exactly it', ic == ['ok'] and fk == '' and orph == str(oid), (ic, fk, orph, oid))
shutil.rmtree(D, ignore_errors=True)

# ---- C  stale phrases in the live text
STRICT = ['export/', 'dump/', 'backups/', 'dump.tmp', 'nightly.sh', 'restore.sh', 'OFFBOX', 'restic', 'rsync', '.sha256', 'exporter', 'Exporter', 'Litestream', 'VACUUM INTO snapshots', 'drilled restore']
for tok in STRICT: K(f'C live text does not contain {tok!r}', tok not in live.replace('export/interop', ''))   # D7's 'standards matter for export/interop' is about units
K("C 'off-box' only in the §7 row that says it is out of scope", live.count('off-box') == 1, live.count('off-box'))
OK_EXPORT = ('JSON/CSV export', 'export/interop', 'FHIR export', 'a health export', 'scale-export', 'with a real export', 'Markdown export of the prose')
stale = [l for l in live.splitlines() if re.search(r'\bexport\b', l, re.I) and not any(o in l for o in OK_EXPORT)]
K('C every remaining line that says "export" is a tool feature, a standards remark, an import source, or the §7 row that puts the markdown export out of scope', not stale, stale[:3])
K('C the DDL text (comments and lifelog_meta rows) names no exporter, export/, backups/, dump/, nightly job or VACUUM INTO', not re.search(r'exporter|export/|backups?/|dump/|nightly|restore\.sh|VACUUM INTO', docsql.ddl(doc), re.I))

# ---- D  the test tree
T = os.path.join(HERE, '..')
K('D tests/backups is gone', not os.path.exists(os.path.join(T, 'backups')))
hits = []
for root, dirs, files in os.walk(T):
    dirs[:] = [x for x in dirs if x not in ('.venv', '__pycache__')]
    for f in files:
        if f in ('r12probes.py', 'r12_mutants.py') or not f.endswith(('.py', '.md', '.sh')): continue
        txt = open(os.path.join(root, f), encoding='utf-8', errors='ignore').read()
        if re.search(r'nightly\.sh|restore\.sh|OFFBOX|--slow|scripts_test', txt): hits.append(f)
K('D no test file or README refers to nightly.sh, restore.sh, --slow or the backup harness', not hits, hits)
print(f'round-12 probes: {sum(res)}/{len(res)} met expectations')
