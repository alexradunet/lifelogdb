"""Round-10 probes (run:  python3 r10probes.py DDLFILE).  Expected outcome is in each label.
A  R8-01: the title CHECK rejects a Windows device name bare OR before an extension, and the six superscript names.
B  The "2075 test" of SCHEMA.md §2.11: every question in the doc's table is answered, from `lifelog_meta` alone.
C  The deferred features of §7 have a working additive path (executed, not assumed):
   C1 partial dates (R4-18)  C2 tokenizer switch for CJK search (R4-15).
D  The orphan query of SCHEMA.md 2.8 finds an entities row without a domain row and is silent on a clean database.
E  The import path of §2.11, run from the document's own SQL on 1 000 rows: idempotent, all-or-nothing, and the three traps are real."""
exec(open('probes1.py').read().split('# ---- P1:')[0])
import re
res = []
def K(label, cond, detail=''):
    res.append(bool(cond))
    if not cond: print('  FAIL', label, detail)
def title_accepted(c, t):
    from unicodedata import normalize
    key = normalize('NFC', normalize('NFC', t).casefold()) if t else ''
    c.execute('SAVEPOINT x')
    try:
        i = ent(c, 'page'); c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES (?, 'page', ?, ?)", (i, t, key)); r = True
    except sqlite3.Error: r = False
    c.execute('ROLLBACK TO x'); c.execute('RELEASE x'); return r

# ---- A
c = fresh()
for t in ['CONSOLE', 'CONSOLE.txt', 'a.CON', 'x.NUL', 'COM10', 'com10.x', 'LPT0', 'Com0.x', 'CON1', 'my.con.note', 'Aux ' + 'x']:
    K(f'A accepts {t!r}', title_accepted(c, t))
for t in ['CON', 'con.txt', 'CON.backup', 'NUL.tar.gz', 'Prn.a.b', 'aux.x', 'COM1', 'com9.txt', 'LPT1.md', 'lpt9.a.b',
          'COM¹', 'com².x', 'COM³', 'LPT¹', 'lpt².txt', 'LPT³.a.b', 'CON.', '.CON', 'CON ', ' CON']:
    K(f'A rejects {t!r}', not title_accepted(c, t))

# ---- B
doc = open(DOCPATH, encoding='utf-8').read()
s = doc.index('### 2.11 '); e = doc.index('### 2.12 ') if '### 2.12 ' in doc else doc.index('## 3. The schema')
rows = re.findall(r'^\|\s*(\d+)\s*\|([^|]+)\|([^|]+)\|([^|]+)\|\s*$', doc[s:e], re.M)
K('B the §2.11 table lists the questions', len(rows) >= 15, len(rows))
meta = dict(c.execute('select key, value from lifelog_meta').fetchall())
link_kinds = c.execute('select count(*) from link_kinds').fetchone()[0]
for num, q, keys, must in rows:
    ks = re.findall(r'`([^`]+)`', keys); phrases = [p.strip().lower() for p in re.findall(r'`([^`]+)`', must)]
    text = ' '.join(meta.get(k, '') for k in ks).lower()
    K(f'B Q{num} keys exist: {ks}', ks and all(k in meta or k == 'link_kinds' for k in ks), [k for k in ks if k not in meta])
    K(f'B Q{num} answer mentions {phrases}', all(p in text for p in phrases), text[:120])
K('B the closed link-kind registry is itself a table a stranger can read', link_kinds >= 12)
used = {k for _, _, keys, _ in rows for k in re.findall(r'`([^`]+)`', keys)}
K('B every lifelog_meta key is used by some question (no rule without a question)', set(meta) <= used, sorted(set(meta) - used))
K('B the table has one row per question, numbered 1..n without a gap', [int(r[0]) for r in rows] == list(range(1, len(rows) + 1)), [r[0] for r in rows])

