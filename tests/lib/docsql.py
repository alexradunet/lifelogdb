"""Read the docs tree (docs/): the DDL in schema/schema.sql, one page, the cookbook blocks, the whole current-truth text.
Used by every suite. The tree is $DOCS (default: ../../docs), so the mutant runner can point a suite at a broken copy.
CLI:  python3 docsql.py ddl OUTFILE"""
import os, re, sys

DOCS = os.environ.get('DOCS') or os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', 'docs'))
CURRENT = ('README.md', 'process.md', 'architecture', 'schema', 'contract', 'decisions', 'cookbook', 'guides', 'research')
RECORDS = ('issues', 'rfcs', 'plans')   # dated records: not current truth

def page(rel, root=None):
    """One file of the tree, by its path relative to docs/ ('' if it does not exist)."""
    p = os.path.join(root or DOCS, *rel.split('/'))
    return open(p, encoding='utf-8').read() if os.path.exists(p) else ''

def cookbook_order(root=None):
    """The recipe keys in the order the cookbook index lists them."""
    return re.findall(r'\]\(([a-z0-9-]+)\.md\)', page('cookbook/README.md', root))

def pages(root=None):
    """[rel] of every current-truth markdown page, in reading order (cookbook recipes in index order)."""
    root = root or DOCS
    out = []
    for top in CURRENT:
        p = os.path.join(root, top)
        if os.path.isfile(p): out.append(top); continue
        if not os.path.isdir(p): continue
        names = sorted(f for f in os.listdir(p) if f.endswith('.md'))
        if top == 'cookbook':
            order = cookbook_order(root)
            names = ['README.md'] + [f'{k}.md' for k in order if f'{k}.md' in names] + [f for f in names if f != 'README.md' and f[:-3] not in order]
        elif 'README.md' in names:
            names.remove('README.md'); names.insert(0, 'README.md')
        out += [f'{top}/{f}' for f in names]
    return out

def doc_text(root=None, skip=()):
    """Every current-truth page, concatenated in reading order, each after a `<!-- file: rel -->` line."""
    return ''.join(f'<!-- file: {r} -->\n{page(r, root)}\n' for r in pages(root) if not r.startswith(skip))

def ddl(root=None):
    return page('schema/schema.sql', root)

def cookbook_blocks(root=None):
    """[(key, sql)] for every ```sql block of the cookbook, recipes in index order."""
    return [(k, m.group(1)) for k in cookbook_order(root)
            for m in re.finditer(r"```sql\n(.*?)\n```", page(f'cookbook/{k}.md', root), re.S)]

if __name__ == "__main__":
    if sys.argv[1:2] == ["ddl"] and len(sys.argv) == 3:
        open(sys.argv[2], "w", encoding="utf-8", newline="\n").write(ddl())
    else:
        sys.exit("usage: docsql.py ddl OUTFILE")
