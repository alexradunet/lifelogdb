"""The document itself (SCHEMA.md): the 2075 test of §2.7 against a fresh database; the rules live in the file (each
table's inside its CREATE statement, the cross-table ones in a few lifelog_meta rows); current truth only (no review rounds,
records, addenda, superseded notes, finding ids, versions, changelog); out-of-scope features stay out; the §3 totals match."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('document')

# ---- the 2075 test
c = fresh(); meta = dict(c.execute('select key, value from lifelog_meta'))
schema = {n: s for n, s in c.execute('select name, sql from sqlite_schema where sql is not null')}
rows = re.findall(r'^\|\s*(\d+)\s*\|([^|]+)\|([^|]+)\|([^|]+)\|\s*$', section('### 2.7 ', '## 3. '), re.M)
S.K('the 2075 table has at least 20 questions, numbered 1..n without a gap', len(rows) >= 20 and [int(r[0]) for r in rows] == list(range(1, len(rows) + 1)), [r[0] for r in rows])
used = set()
for num, q, where, must in rows:
    places = re.findall(r'`([^`]+)`', where); phrases = [p.lower() for p in re.findall(r'`([^`]+)`', must)]
    missing = [p for p in places if p not in meta and p not in schema]
    S.K(f'Q{num}: every place named is a lifelog_meta key or a schema object', places and not missing, missing)
    text = ' '.join(meta.get(p, '') + ' ' + schema.get(p, '') for p in places).lower()
    S.K(f'Q{num}: the answer says {phrases}', phrases and all(p in text for p in phrases), [p for p in phrases if p not in text])
    used |= set(places)
S.K('every lifelog_meta key answers some question (no rule without a question)', set(meta) <= used, sorted(set(meta) - used))

# ---- the rules live in the file, once
S.K('lifelog_meta holds only the few cross-table rules (at most 8 keys)', len(meta) <= 8, sorted(meta))
first = re.search(r'^(?!\s*--)\s*\S', DDL, re.M); head = DDL[:first.start()] if first else DDL
S.K('the DDL header before the first statement is a short pointer (<= 12 lines) naming lifelog_meta', head.count('\n') <= 12 and 'lifelog_meta' in head)
for t in ('entities', 'pages', 'people', 'metrics', 'measurements', 'link_kinds', 'links', 'lifelog_meta'):
    S.K(f'{t}: its CREATE statement carries its rules as comments', re.search(r'\n\s*--', schema.get(t, '')) is not None)
n = {k: one(c, f"select count(*) from sqlite_schema where type='{k}' and name not like 'sqlite_%' and not (type='table' and name like 'pages_fts_%')") for k in ('table', 'view', 'trigger')}
tot = re.search(r'\*\*(\d+) tables \+ 1 FTS5 virtual table \+ (\d+) views\*\*.*?\*\*\+ (\d+) triggers\.\*\*', DOC, re.S)
S.K('the totals under §3 match the DDL (tables, views, triggers)', tot and (int(tot.group(1)) + 1, int(tot.group(2)), int(tot.group(3))) == (n['table'], n['view'], n['trigger']), (tot and tot.groups(), n))

# ---- current truth only
FORBIDDEN = [('a pointer into a removed section (§9)', r'§9\b'), ('an addendum', r'[Aa]ddend'), ('a numbered round', r'\b[Rr]ound[ -]\d'),
             ('a finding id (R4-12, R8-01)', r'\bR\d+-\d+\b'), ('a validation record', r'[Vv]alidation record|record #\d'), ('a superseded statement', r'[Ss]uperseded'),
             ('a version narrative (v1.14)', r'\bv1\.\d+'), ('a changelog', r'Document history|[Cc]hangelog'), ('a review narrative', r'cross-review|independent review|[Rr]eview round|[Rr]eview resolutions'),
             ('a withdrawal notice', r'[Ww]ithdrawn'), ('a "first draft" story', r'\b[Ff]irst draft\b')]
def problems(text):
    out = [f'{name}: {m.group(0)!r}' for name, pat in FORBIDDEN for m in [re.search(pat, text)] if m]
    heads = re.findall(r'^## (\d+)\. (.+)$', text, re.M)
    if [int(x) for x, _ in heads] != list(range(1, 9)) or not heads or heads[-1][1] != 'References': out.append(f'sections are not 1..8 ending in References: {heads}')
    if len(re.findall(r'^\d+\. \[(.+?)\]\(#(.+?)\)$', text, re.M)) != 8: out.append('the table of contents does not have 8 entries')
    if [int(x) for x in re.findall(r'^### D(\d+) — ', text, re.M)] != list(range(1, 24)): out.append('decisions are not D1..D23 in order')
    if re.search(r'^### D\d+ — .*\*\(', text, re.M): out.append('a decision title carries a parenthetical status')
    if not re.search(r'^\*\*Status:\*\* [^\n]{10,}$', text, re.M): out.append('there is no one-line status')
    if not re.search(r'\n## Appendix A[^\n]*\n(?:(?!\n## ).)*\Z', text, re.S): out.append('the document does not end with the prior-art appendix')
    return out
p = problems(DOC)
S.K('the document has none of the marks of history', not p, p)
for name, text in [('an addendum back in D3', DOC.replace('- **Sources.** [R4][R26][R27][R75].', '- **Sources.** [R4][R26][R27][R75].\n\n- **Addendum (round 7).** x', 1)),
                   ('a finding id', DOC.replace('- **Sources.** [R58][R59].', '- **Sources.** [R58][R59]. (R4-11 e)', 1)),
                   ('a review section', DOC.replace('## 8. References', '## 8. Review resolutions\n\n## 9. References', 1)),
                   ('a version in the status line', DOC.replace('**Status:** ', '**Status:** v1.14 — ', 1)),
                   ('a superseded note', DOC.replace('### D4 — Text ownership: the database is canonical.', '### D4 — Text ownership: the database is canonical. *(Superseded by D5.)*', 1)),
                   ('a decision deleted', DOC.replace('### D7 — ', '### DX — ', 1))]:
    q = problems(text); S.K(f'a broken copy is noticed: {name}', q and q != p, q[:1])

# ---- out of scope stays out (§7): export, snapshots, dumps
for tok in ('export/', 'dump/', 'backups/', 'nightly.sh', 'restore.sh', 'restic', 'rsync', 'Litestream', 'drilled restore', 'exporter'):
    S.K(f'the live text does not contain {tok!r}', tok not in LIVE)
S.K("'off-box' only in the §7 row that puts it out of scope", LIVE.count('off-box') == 1)
S.K('the DDL names no exporter, export/, backups/, dump/ or nightly job', not re.search(r'exporter|export/|backups?/|dump/|nightly|restore\.sh', DDL, re.I))
T = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..')
hits = [f for r, ds, fs in os.walk(T) if '.venv' not in r for f in fs if f.endswith(('.py', '.md')) and f != 'document.py'
        and re.search(r'nightly\.sh|restore\.sh|OFFBOX|scripts_test', open(os.path.join(r, f), encoding='utf-8', errors='ignore').read())]
S.K('no test file refers to a backup harness', not hits, hits)
S.K('no test file is named after a review round', not [f for r, ds, fs in os.walk(T) if '.venv' not in r for f in fs if re.match(r'r\d+', f)])
S.done()