# ---- C1 partial dates
c = fresh(); p1 = ent(c, 'person'); c.execute("INSERT INTO people(id,name,birth_day) VALUES (?, 'Ada', '1815-12-10')", (p1,))
p2 = ent(c, 'person'); c.execute("INSERT INTO people(id,name) VALUES (?, 'Ancestor')", (p2,))
K('C1 birth_day rejects 1870 and 1870-05 today', tryx(c, "UPDATE people SET birth_day='1870' WHERE id=?", (p2,))[0] == 'ERR' and tryx(c, "UPDATE people SET birth_day='1870-05' WHERE id=?", (p2,))[0] == 'ERR')
# round 15: every CHECK is named, so the §7 path is DROP + ADD of people_birth_day (it was an extra column while the CHECK had no name)
K('C1 a guessed name is not the constraint (names are <table>_<column>)', tryx(c, "ALTER TABLE people DROP CONSTRAINT birth_day")[0] == 'ERR')
c.execute('BEGIN')
r1 = tryx(c, "ALTER TABLE people DROP CONSTRAINT people_birth_day")
r2 = tryx(c, "ALTER TABLE people ADD CONSTRAINT people_birth_day CHECK (birth_day IS NULL OR date(birth_day) IS birth_day OR birth_day GLOB '[0-9][0-9][0-9][0-9]' OR (birth_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]' AND substr(birth_day, 6, 2) BETWEEN '01' AND '12'))")
c.execute('COMMIT')
K('C1 the named CHECK is dropped and re-added looser on the populated STRICT table', r1[0] == 'OK' and r2[0] == 'OK', (r1, r2))
K('C1 existing full dates are untouched', c.execute("select birth_day from people where id=?", (p1,)).fetchone()[0] == '1815-12-10')
K('C1 YYYY and YYYY-MM are stored', tryx(c, "UPDATE people SET birth_day='1870' WHERE id=?", (p2,))[0] == 'OK' and tryx(c, "UPDATE people SET birth_day='1870-05' WHERE id=?", (p2,))[0] == 'OK')
K('C1 junk and month 13 are rejected', all(tryx(c, "UPDATE people SET birth_day=? WHERE id=?", (v, p2))[0] == 'ERR' for v in ('abc', '1870-13', '1870-5', '18700', '1870-05-01x')))
K('C1 integrity_check and foreign_key_check stay clean', c.execute('pragma integrity_check').fetchone()[0] == 'ok' and c.execute('pragma foreign_key_check').fetchall() == [])
K('C1 the people triggers still work (updated_at bumps on update)', c.execute("select updated_at >= created_at from entities where id=?", (p2,)).fetchone()[0] == 1)
K('C1 a full date still goes in birth_day', tryx(c, "UPDATE people SET birth_day='1870-05-03' WHERE id=?", (p2,))[0] == 'OK')

# ---- C2 tokenizer switch (derived index: drop, recreate, rebuild)
c = fresh(); jp = '日本語のノートを書く'
for body in (jp, 'Zürich café notes', 'plain english text'):
    memo(c, body)
def hits(q): return c.execute("select count(*) from pages_fts where pages_fts match ?", (q,)).fetchone()[0]
K('C2 before: unicode61 finds the whole CJK run and accented words, not a part of the run', hits(jp) == 1 and hits('zurich') == 1 and hits('本語') == 0 and hits('ノート') == 0, (hits(jp), hits('zurich'), hits('本語'), hits('ノート')))
c.execute('BEGIN IMMEDIATE'); c.execute('DROP TABLE pages_fts')
c.execute("CREATE VIRTUAL TABLE pages_fts USING fts5(title, body, content='pages', content_rowid='id', tokenize='trigram remove_diacritics 1')")
c.execute("INSERT INTO pages_fts(pages_fts) VALUES('rebuild')"); c.execute('COMMIT')
K('C2 after the switch: trigram finds 3+-character parts of the CJK run and folds accents', hits('本語の') == 1 and hits('ノート') == 1 and hits('日本語') == 1 and hits('zurich') == 1, (hits('本語の'), hits('ノート'), hits('日本語'), hits('zurich')))
K('C2 ...but a two-character word (京都, 本語) is still not found — the known limit', hits('本語') == 0)
K('C2 the sync triggers keep working after the switch (insert, update, delete)', True)
m = memo(c, 'これは新しい記録です'); n1 = hits('新しい')
c.execute("UPDATE pages SET body='全く別の内容' WHERE id=?", (m,)); n2 = (hits('新しい'), hits('別の内'))
K('C2 insert indexed, update re-indexed', n1 == 1 and n2 == (0, 1), (n1, n2))
K('C2 the FTS integrity-check passes after the switch', tryx(c, "INSERT INTO pages_fts(pages_fts, rank) VALUES('integrity-check', 1)")[0] == 'OK')

