"""Diagram checks (run:  python3 diagrams.py DDLFILE).  Expected outcome is in each label.
The mermaid diagrams of SCHEMA.md are part of the document, so they must say what the DDL says (round 13).
A  Structure: exactly the nine diagrams, each announced by a `%% diagram: <id>` line, each of a known type.
B  The two ER diagrams against the DDL: every table is drawn; every drawn column exists with the declared type; PK / FK marks
   equal the real keys; every foreign key is a relationship and every relationship is a foreign key, with the label the FK column
   and the cardinality symbols the constraint implies (NOT NULL -> `||`, nullable -> `|o`; unique in the child -> `o|`, else `o{`).
C  The link map against `link_kinds`: same edges, same symmetric arrows, `any entity` for a NULL endpoint list.
D  The correction story of section 2.4, executed: what `measurement_values` shows after each of the four inserts.
E  The save flow and section 6.14 name the same steps; the memo and page state diagrams use only states the DDL can be in."""
import os, re, sqlite3, sys
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
DDL = open(sys.argv[1], encoding='utf-8').read()
doc = docsql.doc_text()
res = []
def K(label, cond, detail=''):
    res.append(bool(cond))
    if not cond: print('  FAIL', label, detail)

# ---- A
blocks = re.findall(r'```mermaid\n(.*?)\n```', doc, re.S)
byid = {}
for b in blocks:
    m = re.match(r'%% diagram: ([a-z-]+)\n', b)
    K('A every mermaid block starts with a `%% diagram: id` line', m is not None, b[:40])
    if m: byid.setdefault(m.group(1), []).append(b[m.end():])
WANT = {'er-core': 'erDiagram', 'er-facts': 'erDiagram', 'link-map': 'flowchart', 'memo-life': 'stateDiagram-v2', 'page-life': 'stateDiagram-v2',
        'correct-measurement': 'stateDiagram-v2', 'writers': 'flowchart', 'money-flow': 'flowchart', 'save-flow': 'flowchart'}
K('A exactly the nine diagrams, each once', sorted(byid) == sorted(WANT) and all(len(v) == 1 for v in byid.values()), {k: len(v) for k, v in byid.items()})
for k, kind in WANT.items():
    if k in byid: K(f'A {k} is a {kind}', byid[k][0].startswith(kind), byid[k][0][:30])
def body(k): return byid.get(k, [''])[0]

# ---- B  ER diagrams vs the DDL
c = sqlite3.connect(':memory:'); c.executescript(DDL)
tables = [t for (t,) in c.execute("select name from sqlite_master where type='table' and name not like 'sqlite_%' and name not like 'pages_fts%'")]
info = {t: {r[1]: dict(type=r[2], notnull=r[3], pk=r[5]) for r in c.execute(f"pragma table_info('{t}')")} for t in tables}
fk_cols = {t: set() for t in tables}; fks = []          # (child, parent, from_cols, to_cols)
for t in tables:
    groups = {}
    for r in c.execute(f"pragma foreign_key_list('{t}')"): groups.setdefault(r[0], []).append(r)
    for rows in groups.values():
        rows.sort(key=lambda r: r[1]); cols = [r[3] for r in rows]
        fks.append((t, rows[0][2], cols)); fk_cols[t] |= set(cols)
def unique_sets(t):
    sets = [{c_ for c_, v in info[t].items() if v['pk']}]
    for r in c.execute(f"pragma index_list('{t}')"):
        if r[2]: sets.append({x[2] for x in c.execute(f"pragma index_info('{r[1]}')")})
    return [s for s in sets if s]
