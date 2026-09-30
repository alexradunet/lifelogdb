"""Round-11 probes (run:  python3 r11probes.py DDLFILE).  Expected outcome is in each label.
Round 11 merged `note` and `wiki` into one `page` kind (SCHEMA.md D5 addendum 5). What changed, and what must not have:
A  kinds: 'memo' and 'page' only; 'note' and 'wiki' are gone.
B  day: a memo always has one; a page may or may not (the one CHECK that changed).
C  one title namespace: titles collide across pages whether or not either has a day; memos have no key and never collide.
D  lookups: the §6.14 resolve statement (read from the document) has no kind predicate and still uses pages_title; the index is partial.
E  link first, write later: a link target is a page, and filling it in needs no new page and no redirect.
F  ghost_pages, §6.13 and the §6.2 day view, run from the document's own SQL.
G  kind stays fixed, by the trigger (not by a CHECK that happens to fail)."""
exec(open('probes1.py').read().split('# ---- P1:')[0])
import re, sys, os, unicodedata
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, '..', 'lib')); sys.path.insert(0, os.path.join(HERE, '..', 'wikilinks'))
import docsql, wikisave as W
res = []
def K(label, cond, detail=''):
    res.append(bool(cond))
    if not cond: print('  FAIL', label, detail)
def key_of(t): return unicodedata.normalize('NFC', unicodedata.normalize('NFC', t).casefold()) if t else None
def add(c, kind, title=None, day='2026-06-09', body='', key='auto', created=None):
    """Insert an entities + pages row inside a savepoint; the id on success, None when the DDL refused it."""
    k = key_of(title) if key == 'auto' else key
    c.execute('SAVEPOINT a')
    try:
        if created:
            c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES ('page',{created},{created})"); i = c.execute('select last_insert_rowid()').fetchone()[0]
        else:
            i = ent(c, 'page')
        c.execute("INSERT INTO pages(id,kind,title,title_key,day,body) VALUES (?,?,?,?,?,?)", (i, kind, title, k, day, body))
        c.execute('RELEASE a'); return i
    except sqlite3.Error:
        c.execute('ROLLBACK TO a'); c.execute('RELEASE a'); return None
def one(c, sql, args=(), col=0):
    """First row's column, or None when there is no row or the statement is refused: a broken schema must fail a probe, not crash the run."""
    try: r = c.execute(sql, args).fetchone()
    except sqlite3.Error: return None
    return None if r is None else r[col]
doc = open(DOCPATH, encoding='utf-8').read()
blocks = docsql.cookbook_blocks(doc)
def block(prefix): return [s for h, s in blocks if h.startswith(prefix)][0]

# ---- A kinds
c = fresh()
K('A1 kind memo accepted', add(c, 'memo') is not None)
K('A2 kind page accepted', add(c, 'page', 'Diet') is not None)
for k in ('note', 'wiki', 'image', ''):
    K(f'A3 kind {k!r} rejected', add(c, k, 'Some ' + k) is None)
K('A4 the named CHECK lists exactly memo and page', "kind IN ('memo','page')" in c.execute("select sql from sqlite_master where name='pages'").fetchone()[0])

# ---- B day
c = fresh()
K('B1 memo with a day accepted', add(c, 'memo', day='2026-06-09') is not None)
K('B2 memo without a day rejected', add(c, 'memo', day=None) is None)
K('B3 page with a day accepted', add(c, 'page', 'Trip report', day='2026-06-09') is not None)
K('B4 page without a day accepted (what a wiki page always could, now what a note can too)', add(c, 'page', 'Reference', day=None) is not None)
K('B5 page without a title rejected', add(c, 'page', None, key=None) is None)
K('B6 a titled memo rejected (it would be unfindable)', add(c, 'memo', 'Titled memo') is None)
K('B7 a memo with a key but no title rejected', add(c, 'memo', None, key='orphan') is None)
K('B8 a page with a title but no key rejected', add(c, 'page', 'Keyless', key=None) is None)

