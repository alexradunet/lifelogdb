"""The docs themselves (docs/): the 2075 test of the threat model against a fresh database; the rules live in the file (each
table's inside its CREATE statement, the cross-table ones in a few lifelog_meta rows); the tree holds together (every decision
D1..Dn in its own record, every relative link resolves, every page is reachable from docs/README.md); the totals in
schema/README.md match."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('document')

# ---- the 2075 test
c = fresh(); meta = dict(c.execute('select key, value from lifelog_meta'))
schema = {n: s for n, s in c.execute('select name, sql from sqlite_schema where sql is not null')}
rows = re.findall(r'^\|\s*(\d+)\s*\|([^|]+)\|([^|]+)\|([^|]+)\|\s*$', doc_page('contract/threat-model.md'), re.M)
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
S.K('import-a-row-once sends an imported body through the save contract (D19)', 'run the link sync of [save a body](save-a-body.md)' in doc_page('cookbook/import-a-row-once.md'))

# ---- the rules live in the file, once
S.K('lifelog_meta holds only the few cross-table rules (at most 8 keys)', len(meta) <= 8, sorted(meta))
first = re.search(r'^(?!\s*--)\s*\S', DDL, re.M); head = DDL[:first.start()] if first else DDL
S.K('the DDL header before the first statement is a short pointer (<= 12 lines) naming lifelog_meta', head.count('\n') <= 12 and 'lifelog_meta' in head)
for t in ('entities', 'pages', 'people', 'metrics', 'measurements', 'habit_periods', 'link_kinds', 'links', 'lifelog_meta'):
    S.K(f'{t}: its CREATE statement carries its rules as comments', re.search(r'\n\s*--', schema.get(t, '')) is not None)
n = {k: one(c, f"select count(*) from sqlite_schema where type='{k}' and name not like 'sqlite_%' and not (type='table' and name like 'pages_fts_%')") for k in ('table', 'view', 'trigger')}
tot = re.search(r'\*\*(\d+) tables \+ 1 FTS5 virtual table \+ (\d+) views\*\*.*?\*\*\+ (\d+) triggers\.\*\*', DOC, re.S)
S.K('the totals in schema/README.md match the DDL (tables, views, triggers)', tot and (int(tot.group(1)) + 1, int(tot.group(2)), int(tot.group(3))) == (n['table'], n['view'], n['trigger']), (tot and tot.groups(), n))

# ---- the tree holds together
REPO = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..'))
TREE = {r: doc_page(r) for r in docsql.pages()}
for top in docsql.RECORDS[:2]:   # issues, rfcs: their indexes and templates are linked pages too
    for f in ('README.md', 'template.md'): TREE[f'{top}/{f}'] = doc_page(f'{top}/{f}')

def strip_code(text):
    text = re.sub(r'^```.*?^```', '', text, flags=re.S | re.M)
    return re.sub(r'(`+).+?\1', '', text)

def slug(h):
    """GitHub's heading anchor: lowercase, punctuation dropped, spaces to hyphens."""
    h = re.sub(r'\[([^\]]*)\]\([^)]*\)', r'\1', h).strip().lower()
    return re.sub(r' ', '-', re.sub(r'[^\w\- ]', '', h))

def anchors(text):
    out, seen = set(re.findall(r'<a id="([^"]+)"></a>', text)), {}
    for h in re.findall(r'^#{1,6} (.+)$', strip_code(text), re.M):
        s = slug(h); n = seen.get(s, 0); seen[s] = n + 1
        out.add(s if n == 0 else f'{s}-{n}')
    return out

def links(text):
    return [u for u in re.findall(r'\]\(([^)\s]+)\)', strip_code(text)) if not re.match(r'[a-z]+:', u)]

def resolve(base, url):
    """(target, anchor) of a link on the page base; both relative to docs/ ('..' leaves the tree)."""
    path, _, anchor = url.partition('#')
    if not path: return base, anchor
    return os.path.normpath(os.path.join(os.path.dirname(base), path)).replace(os.sep, '/'), anchor

def problems(tree, root=None):
    root = root or docsql.DOCS; out = []
    adr = sorted(r for r in tree if re.match(r'decisions/D\d\d-[a-z0-9-]+\.md$', r))
    nums = [int(r[11:13]) for r in adr]
    if nums != list(range(1, len(nums) + 1)) or len(nums) < 24: out.append(f'decisions are not D01..Dn without a gap: {nums}')
    for r in adr:
        n = int(r[11:13]); h1 = tree[r].split('\n', 1)[0]
        if not h1.startswith(f'# D{n} — '): out.append(f'{r}: the title is not "# D{n} — ..."')
        if re.search(r'\*\(', h1): out.append(f'{r}: the title carries a parenthetical status')
        if not re.search(r'^\*\*Status:\*\* (accepted|deferred)$', tree[r], re.M): out.append(f'{r}: no **Status:** accepted|deferred line')
        if f']({r[10:]})' not in tree.get('decisions/README.md', ''): out.append(f'{r} is not in the decision index')
    if not re.search(r'^\*\*Status:\*\* [^\n]{10,}$', tree.get('README.md', ''), re.M): out.append('docs/README.md has no one-line status')
    for r, text in tree.items():
        for u in links(text):
            target, anchor = resolve(r, u)
            outside = os.path.join(REPO, 'docs', target) if target.startswith('..') else os.path.join(root, *target.split('/'))
            if target not in tree and not os.path.exists(outside): out.append(f'{r}: broken link {u}')
            elif anchor and target in tree and anchor not in anchors(tree[target]): out.append(f'{r}: no anchor #{anchor} in {target}')
    reach, todo = set(), ['README.md']
    while todo:
        r = todo.pop()
        if r in reach or r not in tree: continue
        reach.add(r); todo += [resolve(r, u)[0] for u in links(tree[r])]
    orphans = sorted(set(tree) - reach)
    if orphans: out.append(f'pages not reachable from docs/README.md: {orphans}')
    return out

p = problems(TREE)
S.K('the tree holds together', not p, p[:5])
D7 = 'decisions/D07-measurements.md'
def broken(r, old, new): return {**TREE, r: TREE[r].replace(old, new, 1)}
for name, tree in [('a decision deleted', {k: v for k, v in TREE.items() if k != D7}),
                   ('a decision without its status', broken(D7, '**Status:** accepted', '')),
                   ('a broken link', broken('contract/time.md', '\n', '\nSee [nowhere](nowhere.md).\n')),
                   ('a broken anchor', broken('contract/time.md', '\n', '\nSee [R999](../research/references.md#r999).\n')),
                   ('an orphan page', {**TREE, 'contract/orphan.md': '# Orphan\n'})]:
    q = problems(tree); S.K(f'a broken copy is noticed: {name}', q and q != p, q[:1])
roots = {f: open(os.path.join(REPO, *f.split('/')), encoding='utf-8').read() for f in ('README.md', 'AGENTS.md', 'tests/README.md')}
bad = [f'{f}: {u}' for f, text in roots.items() for u in links(text) if not os.path.exists(os.path.join(REPO, os.path.dirname(f), u.partition('#')[0]))]
S.K('every relative link of README.md, AGENTS.md and tests/README.md resolves', not bad, bad)

S.done()
