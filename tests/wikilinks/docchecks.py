"""The save contract as the DOCUMENT prints it (contract/titles-and-wikilinks and cookbook/save-a-body), not as the reference implementation does.
A  the vector table of contract/titles-and-wikilinks reproduces with the reference extraction, row for row;
B  the SQL of cookbook/save-a-body, run literally statement by statement, gives the vector results, leaves no orphan, carries ids by
   RETURNING, and equals the reference implementation after 400 random edits;
C  cookbook/backlinks lists a day page's wikilink and not a stub's redirect row."""
import json, os, random, re, sqlite3, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import Suite, DOC, NOW, fresh, block, statements, code
import docsql
import wikisave as W
from vectors import V
S = Suite('document save contract')

# ---- A  the table of contract/titles-and-wikilinks
sec = docsql.page('contract/titles-and-wikilinks.md')
m = re.search(r'\| body \| links to.*?\n\s*\|---\|---\|\n((?:\s*\|.*\n)+)', sec)
rows = []
for line in (m.group(1).splitlines() if m else []):
    mm = re.fullmatch(r'\| (``? .*? ``?|`[^`]*`) \| (.*) \|', line.strip())
    if not mm: S.K('A a vector row parses', False, line); continue
    body = mm.group(1)
    body = body[3:-3] if body.startswith('``') else body[1:-1]
    body = body.replace('\\|', '|').replace('\\n', '\n').replace('\\u0301', '́').replace('\\u0308', '̈')
    rows.append((body, [] if mm.group(2) == '—' else re.findall(r'`([^`]*)`', mm.group(2))))
bad = [(b, e, list(W.targets(b)[0].values())) for b, e in rows if list(W.targets(b)[0].values()) != e]
S.K('A the contract/titles-and-wikilinks table has at least 25 vectors and every one reproduces', len(rows) >= 25 and not bad, (len(rows), bad[:3]))
S.K('A the 240/241-byte and 80/81-CJK boundary under the table holds', W.targets('[[' + 'a' * 240 + ']]')[0] and not W.targets('[[' + 'a' * 241 + ']]')[0]
    and W.targets('[[' + '日' * 80 + ']]')[0] and not W.targets('[[' + '日' * 81 + ']]')[0])

# ---- B  cookbook/save-a-body run literally
sts = statements(block('save-a-body'))
def by(prefix): return [s for s in sts if code(s).upper().startswith(prefix)]
St = {k: by(v) for k, v in dict(begin='BEGIN IMMEDIATE', sp='SAVEPOINT', sel='SELECT', rev='UPDATE ENTITIES', ent='INSERT INTO ENTITIES',
                                  pg='INSERT INTO PAGES', ln='INSERT INTO LINKS', rel='RELEASE', dele='DELETE FROM LINKS', commit='COMMIT').items()}
S.K('B cookbook/save-a-body has exactly one statement of each step', all(len(v) == 1 for v in St.values()), {k: len(v) for k, v in St.items()})
S.K('B cookbook/save-a-body opens with BEGIN IMMEDIATE and resolves inside the transaction', sts and code(sts[0]).upper().startswith('BEGIN IMMEDIATE')
    and sts.index(St['sel'][0]) < sts.index(St['ent'][0]) < sts.index(St['commit'][0]) if all(St.values()) else False)
S.K('B no cookbook/save-a-body statement uses last_insert_rowid()', 'last_insert_rowid' not in block('save-a-body'))

def doc_save(c, page_id, body, own_key):
    ok, _ = W.targets(body, own_key); ids = []
    for key, title in ok.items():
        c.execute(St['sp'][0])
        try:
            row = c.execute(St['sel'][0], {'key': key}).fetchone()
            if row is None:
                tid = c.execute(St['ent'][0], {'source': 'ui'}).fetchone()[0]
                c.execute(St['pg'][0], {'title': title, 'key': key, 'target_id': tid})
            else:
                tid = row[0]
                if row[2] is not None: c.execute(St['rev'][0], {'found_id': tid})
            c.execute(St['ln'][0], {'page_id': page_id, 'target_id': tid, 'source': 'ui'})
            c.execute(St['rel'][0])
        except sqlite3.Error:
            c.execute('ROLLBACK TO target'); c.execute('RELEASE target'); continue
        ids.append(tid)
    c.execute(St['dele'][0], {'page_id': page_id, 'target_ids': json.dumps(ids)})