# ---- C one title namespace
c = fresh()
K('C1 first page accepted', add(c, 'page', 'Diet', day='2026-06-09') is not None)
K('C2 DIET without a day collides with Diet with a day', add(c, 'page', 'DIET', day=None) is None)
K('C3 diet with a day collides too', add(c, 'page', 'diet', day='2026-07-01') is None)
K('C4 NFD Café collides with NFC Café', add(c, 'page', 'Café') is not None and add(c, 'page', 'Café') is None)
K('C5 a different title is fine', add(c, 'page', 'Diet plan') is not None)
n = sum(add(c, 'memo', body=f'memo {i}') is not None for i in range(200))
K('C6 200 memos (NULL keys) accepted — a NULL key never collides', n == 200, n)
pid = one(c, "select id from pages where title='Diet'") or -1
tryx(c, f"UPDATE entities SET deleted_at={NOW} WHERE id=?", (pid,))
K('C7 a tombstoned page still holds its title (the index covers tombstones)', add(c, 'page', 'DIET') is None)

# ---- D lookups
c = fresh()
for i in range(500): add(c, 'memo', body=f'memo {i}')
for t in ('Diet', 'Food', 'Japan trip', 'Zürich'): add(c, 'page', t)
blk = block('6.14')
sel = re.search(r'-- 1\) resolve[^\n]*\n(SELECT .*?;)', blk, re.S).group(1)
K('D1 the §6.14 resolve statement carries no kind predicate', 'kind' not in sel.lower(), sel)
plan = ' | '.join(r[3] for r in c.execute('EXPLAIN QUERY PLAN ' + sel, {'key': 'diet'}))
K('D2 ... and is a SEARCH on pages_title, not a SCAN', 'SEARCH' in plan and 'pages_title' in plan and 'SCAN' not in plan, plan)
K('D3 it finds the page', one(c, sel, {'key': 'zürich'}, 1) == 'Zürich')
plan = ' | '.join(r[3] for r in c.execute("EXPLAIN QUERY PLAN SELECT id FROM pages WHERE lower(title) = 'diet'"))
K('D4 control: a lookup NOT by title_key is a SCAN (so D2 can fail)', 'SCAN' in plan, plan)
r = tryx(c, 'SELECT id FROM pages INDEXED BY pages_title WHERE title_key IS NULL')        # count(*) ignores INDEXED BY; selecting id does not
K('D5 the index holds no memos: it cannot answer "which pages have no key"', r[0] == 'ERR' and 'no query solution' in r[1], r)
K('D6 ... and answers a lookup by key', one(c, "SELECT id FROM pages INDEXED BY pages_title WHERE title_key = 'diet'") is not None)
K('D7 the index is unique and its predicate is title_key IS NOT NULL (no kind)',
  re.search(r'CREATE UNIQUE INDEX pages_title ON pages\(title_key\) WHERE title_key IS NOT NULL\s*$', c.execute("select sql from sqlite_master where name='pages_title'").fetchone()[0]) is not None)

# ---- E link first, write later
c = fresh()
m = W.save_memo(c, 'Planning #japan-trip for spring')[0]
row = c.execute("select id, kind, day, body from pages where title_key='japan-trip'").fetchone()
K('E1 the link made the target a page, empty, with no day', row is not None and row[1] == 'page' and row[2] is None and row[3] == '', row)
ghost = row[0] if row else -1
r = tryx(c, "UPDATE pages SET body = 'Trip report: day one…' WHERE id = ?", (ghost,))
K('E2 writing the ghost needs no new page, no new kind, no redirect', r[0] == 'OK' and c.execute("select count(*) from pages where title_key='japan-trip'").fetchone()[0] == 1 and one(c, "select kind from pages where id=?", (ghost,)) == 'page')
K('E3 the memo still links to it', c.execute("select count(*) from links where from_id=? and to_id=? and kind='wikilink'", (m, ghost)).fetchone()[0] == 1)
K('E4 a second page with that title is still refused (one namespace)', add(c, 'page', 'Japan-Trip', day='2026-09-30') is None)

