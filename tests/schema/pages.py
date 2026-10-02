"""Pages and titles (contract/titles-and-wikilinks, D5): every page titled, the day rule, one title namespace, filename-safe titles,
title_key and its vectors, lookups by key, fixed titles, link first and write later, full-text search, and why titles are
not unique by a collation. The day page itself is in journal.py."""
import os, re, subprocess, sys, tempfile
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
import wikisave as W
S = Suite('pages')

def add(c, title=None, day=None, body='', key='auto'):
    """An entities + pages row inside a savepoint: the id, or None when the DDL refused it."""
    k = (title_key(title) if title else None) if key == 'auto' else key
    c.execute('SAVEPOINT a')
    try:
        i = ent(c, 'page'); c.execute('INSERT INTO pages(id,title,title_key,day,body) VALUES (?,?,?,?,?)', (i, title, k, day, body))
        c.execute('RELEASE a'); return i
    except sqlite3.Error:
        c.execute('ROLLBACK TO a'); c.execute('RELEASE a'); return None

# ---- every page titled; the day rule
c = fresh()
S.K('a page accepted', add(c, 'Diet') is not None)
S.K('a page with a day accepted', add(c, 'Trip report', day='2026-06-09') is not None)
S.K('a page without a day accepted', add(c, 'Reference') is not None)
S.K('a malformed day rejected', add(c, 'Bad day', day='2026-6-9') is None)
S.K('a page without a title rejected', add(c, None, key=None) is None)
S.K('a page with a key but no title rejected', add(c, None, key='orphan') is None)
S.K('a page with a title but no key rejected', add(c, 'Keyless', key=None) is None)

# ---- one title namespace
c = fresh()
S.K('first page accepted', add(c, 'Diet', day='2026-06-09') is not None)
S.K('DIET without a day collides with Diet with a day', add(c, 'DIET', day=None) is None)
S.K('NFD Café collides with NFC Café', add(c, 'Café') is not None and add(c, 'Café') is None)
S.K('Straße and STRASSE are one page', add(c, 'Straße') is not None and add(c, 'STRASSE') is None)
S.K('a different title is fine', add(c, 'Cafe') is not None)
tryx(c, f"UPDATE entities SET deleted_at={NOW} WHERE id=(select id from pages where title='Diet')")
S.K('a tombstoned page still holds its title (the index covers tombstones)', add(c, 'diet') is None)

# ---- filename-safe titles (pages_title_len, pages_title_safe)
c = fresh()
for ch in ['/', '\\', ':', '*', '?', '"', '<', '>', '|', '\x01', '\t', '\n', '\x1f', '\x7f', '\x85', '\x9f']:
    S.K(f'character {ch!r} rejected', add(c, 'a' + ch + 'b', key='ab') is None)
for t in ['.hidden', '..', 'trail.', ' pad', 'pad ', 'CON', 'con', 'Nul', 'prn', 'AUX', 'COM1', 'lpt9', 'con.txt', 'CON.backup', 'NUL.tar.gz', 'Prn.a.b',
          'COM¹', 'com².x', 'COM³', 'LPT¹', 'lpt².txt', 'LPT³.a.b', 'CON.', '.CON', 'CON ', ' CON']:
    S.K(f'unsafe or device title {t!r} rejected', add(c, t, key=t.lower().strip() or 'x') is None)
for t in ['CONSOLE', 'CONSOLE.txt', 'a.CON', 'x.NUL', 'COM10', 'com10.x', 'LPT0', 'Com0.x', 'CON1', 'my.con.note', 'Lifelog v1.2', '日本語 ノート', 'C#', 'a' * 240]:
    S.K(f'title {t[:14]!r} accepted', add(c, t) is not None)
S.K('241 bytes rejected (é × 121 = 242 bytes)', add(c, 'é' * 121) is None and add(c, 'a' * 241) is None)
S.K('80 × 日 = 240 bytes accepted, 81 rejected', add(c, '日' * 80) is not None and add(c, '日' * 81) is None)
INVIS = [0xAD, 0x61C, 0x200B, 0x200E, 0x200F, 0x202A, 0x202E, 0x2060, 0x2064, 0x2066, 0x2069, 0xFEFF]
for cp in INVIS:
    t = f'Diet{chr(cp)}plan'
    S.K(f'U+{cp:04X} in a title is rejected by the DB and by the app', add(c, t) is None and not W.title_ok(t))
for t in ['Café', '\U0001F389 Party', 'می‌خواهم', '\U0001F468‍\U0001F469‍\U0001F467', 'İstanbul', '❤️']:
    S.K(f'{t!r} accepted by both (ZWNJ/ZWJ and variation selectors carry meaning)', add(c, t) is not None and W.title_ok(t))
for t in ('Diet͸', '\U000E0080x'):
    S.K(f'unassigned code point {t!r}: the app rejects it, the DB cannot tell', not W.title_ok(t) and add(c, t) is not None)
S.K('ASCII title with a wrong key rejected', add(c, 'Diet2', key='dyet2') is None)
S.K('a key with an ASCII capital rejected', add(c, 'Diet3', key='Diet3') is None)
S.K('a key with a leading space rejected', add(c, 'Diet4', key=' diet4') is None)
S.K("a non-ASCII title's key may not hold an ASCII capital", add(c, 'Café Q', key='Café Q') is None)
S.K('a title with leading space refused (a key the app folded to match)', add(c, ' Pädded', key='pädded') is None)
S.K('a title with a NUL byte refused', add(c, 'a\x00é', key=title_key('a\x00é')) is None)
S.K('a non-ASCII title with a plausible key accepted (the app owns the fold)', add(c, 'Über', key='über') is not None)