# ---- D orphan check (the query of section 2.8, taken from the document text)
m = re.search(r"SELECT id FROM entities WHERE id NOT IN \(SELECT id FROM pages UNION SELECT id FROM events UNION SELECT id FROM tasks UNION SELECT id FROM people UNION SELECT id FROM places UNION SELECT id FROM holdings\)", doc)
K('D the orphan query is in the document (2.8)', m is not None)
c = fresh(); memo(c, 'x')
K('D clean database: the orphan query returns nothing', m and c.execute(m.group(0)).fetchall() == [])
o = ent(c, 'page')
K('D an entities row without a domain row is found', m and c.execute(m.group(0)).fetchall() == [(o,)])

# ---- E imports (§2.11), executed from the document text
import subprocess, tempfile, shutil, csv as _csv
sec = doc[doc.index('### 2.11 '):doc.index('## 3. The schema')]
IMP = [b for b in re.findall(r'```sql\n(.*?)\n```', sec, re.S) if "ATTACH 'scratch.db' AS s;" in b][0]
work = tempfile.mkdtemp(prefix='lifelog-import-')
def life(path):
    c = sqlite3.connect(path, isolation_level=None); c.executescript(DDL); c.execute('PRAGMA foreign_keys=ON')
    c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); return c
def stage(rows, header=('id', 'day', 'taken_at', 'tz', 'value')):
    with open(f'{work}/weights.csv', 'w', newline='') as f:
        w = _csv.writer(f); w.writerow(header); w.writerows(rows)
    if os.path.exists(f'{work}/scratch.db'): os.remove(f'{work}/scratch.db')
    subprocess.run(['sqlite3', f'{work}/scratch.db', '.import --csv weights.csv staging'], cwd=work, check=True, stdin=subprocess.DEVNULL)
def run(c, sql=None):
    cwd = os.getcwd(); os.chdir(work)
    try: c.executescript(sql or IMP); return 'OK'
    except sqlite3.Error as e:
        try: c.execute('ROLLBACK')
        except sqlite3.Error: pass
        try: c.execute('DETACH s')
        except sqlite3.Error: pass
        return 'ERR ' + str(e)
    finally: os.chdir(cwd)