def doc_page(c, body, title='Vector body'):
    c.execute(St['begin'][0])
    pid = c.execute(f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES('page',{NOW},{NOW},'ui') RETURNING id").fetchone()[0]
    c.execute('INSERT INTO pages(id,title,title_key,body) VALUES(?, ?, ?, ?)', (pid, title, W.title_key(title), body))
    doc_save(c, pid, body, W.title_key(title)); c.execute(St['commit'][0]); return pid

def doc_edit(c, pid, body):
    c.execute(St['begin'][0]); c.execute('UPDATE pages SET body=? WHERE id=?', (body, pid))
    doc_save(c, pid, body, c.execute('select title_key from pages where id=?', (pid,)).fetchone()[0]); c.execute(St['commit'][0])

def links_of(c, pid): return sorted(r[0] for r in c.execute("SELECT p.title FROM links l JOIN pages p ON p.id=l.to_id WHERE l.from_id=? AND l.kind='wikilink'", (pid,)))
if all(len(v) == 1 for v in St.values()):
    okv, orph = True, 0
    for label, body, exp in V:
        c = fresh(); pid = doc_page(c, body)
        orph += c.execute("select count(*) from entities e where not exists (select 1 from pages p where p.id=e.id)").fetchone()[0]
        if links_of(c, pid) != sorted(exp): okv = False; print('   differs:', label, links_of(c, pid), exp)
    S.K(f'B the document\'s SQL, run literally, gives the vector result for all {len(V)} vectors', okv)
    S.K('B ...and leaves no orphan entities row', orph == 0)
    c = fresh(); c.execute('BEGIN IMMEDIATE'); eid = c.execute(St['ent'][0], {'source': 'ui'}).fetchone()[0]
    c.execute(St['pg'][0], {'title': 'Zed', 'key': 'zed', 'target_id': eid}); c.execute('COMMIT')
    S.K('B the id step 2b RETURNs is the new page\'s id', c.execute("select id from pages where title='Zed'").fetchone()[0] == eid)
    frags = ['[[Alpha]]', '[[alpha|a]]', '#beta', '`[[code]]`', '[[Bad/Name]]', '~~~\n[[fence]]\n~~~', '[[Ünï]]', '[[UNÏ]]', 'text', '#Beta',
             '[[Gamma delta]]', '#12', '[[CON]]', '#REDIRECTED', '#REDIRECT [[Alpha]]']
    rng = random.Random(10); ca, cb = fresh(), fresh()
    pa = [doc_page(ca, 'seed', f'Seed {i}') for i in range(6)]; pb = [W.save_page(cb, 'seed', f'Seed {i}')[0] for i in range(6)]
    for _ in range(400):
        i = rng.randrange(6); body = ' '.join(rng.choice(frags) for _ in range(rng.randint(0, 6)))
        doc_edit(ca, pa[i], body); W.edit_body(cb, pb[i], body)
    S.K('B the document\'s SQL equals the reference implementation after 400 random edits', [links_of(ca, p) for p in pa] == [links_of(cb, p) for p in pb])
    S.K('B ...with the same set of pages', sorted(r[0] for r in ca.execute('select title_key from pages where title_key is not null'))
        == sorted(r[0] for r in cb.execute('select title_key from pages where title_key is not null')))

# ---- C  cookbook/backlinks drops redirect rows
c = fresh(); c.execute('BEGIN IMMEDIATE')
def mk(title, body='', day=None):
    i = c.execute(f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES('page',{NOW},{NOW},'ui') RETURNING id").fetchone()[0]
    c.execute("INSERT INTO pages(id,title,title_key,day,body) VALUES(?,?,?,?,?)", (i, title, W.title_key(title), day, body)); return i
new, old, mm = mk('Diet plan'), mk('Diet', '#REDIRECT [[Diet plan]]'), mk('2026-09-30', '[[Diet plan]]', '2026-09-30')
for f, t, k in ((old, new, 'redirect'), (mm, new, 'wikilink')):
    c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,?,{NOW},'ui')", (f, t, k))
c.execute('COMMIT')
rows = c.execute(block('backlinks'), {'page_id': new}).fetchall()
S.K('C cookbook/backlinks lists the day page\'s wikilink and not the stub\'s redirect row', [r[0] for r in rows] == ['wikilink'], rows)
S.done()
