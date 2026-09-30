"""Round-16 probes (run with the tests venv:  python3 r16probes.py DDLFILE).  Expected outcome is in each label.
D20: a person, place or holding is also a page, and `[[Bob Sample]]` reaches it through the title.
A  The DDL: a named type must have a page and every other type must not; the page is unique across types, a titled page
   and not a memo, and fixed after insert; people, places and holdings have no `notes`.
B  §6.20 run literally from the document: a person, then a place and a holding the same way; promoting a ghost page;
   a handle somebody owns is refused.
C  A memo that writes the name reaches the person: §6.6's third leg and §6.20's memo query, through the real save
   contract; a typo makes a ghost page; renaming a person drops no link; a rebuild from the bodies gives every link.
D  Two people called Sam are told apart in the handle; ghost_pages leaves a named page alone and still lists a real ghost.
E  Document text: D20, the §2.3 bullet, the meta key, the §7 row and the `mention` kind gone, §6.13 and §3 say the same."""
import json, os, re, sqlite3, sys
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, '..', 'lib')); sys.path.insert(0, os.path.join(HERE, '..', 'wikilinks'))
import docsql, wikisave as W
DDL = open(sys.argv[1], encoding='utf-8').read()
doc = docsql.doc_text(); live = doc[:doc.index('## 8. References')]
NOW = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
res = []
def K(label, cond, detail=''):
    res.append(bool(cond))
    if not cond: print('  FAIL', label, str(detail)[:240])
def fresh():
    c = sqlite3.connect(':memory:', isolation_level=None)
    c.executescript(DDL); c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA recursive_triggers=ON'); return c
def tryx(c, sql, args=()):
    try: c.execute(sql, args); return 'OK'
    except sqlite3.Error as e: return 'ERR ' + str(e)