ents, rels = {}, []
for k in ('er-core', 'er-facts'):
    for m in re.finditer(r'^    (\w+) \{\n(.*?)^    \}', body(k), re.S | re.M):
        name = m.group(1); attrs = []
        for line in m.group(2).splitlines():
            a = re.fullmatch(r'\s+(\w+) (\w+)(?: ((?:PK|FK|UK)(?:, (?:PK|FK|UK))*))?(?: "[^"]*")?', line)
            K(f'B {k}.{name}: attribute line parses: {line.strip()}', a is not None)
            if a: attrs.append((a.group(2), a.group(1), set((a.group(3) or '').split(', ')) - {''}))   # mermaid writes `TYPE name`
        ents.setdefault(name, []).append((k, attrs))
    for m in re.finditer(r'^    (\w+)\s+(\|\||\|o)--(o\||o\{)\s+(\w+)\s+: "(\w+)"', body(k), re.M):
        rels.append((k, m.group(1), m.group(2), m.group(3), m.group(4), m.group(5)))
K('B every table of the DDL is drawn in some ER diagram', set(tables) <= set(ents), sorted(set(tables) - set(ents)))
K('B every drawn entity is a table of the DDL', set(ents) <= set(tables), sorted(set(ents) - set(tables)))
for name, drawings in ents.items():
    if name not in info: continue
    for k, attrs in drawings:
        K(f'B {k}.{name} lists each column at most once', len({a[0] for a in attrs}) == len(attrs))
        for col, typ, marks in attrs:
            ok = col in info[name]
            K(f'B {k}.{name}.{col} is a column of {name}', ok)
            if not ok: continue
            K(f'B {k}.{name}.{col} has the declared type ({info[name][col]["type"]})', typ == info[name][col]['type'], typ)
            K(f'B {k}.{name}.{col} PK mark = primary key', ('PK' in marks) == bool(info[name][col]['pk']), marks)
            K(f'B {k}.{name}.{col} FK mark = part of a foreign key', ('FK' in marks) == (col in fk_cols[name]), marks)
# relationships <-> foreign keys
drawn = {}
for k, parent, ps, cs, child, label in rels: drawn.setdefault((parent, child, label), []).append((ps, cs))
want = {}
for child, parent, cols in fks: want[(parent, child, cols[0])] = (cols, child)
K('B every foreign key of the DDL is drawn as `parent --- child : first FK column`', set(want) <= set(drawn), sorted(set(want) - set(drawn)))
K('B every drawn relationship is a foreign key of the DDL', set(drawn) <= set(want), sorted(set(drawn) - set(want)))
for key, (cols, child) in want.items():
    if key not in drawn: continue
    nullable = any(not info[child][x]['notnull'] and not info[child][x]['pk'] for x in cols)
    uniq = any(s <= set(cols) for s in unique_sets(child))
    exp = ('|o' if nullable else '||', 'o|' if uniq else 'o{')
    K(f'B {key[0]} -> {key[1]} ({key[2]}): symbols {exp} as the constraint implies', exp in drawn[key], (drawn[key], exp))
K('B er-core draws entities -> each of the six domain tables (id)', all(('entities', t, 'id') in drawn for t in ('pages', 'events', 'tasks', 'people', 'places', 'accounts')))

# ---- C  the link map
edges = {}
for m in re.finditer(r'^    (\w+) (-->|<-->)\|"([^"]+)"\| (\w+)', body('link-map'), re.M):
    edges.setdefault((m.group(1), m.group(4), m.group(2) == '<-->'), set()).update(x.strip() for x in m.group(3).split(','))
exp = {}
for kind, sym, ft, tt in c.execute('select kind, symmetric, from_types, to_types from link_kinds'):
    for f in (ft.split(',') if ft else ['any']):
        for t in (tt.split(',') if tt else ['any']): exp.setdefault((f, t, bool(sym)), set()).add(kind)
K('C the link map has the same edges as link_kinds (from, to, symmetric) -> kinds', edges == exp, {'only in map': {k: v for k, v in edges.items() if exp.get(k) != v}, 'only in DDL': {k: v for k, v in exp.items() if edges.get(k) != v}})
K('C every node of the map is an entity type or `any`', {n for e in edges for n in e[:2]} <= {'any', 'page', 'event', 'task', 'person', 'place', 'account'})