def n(c): return c.execute("select count(*) from measurements where source='scale-export'").fetchone()[0]
rows = [(f'w{i}', f'2025-{1 + i % 12:02d}-{1 + i % 28:02d}', '' if i % 5 == 0 else f'2025-{1 + i % 12:02d}-{1 + i % 28:02d}T07:30:00.000Z', '' if i % 5 == 0 else 'Europe/Berlin', f'{60 + (i % 50) / 10}') for i in range(1000)]
c = life(f'{work}/life.db'); stage(rows)
K('E1 the document\'s import block loads 1 000 rows', run(c) == 'OK' and n(c) == 1000, (run(c), n(c)))
K('E1 empty CSV cells became NULL through NULLIF (200 rows without taken_at/tz)', c.execute("select count(*) from measurements where source='scale-export' and taken_at is null and tz is null").fetchone()[0] == 200)
K('E2 running it again inserts nothing (idempotent)', run(c) == 'OK' and n(c) == 1000)
K('E2 the values are REAL, converted by the STRICT column', c.execute("select count(*) from measurements where source='scale-export' and typeof(value)='real'").fetchone()[0] == 1000)
stage(rows[:10] + [(f'new{i}', '2026-01-02', '', '', '70.5') for i in range(10)])
K('E3 10 duplicate keys and 10 new rows: exactly the 10 new ones are inserted', run(c) == 'OK' and n(c) == 1010, n(c))
before = n(c)
stage([(f'b{i}', '2026-02-01', '', '', '71') for i in range(5)] + [('bad', '2026-02-01', '', '', 'abc')])
r = run(c); K("E4 one value 'abc' in the batch: the whole batch is rejected and nothing of it is inserted", r.startswith('ERR') and n(c) == before, (r[:80], n(c)))
stage([(f'c{i}', '2026-02-01', '', '', '71') for i in range(5)] + [('empty', '2026-02-01', '', '', '')])
r = run(c); K("E4 an empty value cell is rejected too (not stored as 0.0)", r.startswith('ERR') and n(c) == before, (r[:80], n(c)))
stage([(f'd{i}', '2026-02-01', '', '', '71') for i in range(5)] + [('day', '2026-2-1', '', '', '71')])
r = run(c); K('E4 a malformed day is NOT swallowed by ON CONFLICT DO NOTHING: the batch is rejected', r.startswith('ERR') and 'CHECK' in r and n(c) == before, (r[:90], n(c)))
stage([('t1', '2026-03-01', '', '', '70')])
r = run(c, IMP.replace("NULLIF(taken_at, '')", 'taken_at')); K("E5 without NULLIF an empty taken_at ('' is not NULL) fails its CHECK", r.startswith('ERR') and 'CHECK' in r, r[:90])
stage([('t2', '2026-03-01', '', '', '70')])
r = run(c, IMP.replace('  FROM s.staging WHERE true', '  FROM s.staging')); K('E5 without WHERE true the statement is rejected (SQLite reads ON CONFLICT as a join\'s ON)', r.startswith('ERR') and ('JOIN' in r or 'syntax' in r), r[:90])
r = run(c, IMP.replace(' WHERE import_id IS NOT NULL DO NOTHING', ' DO NOTHING')); K('E5 a conflict target without the index\'s WHERE does not match the partial unique index', r.startswith('ERR') and 'does not match' in r, r[:110])
cst = c.execute("select cast('abc' as real), cast('' as real), cast('12.5kg' as real)").fetchone()
K("E5 CAST hides garbage: 'abc' -> 0.0, '' -> 0.0, '12.5kg' -> 12.5", cst == (0.0, 0.0, 12.5), cst)
stage([('cast1', '2026-03-05', '', '', 'abc')]); r = run(c, IMP.replace('NULLIF(tz, \'\'), value,', "NULLIF(tz, ''), CAST(value AS REAL),"))
K("E5 with CAST the bad value 'abc' would be stored as 0.0 (the trap, shown)", r == 'OK' and c.execute("select value from measurements where import_id='cast1'").fetchone()[0] == 0.0, r[:80])
K('E6 afterwards: integrity_check ok, foreign_key_check empty, no orphan entities row',
  c.execute('pragma integrity_check').fetchone()[0] == 'ok' and c.execute('pragma foreign_key_check').fetchall() == [] and m is not None and c.execute(m.group(0)).fetchall() == [])
pc = c.execute("SELECT source, count(*), min(day), max(day) FROM measurements WHERE source='scale-export' GROUP BY source").fetchall()
K('E6 the per-source count query of §2.11 answers', len(pc) == 1 and pc[0][1] >= 1010, pc)
shutil.rmtree(work, ignore_errors=True)

print(f'round-10 probes: {sum(res)}/{len(res)} met expectations')