# ---- F ghost_pages, §6.13, §6.2 from the document
c = fresh(); OLD = "strftime('%Y-%m-%dT%H:%M:%fZ','now','-40 day')"
g_a = add(c, 'page', 'Ghost A', day=None, created=OLD)                    # typo target, never written
g_b = add(c, 'page', 'Ghost B', day='2026-06-09', created=OLD)            # created on purpose, left empty
w_c = add(c, 'page', 'Written', day='2026-06-09', body='text', created=OLD)
l_d = add(c, 'page', 'Linked empty', day=None, created=OLD)
y_e = add(c, 'page', 'Young ghost', day=None)                              # created now: under 30 days
t_f = add(c, 'page', 'Tombstoned ghost', day=None, created=OLD)
mm = add(c, 'memo', day='2026-06-09', body='', created=OLD)               # an empty memo is not a page
src = add(c, 'memo', day='2026-06-09', body='[[Linked empty]]')
tryx(c, f"INSERT INTO links(from_id,to_id,kind,created_at) VALUES (?,?,'wikilink',{NOW})", (src, l_d))
tryx(c, f"UPDATE entities SET deleted_at={NOW} WHERE id=?", (t_f,))
expect = ['Ghost A', 'Ghost B']
K('F1 the ghost_pages view lists the empty, unlinked, old, live pages — with or without a day', sorted(r[0] for r in c.execute('select title from ghost_pages')) == expect)
K('F2 §6.13 (the document\'s block) lists the same pages', sorted(r[1] for r in c.execute(block('6.13'))) == expect)
day_sql = block('6.2 ')
c = fresh()
add(c, 'memo', day='2026-09-29', body='a memo')
add(c, 'page', 'Essay', day='2026-09-29', body='text')
add(c, 'page', 'Link target', day=None)
add(c, 'page', 'Yesterday essay', day='2026-09-28', body='text')
rows = c.execute(day_sql, {'day': '2026-09-29'}).fetchall()
pages = [r for r in rows if r[0].startswith('page')]
K('F3 §6.2 shows the page written that day, labelled page, once', [r[2] for r in pages] == ['Essay'], rows)
K('F4 ... not the link target (no day), not the page of another day', not any(r[2] in ('Link target', 'Yesterday essay') for r in rows))
K('F5 ... and the memo', any(r[0] == 'memo' and r[2] == 'a memo' for r in rows))
pid = one(c, "select id from pages where title='Essay'")
tryx(c, "UPDATE entities SET created_at='2026-09-29T08:00:00.000Z' WHERE id=?", (pid,)); tryx(c, "UPDATE pages SET body='text 2' WHERE id=?", (pid,))
rows = c.execute(day_sql, {'day': '2026-09-29'}).fetchall()
K('F6 an edited page is flagged "(edited)" in the day view', any(r[0] == 'page (edited)' and r[2] == 'Essay' for r in rows), rows)

# ---- G kind is fixed — by the trigger
c = fresh(); p = add(c, 'page', 'Fixed'); mo = add(c, 'memo')
for label, sql, args in (('G1 page -> memo', "UPDATE pages SET kind='memo' WHERE id=?", (p,)), ('G2 memo -> page', "UPDATE pages SET kind='page' WHERE id=?", (mo,))):
    r = tryx(c, sql, args)
    K(f'{label} rejected, by pages_kind_fixed', r[0] == 'ERR' and 'pages.kind is fixed' in r[1], r)
K('G3 a no-op SET kind = kind is accepted', tryx(c, "UPDATE pages SET kind = kind WHERE id=?", (p,))[0] == 'OK')
r = tryx(c, "UPDATE pages SET title='Changed' WHERE id=?", (p,))
K('G4 a title still cannot change', r[0] == 'ERR' and 'immutable' in r[1], r)

print(f'round-11 probes: {sum(res)}/{len(res)} met expectations')