# ---- D  the correction story, executed
NOW = "'2026-09-30T10:00:00.000Z'"
c = sqlite3.connect(':memory:', isolation_level=None); c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA recursive_triggers=ON'); c.executescript(DDL)
c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); mid = c.execute("select id from metrics where name='weight'").fetchone()[0]
def ins(v, sup=None): c.execute(f"INSERT INTO measurements(metric_id,day,value,supersedes_id,recorded_at) VALUES (?, '2026-09-30', ?, ?, {NOW})", (mid, v, sup)); return c.execute('select last_insert_rowid()').fetchone()[0]
def shown(): return [r[0] for r in c.execute('select value from measurement_values where metric_id=?', (mid,))]
r1 = ins(71.2); s1 = shown(); r2 = ins(70.8, r1); s2 = shown(); r3 = ins(None, r2); s3 = shown(); r4 = ins(71.4, r3); s4 = shown()
K('D the story: rows 1-4 get ids 1-4 in a fresh database', (r1, r2, r3, r4) == (1, 2, 3, 4), (r1, r2, r3, r4))
K('D after row 1 the view shows 71.2; after row 2 70.8; after row 3 (NULL) nothing; after row 4 71.4', (s1, s2, s3, s4) == ([71.2], [70.8], [], [71.4]), (s1, s2, s3, s4))
cm = body('correct-measurement')
K('D the diagram tells that story: four inserts, their values and supersedes, in order',
  re.findall(r'INSERT row (\d), value (\S+?)(?:, supersedes (\d))?\n', cm + '\n') == [('1', '71.2', ''), ('2', '70.8', '1'), ('3', 'NULL', '2'), ('4', '71.4', '3')], re.findall(r'INSERT row[^\n]*', cm))
K('D ...and the four states it shows', re.findall(r'state "([^"]+)" as V\d', cm) == ['view shows 71.2', 'view shows 70.8', 'view shows nothing (retracted)', 'view shows 71.4'])

# ---- E
sql = re.search(r'### 6\.14 .*?```sql\n(.*?)\n```', doc, re.S).group(1)
steps_sql = set(re.findall(r'^-- (0|1|2a|2b|3|4)\)', sql, re.M))
steps_fig = set(re.findall(r'\b(0|1|2a|2b|3|4)\. ', body('save-flow')))
K('E the save flow names steps 0, 1, 2a, 2b, 3, 4 exactly as the SQL of 6.14 does', steps_sql == steps_fig == {'0', '1', '2a', '2b', '3', '4'}, (steps_sql, steps_fig))
K('E the save flow has one BEGIN IMMEDIATE, one SAVEPOINT / RELEASE / ROLLBACK TO, one COMMIT — as 6.14', all(w in body('save-flow') for w in ('BEGIN IMMEDIATE', 'SAVEPOINT target', 'RELEASE target', 'ROLLBACK TO target', 'COMMIT')))
K('E the memo diagram uses the three states of a memo: inbox (triaged_at NULL), triaged, tombstoned', re.findall(r'(\w+) --> (\w+)', body('memo-life')) == [('Inbox', 'Triaged'), ('Inbox', 'Triaged'), ('Inbox', 'Tombstoned'), ('Triaged', 'Tombstoned')] or set(re.findall(r'(?:--> |^    )(Inbox|Triaged|Tombstoned)', body('memo-life'), re.M)) == {'Inbox', 'Triaged', 'Tombstoned'})
K('E the writers diagram names the settings it relies on: BEGIN IMMEDIATE, WAL, mode=ro', all(w in body('writers') for w in ('BEGIN IMMEDIATE', 'WAL', 'mode=ro')))
K('E the money flow names the tables it reads: balances, accounts, fx_rates, currencies, balance_values', all(w in body('money-flow') for w in ('balances', 'accounts', 'fx_rates', 'currencies', 'balance_values')) and all(w in tables or w == 'balance_values' for w in ('balances', 'accounts', 'fx_rates', 'currencies')))
print(f'diagram checks: {sum(res)}/{len(res)} met expectations')