def ent(c, typ, page_id=None):
    return c.execute(f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES (?,{NOW},{NOW},?) RETURNING id", (typ, page_id)).fetchone()[0]
def page(c, title):
    i = ent(c, 'page'); c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES (?, 'page', ?, ?)", (i, title, W.title_key(title))); return i
def memo_row(c, body='x'):
    i = ent(c, 'page'); c.execute("INSERT INTO pages(id,kind,day,body) VALUES (?, 'memo', '2026-09-30', ?)", (i, body)); return i
DOMAIN = dict(person="INSERT INTO people(id,name) VALUES (?, 'P')", place="INSERT INTO places(id,name) VALUES (?, 'Pl')",
              holding="INSERT INTO holdings(id,name,side,currency) VALUES (?, 'A', 'asset', 'EUR')")
def named(c, typ, handle):
    e = ent(c, typ, page(c, handle)); c.execute(DOMAIN[typ].replace("'P'", f"'{handle}'").replace("'Pl'", f"'{handle}'").replace("'A'", f"'{handle}'"), (e,)); return e
def statements(sql):
    out, acc = [], ''
    for line in sql.splitlines(keepends=True):
        acc += line
        if sqlite3.complete_statement(acc):
            if re.sub(r'--[^\n]*', '', acc).strip(): out.append(acc)
            acc = ''
    return out
def code(st): return re.sub(r'--[^\n]*', '', st).strip()
def blocks(): return dict((h.split()[0], s) for h, s in docsql.cookbook_blocks(doc))
B620 = statements(blocks()['6.20']); B66 = statements(blocks()['6.6'])[0]
def by(prefix, sts=B620): return [s for s in sts if code(s).upper().startswith(prefix.upper())]
def run(c, st, P):
    cur = c.execute(st, {k: v for k, v in P.items() if ':' + k in st}); rows = cur.fetchall() if cur.description else None
    m = re.search(r'RETURNING id;[^\n]*?:(\w+)', st)
    if m and rows: P[m.group(1)] = rows[0][0]
    return rows

# ---- A  the DDL
NAMED = ('person', 'place', 'holding'); OTHER = ('page', 'event', 'task')
c = fresh()
for t in NAMED:
    K(f'A a {t} without a page is refused', tryx(c, f"INSERT INTO entities(type,created_at,updated_at) VALUES ('{t}',{NOW},{NOW})").startswith('ERR'))
    K(f'A a {t} with a page is accepted', tryx(c, f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('{t}',{NOW},{NOW},?)", (page(c, f'h-{t}'),)) == 'OK')
for t in OTHER:
    K(f'A a {t} with a page is refused (only named types have one)', tryx(c, f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('{t}',{NOW},{NOW},?)", (page(c, f'x-{t}'),)).startswith('ERR'))
    K(f'A a {t} without a page is accepted', tryx(c, f"INSERT INTO entities(type,created_at,updated_at) VALUES ('{t}',{NOW},{NOW})") == 'OK')
c = fresh(); pg = page(c, 'Paris')
pe = ent(c, 'person', pg)
K('A a page is one thing\'s page: a place cannot take the page of a person', 'UNIQUE' in tryx(c, f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('place',{NOW},{NOW},?)", (pg,)))
K('A ...nor a second person', 'UNIQUE' in tryx(c, f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('person',{NOW},{NOW},?)", (pg,)))
mm = memo_row(c, 'a memo')
K('A a memo cannot be a page of a place (only a titled page can)', 'not a memo' in tryx(c, f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('place',{NOW},{NOW},?)", (mm,)))
K('A a page_id that is no page is refused', tryx(c, f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('place',{NOW},{NOW},9999)").startswith('ERR'))
c2 = fresh(); c2.execute('DROP TRIGGER entities_page_is_a_page')      # the foreign key alone, without the trigger that also looks the page up
K('A ...by the foreign key itself, too', 'FOREIGN KEY' in tryx(c2, f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('place',{NOW},{NOW},9999)"))
K('A the page cannot be swapped afterwards', 'never changed' in tryx(c, 'UPDATE entities SET page_id = ? WHERE id = ?', (page(c, 'Other'), pe)))
K('A ...nor removed', tryx(c, 'UPDATE entities SET page_id = NULL WHERE id = ?', (pe,)).startswith('ERR'))
K('A a full-row update that changes nothing still passes', tryx(c, 'UPDATE entities SET page_id = page_id, source = source WHERE id = ?', (pe,)) == 'OK')
K('A tombstoning the person leaves the page in place', tryx(c, f"UPDATE entities SET deleted_at = {NOW} WHERE id = ?", (pe,)) == 'OK' and c.execute('SELECT page_id FROM entities WHERE id=?', (pe,)).fetchone()[0] == pg)
K('A the page of a person cannot be deleted either (no hard deletes)', tryx(c, 'DELETE FROM pages WHERE id = ?', (pg,)).startswith('ERR'))
cols = lambda t: [r[1] for r in c.execute(f"pragma table_info('{t}')")]
K('A people, places and holdings have no notes column (the page body holds the prose)', all('notes' not in cols(t) for t in ('people', 'places', 'holdings')) and 'notes' in cols('events'))
K('A the integrity checks are clean', c.execute('PRAGMA integrity_check').fetchall() == [('ok',)] and not c.execute('PRAGMA foreign_key_check').fetchall())

# ---- B  §6.20 run literally
K('B §6.20 has the statements the text describes: a SELECT, two inserts into entities, one into pages, one into people, and three reads',
  len(by('SELECT')) == 4 and len(by('INSERT INTO entities')) == 2 and len(by('INSERT INTO pages')) == 1 and len(by('INSERT INTO people')) == 1 and len(by('BEGIN IMMEDIATE')) == 1, [code(s)[:30] for s in B620])
c = fresh(); P = dict(handle_title='Bob Sample', handle_key=W.title_key('Bob Sample'))
outs = [run(c, st, P) for st in B620[:B620.index(by('SELECT')[1])]]      # BEGIN .. COMMIT, stopping before the reads
K('B step 0 found no page for a new handle', outs[1] == [], outs[:2])
K('B a person exists, with the page the handle names', c.execute("SELECT p.title FROM entities e JOIN people pe ON pe.id = e.id JOIN pages p ON p.id = e.page_id").fetchall() == [('Bob Sample',)])
K('B step 0 now says the handle is owned', c.execute(by('SELECT')[0], {k: v for k, v in P.items() if k.startswith('handle_')}).fetchall() == [(P['handle_page_id'], 1)])
K('B a second person on that page is refused', 'UNIQUE' in tryx(c, f"INSERT INTO entities(type,created_at,updated_at,page_id) VALUES ('person',{NOW},{NOW},?)", (P['handle_page_id'],)))
K('B the same handle cannot be made twice (title_key)', 'title_key' in tryx(c, "INSERT INTO pages(id,kind,title,title_key) VALUES (?, 'page', 'BOB SAMPLE', 'bob sample')", (ent(c, 'page'),)))
P.update(person_id=P['new_person_id'], page_id=P['handle_page_id'])
R = [run(c, st, P) for st in by('SELECT')[1:]]
K('B the page of an entity, and the entity of a page', R[0] and R[0][0][1] == 'Bob Sample' and R[1] == [(P['person_id'], 'person')], R[:2])
K('B a place and a holding take the same four statements', named(c, 'place', 'Berlin') and named(c, 'holding', 'Main checking') and c.execute("SELECT count(*) FROM entities WHERE page_id IS NOT NULL").fetchone()[0] == 3)
# promotion: a memo writes the name first; the ghost page it made is adopted later
c = fresh(); mid, _ = W.save_memo(c, 'Today I met [[Bbo Sample]] and went with him for a coffee')
gp = c.execute("SELECT id FROM pages WHERE title_key = 'bodgan sample'").fetchone()[0]
P = dict(handle_key='bodgan sample', handle_title='Bbo Sample')
sel = run(c, by('SELECT')[0], P)
K('B step 0 finds the ghost page the memo made, unowned', sel == [(gp, 0)], sel)
P['handle_page_id'] = gp                                               # adopt: skip steps 1 and 2
for st in by('INSERT INTO entities')[1:] + by('INSERT INTO people'): run(c, st, P)
K('B the person adopts the ghost page: one page for the handle, and it is the person\'s', c.execute("SELECT count(*) FROM pages WHERE title_key = 'bodgan sample'").fetchone()[0] == 1
  and c.execute("SELECT e.id FROM entities e WHERE e.page_id = ?", (gp,)).fetchone()[0] == P['new_person_id'])
P['person_id'] = P['new_person_id']; memos = run(c, by('SELECT')[3], P)
K('B ...and the old memo is already about him, no re-save needed', [r[0] for r in memos] == [mid], memos)

# ---- C  a memo that writes the name reaches the person
c = fresh(); P = dict(handle_title='Bob Sample', handle_key='bob sample')
for st in B620[:B620.index(by('SELECT')[1])]: run(c, st, P)
bod, bpage = P['new_person_id'], P['handle_page_id']
m1, _ = W.save_memo(c, 'Today I met [[Bob Sample]] and went with him for a coffee', '2026-09-28')
m2, _ = W.save_memo(c, 'Nothing about him today', '2026-09-29')
m3, _ = W.save_memo(c, '[[Bob Sample|Bob]] called', '2026-09-30')
m4, _ = W.save_memo(c, 'Bob was here, but I wrote [[Bbo Sample]]', '2026-09-30')
wiki = page(c, 'Coffee spots'); W.edit_body(c, wiki, 'Best one: [[Bob Sample]] goes there')
K('C the save contract linked the memo to the person\'s page, with no new page', c.execute("SELECT to_id FROM links WHERE from_id = ? AND kind = 'wikilink'", (m1,)).fetchall() == [(bpage,)] and c.execute("SELECT count(*) FROM pages WHERE title_key = 'bob sample'").fetchone()[0] == 1)
K('C ...and an alias does not change the target', c.execute("SELECT to_id FROM links WHERE from_id = ?", (m3,)).fetchall() == [(bpage,)])
ark = c.execute(B66, {'entity_id': bod}).fetchall()
via = sorted(r[2] for r in ark if r[3] == 'in via page')
K('C §6.6\'s third leg finds the memos and the wiki page that name him, as kind wikilink', via == sorted([m1, m3, wiki]) and all(r[0] == 'wikilink' for r in ark if r[3] == 'in via page'), ark)
K('C ...and the typo (Bbo) is not among them', m4 not in via)
q = run(c, by('SELECT')[3], dict(person_id=bod))
K('C §6.20 lists the memos only, newest day first, each once', [r[0] for r in q] in ([m3, m1], ) and all(len(r) == 3 for r in q), q)
c.execute(f"UPDATE entities SET deleted_at = {NOW} WHERE id = ?", (m3,))
K('C a tombstoned memo drops out of the list', [r[0] for r in run(c, by('SELECT')[3], dict(person_id=bod))] == [m1])
K('C the typo made an ordinary ghost page and no link to the person', c.execute("SELECT count(*) FROM pages WHERE title_key = 'bodgan sample'").fetchone()[0] == 1 and (bpage not in [r[0] for r in c.execute('SELECT to_id FROM links WHERE from_id = ?', (m4,))]))
before = sorted(c.execute("SELECT from_id, to_id FROM links WHERE kind = 'wikilink'").fetchall())
c.execute("UPDATE people SET name = 'Bob S.', nickname = 'Bobby' WHERE id = ?", (bod,))
for pid, body in c.execute("SELECT id, body FROM pages").fetchall():
    if body: W.edit_body(c, pid, body)
K('C renaming the person drops no link (the link goes to the page, not to the name)', sorted(c.execute("SELECT from_id, to_id FROM links WHERE kind = 'wikilink'").fetchall()) == before)
c.execute("DELETE FROM links WHERE kind = 'wikilink'")
for pid, body in c.execute("SELECT id, body FROM pages").fetchall():
    if body: W.edit_body(c, pid, body)
K('C a rebuild from the bodies alone gives back every link, with people in the database', sorted(c.execute("SELECT from_id, to_id FROM links WHERE kind = 'wikilink'").fetchall()) == before)
c.execute(f"UPDATE entities SET deleted_at = {NOW} WHERE id = ?", (bod,))
W.edit_body(c, m1, 'Today I met [[Bob Sample]] and went with him for a coffee')
K('C a tombstoned person keeps the page, and the memo still links to it', c.execute("SELECT to_id FROM links WHERE from_id = ?", (m1,)).fetchall() == [(bpage,)])

# ---- D  two Sams; the ghost view
c = fresh(); s1 = named(c, 'person', 'Sam'); p1 = c.execute('SELECT page_id FROM entities WHERE id=?', (s1,)).fetchone()[0]
K('D a second Sam is refused a page called Sam, in any case', all('title_key' in tryx(c, "INSERT INTO pages(id,kind,title,title_key) VALUES (?, 'page', ?, ?)", (ent(c, 'page'), t, W.title_key(t))) for t in ('Sam', 'SAM', 'sam')))
s2 = named(c, 'person', 'Sam (barber)')
ma, _ = W.save_memo(c, 'Met [[Sam]]'); mb, _ = W.save_memo(c, 'Haircut with [[Sam (barber)]]')
K('D the two Sams are told apart in the handle, and each memo reaches its own', c.execute("SELECT e.id FROM links l JOIN entities e ON e.page_id = l.to_id WHERE l.from_id = ?", (ma,)).fetchone() == (s1,) and c.execute("SELECT e.id FROM links l JOIN entities e ON e.page_id = l.to_id WHERE l.from_id = ?", (mb,)).fetchone() == (s2,))
named(c, 'place', 'Tokyo'); named(c, 'holding', 'Flat (Berlin)'); ghost = page(c, 'Typo page')
c.execute("UPDATE entities SET created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now','-40 day')")
gh = [r[1] for r in c.execute('SELECT id, title FROM ghost_pages')]
K('D ghost_pages lists the real ghost and none of the person, place or holding pages', gh == ['Typo page'], gh)

# ---- E  the document text
s203 = docsql.section(doc, r'^### 2\.3 ', r'^### 2\.4 '); d20 = doc[doc.index('### D20 '):doc.index('## 6. Query cookbook')]
K('E D20 exists, and §2.3 names the rule', 'Named entities are pages' in d20 and 'Named entities have a page (D20)' in s203)
K('E D20 names the rejected ways and the cost it accepts', all(x in d20 for x in ('`mention` link kind', '`page_id` on each', '`pages.subject_id`', '**Costs accepted.**')))
K('E the meta key says how a wikilink reaches a person', 'named_pages' in DDL and 'never changed' in DDL[DDL.index("'named_pages'"):][:400])
K('E no `mention` link kind, and no §7 row for typing a name', 'mention' not in DDL and "Typing a person's name" not in doc)
K('E §2.5 no longer says a wikilink never reaches a person', 'never reaches the `people` row' not in live and 'Named pages (D20)' in live)
view = DDL[DDL.index('CREATE VIEW ghost_pages'):][:900]; g613 = blocks()['6.13']
K('E §3 and §6.13 both exclude the named pages from the ghost view', 'n.page_id = p.id' in view and 'n.page_id = p.id' in g613)
K('E §6.6 is titled for any named entity and binds :entity_id, with a third leg', '### 6.6 Everything about a person, place or holding' in doc and ':person_id' not in B66 and B66.count('UNION ALL') == 2)
lm = doc[doc.index('%% diagram: link-map'):][:1400]
K('E the link map has no mention edge and the ER diagram draws pages -> entities (page_id)', 'mention' not in lm and 'pages    |o--o| entities : "page_id"' in doc)

print(f'round-16 probes: {sum(res)}/{len(res)} met expectations')