# ---- title_key vectors of contract/titles-and-wikilinks
sec = doc_page('contract/titles-and-wikilinks.md')
m = re.search(r'\| title \| `title_key` \|\n\|---\|---\|\n((?:\|.*\n)+)', sec)
vec = []
for line in (m.group(1).splitlines() if m else []):
    left, right = line.strip('|').split('|')[:2]
    ins = [x.replace('\\u0301', '́') for x in re.findall(r'`([^`]+)`', left)]; outs = re.findall(r'`([^`]+)`', right)
    if len(outs) == 1: outs = outs * len(ins)
    vec += list(zip(ins, outs))
S.K('the title_key table of contract/titles-and-wikilinks has at least 12 pairs and every one is what the function gives', len(vec) >= 12 and all(title_key(a) == b for a, b in vec), [(a, b, title_key(a)) for a, b in vec if title_key(a) != b])

# ---- fixed title
c = fresh(); p = add(c, 'Fixed')
S.K('a title cannot change', 'immutable' in tryx(c, "UPDATE pages SET title='Changed' WHERE id=?", (p,)))
S.K('a no-op SET title = title passes', tryx(c, "UPDATE pages SET title=title, body='b' WHERE id=?", (p,)) == 'OK')

# ---- lookups by key
c = fresh()
for i in range(500): add(c, f'Note {i}', body=f'note {i}')
for t in ('Diet', 'Food', 'Zürich'): add(c, t)
sel = [s for s in statements(block('save-a-body')) if code(s).upper().startswith('SELECT')]
S.K('cookbook/save-a-body has one resolve', len(sel) == 1, sel)
if sel:
    plan = ' | '.join(r[3] for r in c.execute('EXPLAIN QUERY PLAN ' + sel[0], {'key': 'diet'}))
    S.K('...and is a SEARCH on pages_title', 'SEARCH' in plan and 'pages_title' in plan and 'SCAN p' not in plan, plan)
    S.K('...and finds the page', c.execute(sel[0], {'key': 'zürich'}).fetchone()[1] == 'Zürich')
S.K('control: a lookup by lower(title) is a SCAN', 'SCAN' in ' '.join(r[3] for r in c.execute("EXPLAIN QUERY PLAN SELECT id FROM pages WHERE lower(title)='diet'")))

# ---- link first, write later
c = fresh(); mm = W.save_page(c, 'Planning #japan-trip for spring', 'Plans')[0]
row = c.execute("select id, day, body from pages where title_key='japan-trip'").fetchone()
S.K('a link made its target a page, empty, with no day', row is not None and row[1:] == (None, ''), row)
S.K('writing the ghost needs no new page and no redirect', tryx(c, "UPDATE pages SET body='Trip report' WHERE id=?", (row[0],)) == 'OK' and one(c, "select count(*) from pages where title_key='japan-trip'") == 1)
S.K('the page still links to it', one(c, "select count(*) from links where from_id=? and to_id=? and kind='wikilink'", (mm, row[0])) == 1)

# ---- full-text search
c = fresh(); p = add(c, 'Notes'); mo = add(c, 'Words', body='first words about Zürich')
def hits(q): return one(c, 'select count(*) from pages_fts where pages_fts match ?', (q,))
S.K('an insert is indexed; unicode61 folds accents (zurich finds Zürich)', hits('first') == 1 and hits('zurich') == 1)
before = c.total_changes; c.execute("UPDATE pages SET day='2026-06-09' WHERE id=?", (mo,)); d = c.total_changes - before
S.K('a new day touches pages and entities only, not the index (pages_fts_update watches title and body)', d == 2, d)
c.execute("UPDATE pages SET body='second words' WHERE id=?", (mo,))
S.K('a body change re-indexes: old word gone, new word found', hits('first') == 0 and hits('second') == 1)
add(c, 'CJK', body='日本語のノートを書く')
S.K('a CJK run is one token (the known limit of unicode61)', hits('日本語のノートを書く') == 1 and hits('本語') == 0)
c.execute('INSERT INTO pages_fts(pages_fts) VALUES(\'rebuild\')')
S.K('the index rebuilds from pages and passes its integrity-check', hits('second') == 1 and tryx(c, "INSERT INTO pages_fts(pages_fts, rank) VALUES('integrity-check', 1)") == 'OK')
S.K('cookbook/full-text-search finds a page by a word of its body', [r[0] for r in c.execute(block('full-text-search'), {'query': 'second'})] == [mo])

# ---- why not a collation: an index on a collation only the app has
d = tempfile.mkdtemp(); path = os.path.join(d, 'coll.db')
a = sqlite3.connect(path); a.create_collation('UFOLD', lambda x, y: (x.casefold() > y.casefold()) - (x.casefold() < y.casefold()))
a.execute('CREATE TABLE t (title TEXT)'); a.execute('CREATE UNIQUE INDEX t_title ON t(title COLLATE UFOLD)'); a.execute("INSERT INTO t VALUES ('Café')"); a.commit(); a.close()
r1 = subprocess.run(['sqlite3', path, "INSERT INTO t VALUES ('x')"], capture_output=True, text=True, stdin=subprocess.DEVNULL)
r2 = subprocess.run(['sqlite3', path, 'PRAGMA integrity_check'], capture_output=True, text=True, stdin=subprocess.DEVNULL)
r3 = subprocess.run(['sqlite3', path, 'SELECT count(*) FROM t'], capture_output=True, text=True, stdin=subprocess.DEVNULL)
S.K('a database whose index needs an app-registered collation cannot be written by another program', 'no such collation sequence' in r1.stderr, r1.stderr)
S.K('...nor integrity-checked', 'no such collation sequence' in (r2.stderr + r2.stdout), r2.stderr + r2.stdout)
S.K('...though it can be read', r3.stdout.strip() == '1', r3.stderr)
S.done()
