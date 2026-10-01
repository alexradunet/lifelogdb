"""The mermaid diagrams of SCHEMA.md say what the DDL says.
A  structure: exactly the eight diagrams, each announced by a `%% diagram: <id>` line, each of a known type;
B  the two ER diagrams draw keys only: every table is drawn; every drawn column is a PK or FK column with its declared type
   and marks; every PK and FK column is drawn somewhere; every foreign key is a relationship and every relationship a foreign
   key, labelled by its first column, with the cardinality the constraint implies (NOT NULL `||`, nullable `|o`; unique in
   the child `o|`, else `o{`);
C  the link map against `link_kinds`: same edges (a node naming several types stands for each), same symmetric arrows;
D  the correction story of §2.3, executed;
E  the save flow and §6.13 name the same steps; the other diagrams name what they rely on."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('diagrams')

# ---- A
byid = {}
for b in re.findall(r'```mermaid\n(.*?)\n```', DOC, re.S):
    m = re.match(r'%% diagram: ([a-z-]+)\n', b)
    S.K('every mermaid block starts with a `%% diagram: id` line', m is not None, b[:40])
    if m: byid.setdefault(m.group(1), []).append(b[m.end():])
WANT = {'er-core': 'erDiagram', 'er-facts': 'erDiagram', 'link-map': 'flowchart', 'page-life': 'stateDiagram-v2',
        'correct-measurement': 'stateDiagram-v2', 'writers': 'flowchart', 'money-flow': 'flowchart', 'save-flow': 'flowchart'}
S.K('exactly the eight diagrams, each once', sorted(byid) == sorted(WANT) and all(len(v) == 1 for v in byid.values()), {k: len(v) for k, v in byid.items()})
for k, kind in WANT.items():
    if k in byid: S.K(f'{k} is a {kind}', byid[k][0].startswith(kind))
def body(k): return byid.get(k, [''])[0]

# ---- B
c = sqlite3.connect(':memory:'); c.executescript(DDL)
tables = [t for (t,) in c.execute("select name from sqlite_master where type='table' and name not like 'sqlite_%' and name not like 'pages_fts%'")]
info = {t: {r[1]: dict(type=r[2], notnull=r[3], pk=r[5]) for r in c.execute(f"pragma table_info('{t}')")} for t in tables}
fk_cols = {t: set() for t in tables}; fks = []
for t in tables:
    groups = {}
    for r in c.execute(f"pragma foreign_key_list('{t}')"): groups.setdefault(r[0], []).append(r)
    for rows in groups.values():
        rows.sort(key=lambda r: r[1]); cols = [r[3] for r in rows]; fks.append((t, rows[0][2], cols)); fk_cols[t] |= set(cols)
def unique_sets(t):
    sets = [{x for x, v in info[t].items() if v['pk']}]
    for r in c.execute(f"pragma index_list('{t}')"):
        if r[2]: sets.append({x[2] for x in c.execute(f"pragma index_info('{r[1]}')")})
    return [s for s in sets if s]
ents, rels, drawn_cols = {}, [], {t: set() for t in tables}
for k in ('er-core', 'er-facts'):
    for m in re.finditer(r'^    (\w+) \{\n(.*?)^    \}', body(k), re.S | re.M):
        attrs = []
        for line in m.group(2).splitlines():
            a = re.fullmatch(r'\s+(\w+) (\w+)(?: ((?:PK|FK|UK)(?:, (?:PK|FK|UK))*))?(?: "[^"]*")?', line)
            S.K(f'{k}.{m.group(1)}: attribute line parses: {line.strip()}', a is not None)
            if a: attrs.append((a.group(2), a.group(1), set((a.group(3) or '').split(', ')) - {''}))
        ents.setdefault(m.group(1), []).append((k, attrs))
    for m in re.finditer(r'^    (\w+)\s+(\|\||\|o)--(o\||o\{)\s+(\w+)\s+: "(\w+)"', body(k), re.M):
        rels.append((m.group(1), m.group(2), m.group(3), m.group(4), m.group(5)))
S.K('every table of the DDL is drawn', set(tables) <= set(ents), sorted(set(tables) - set(ents)))
S.K('every drawn entity is a table of the DDL', set(ents) <= set(tables), sorted(set(ents) - set(tables)))
for name, drawings in ents.items():
    if name not in info: continue
    for k, attrs in drawings:
        S.K(f'{k}.{name} lists each column at most once', len({a[0] for a in attrs}) == len(attrs))
        for col, typ, marks in attrs:
            if not S.K(f'{k}.{name}.{col} is a column of {name}', col in info[name]): continue
            drawn_cols[name].add(col)
            S.K(f'{k}.{name}.{col} is a key column (the diagrams draw keys only)', info[name][col]['pk'] or col in fk_cols[name])
            S.K(f'{k}.{name}.{col} has the declared type', typ == info[name][col]['type'], typ)
            S.K(f'{k}.{name}.{col} PK mark = primary key', ('PK' in marks) == bool(info[name][col]['pk']), marks)
            S.K(f'{k}.{name}.{col} FK mark = part of a foreign key', ('FK' in marks) == (col in fk_cols[name]), marks)
for t in tables:
    keys = {x for x, v in info[t].items() if v['pk']} | fk_cols[t]
    S.K(f'every key column of {t} is drawn somewhere', keys <= drawn_cols[t], sorted(keys - drawn_cols[t]))
drawn = {}
for parent, ps, cs, child, label in rels: drawn.setdefault((parent, child, label), []).append((ps, cs))
want = {(parent, child, cols[0]): (cols, child) for child, parent, cols in fks}
S.K('every foreign key is drawn as `parent --- child : first FK column`', set(want) <= set(drawn), sorted(set(want) - set(drawn)))
S.K('every drawn relationship is a foreign key', set(drawn) <= set(want), sorted(set(drawn) - set(want)))
for key, (cols, child) in want.items():
    if key not in drawn: continue
    nullable = any(not info[child][x]['notnull'] and not info[child][x]['pk'] for x in cols)
    exp = ('|o' if nullable else '||', 'o|' if any(s <= set(cols) for s in unique_sets(child)) else 'o{')
    S.K(f'{key[0]} -> {key[1]} ({key[2]}): cardinality {exp}', exp in drawn[key], (drawn[key], exp))

# ---- C  the link map
nodes = {m.group(1): m.group(2) for m in re.finditer(r'^    (\w+)\(?\["?([^"\]]+)"?\]\)?$', body('link-map'), re.M)}
types = {n: (['any'] if lab == 'any entity' else [x.strip() for x in lab.split(',')]) for n, lab in nodes.items()}
edges = {}
for m in re.finditer(r'^    (\w+) (-->|<-->)\|"([^"]+)"\| (\w+)', body('link-map'), re.M):
    for f in types.get(m.group(1), ['?' + m.group(1)]):
        for t in types.get(m.group(4), ['?' + m.group(4)]):
            edges.setdefault((f, t, m.group(2) == '<-->'), set()).update(x.strip() for x in m.group(3).split(','))
exp = {}
for kind, sym, ft, tt in c.execute('select kind, symmetric, from_types, to_types from link_kinds'):
    for f in (ft.split(',') if ft else ['any']):
        for t in (tt.split(',') if tt else ['any']): exp.setdefault((f, t, bool(sym)), set()).add(kind)
S.K('the link map has the same edges as link_kinds', edges == exp, {'only in map': {k: v for k, v in edges.items() if exp.get(k) != v}, 'only in DDL': {k: v for k, v in exp.items() if edges.get(k) != v}})
S.K('every node of the map names entity types or `any entity`', all(set(v) <= {'any', 'page', 'task', 'person', 'place', 'holding'} for v in types.values()), types)

# ---- D  the correction story
cm = body('correct-measurement')
steps = re.findall(r'INSERT row (\d), value (\S+?)(?:, supersedes (\d))?\n', cm + '\n')
states = re.findall(r'state "view shows ([^"]+)" as V\d', cm)
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); mid = one(c, "select id from metrics where name='weight'")
shown = []
for n, v, s in steps:
    measure(c, mid, '2026-09-30', None if v == 'NULL' else float(v), supersedes_id=int(s) if s else None)
    shown.append(', '.join(str(r[0]) for r in c.execute('select value from measurement_values where metric_id=?', (mid,))) or 'nothing (retracted)')
S.K('the story has four inserts', [x[0] for x in steps] == ['1', '2', '3', '4'], steps)
S.K('executed, the view shows what each state of the diagram says', shown == states, (shown, states))

# ---- E
sql = block('6.13')
st_sql = set(re.findall(r'^-- (0|1|2a|2b|3|4)\)', sql, re.M)); st_fig = set(re.findall(r'\b(0|1|2a|2b|3|4)\. ', body('save-flow')))
S.K('the save flow names steps 0, 1, 2a, 2b, 3, 4 exactly as §6.13 does', st_sql == st_fig == {'0', '1', '2a', '2b', '3', '4'}, (st_sql, st_fig))
S.K('the save flow has BEGIN IMMEDIATE, SAVEPOINT, RELEASE, ROLLBACK TO and COMMIT, as §6.13', all(w in body('save-flow') for w in ('BEGIN IMMEDIATE', 'SAVEPOINT target', 'RELEASE target', 'ROLLBACK TO target', 'COMMIT')))
S.K('the page diagram has the named state, promotion by type and the day page', 'Named' in body('page-life') and 'entities.entity_type' in body('page-life') and 'day page' in body('page-life'))
S.K('the writers diagram names BEGIN IMMEDIATE, WAL, mode=ro', all(w in body('writers') for w in ('BEGIN IMMEDIATE', 'WAL', 'mode=ro')))
S.K('the money flow names balances, holdings, currencies, balance_values and sums per currency, with no conversion',
    all(w in body('money-flow') for w in ('balances', 'holdings', 'currencies', 'balance_values', 'per currency')) and 'fx' not in body('money-flow'))
S.done()
