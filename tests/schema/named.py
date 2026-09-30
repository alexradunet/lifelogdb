"""A person, place or holding is a page (SCHEMA.md §2.2, D20): one id with an entities row, a titled pages row and a
domain row, chained domain -> pages -> entities.
A  the foreign keys and CHECKs: what may and may not be built, and what a promotion may and may not do;
B  §6.19 run literally: create, promote a ghost, a taken handle, the memos that name someone;
C  a memo that writes [[Name]] reaches the person through the real save contract; §6.6; renames; rebuild from bodies;
D  two Sams, two Springfields; ghost_pages leaves named pages alone."""
import os, re, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
import wikisave as W
S = Suite('named entities')

# ---- A  structure
c = fresh()
for t in NAMED:
    i = named(c, t, f'H {t}')
    S.K(f'a {t} is one id: entities.type, pages.entity_type and its domain row agree',
        one(c, 'select e.type = p.entity_type from entities e join pages p using(id) where id=?', (i,)) == 1 and one(c, 'select count(*) from entities') == len(NAMED[:NAMED.index(t) + 1]))
for t, tbl in (('person', 'people'), ('place', 'places'), ('holding', 'holdings')):
    e = ent(c, t)
    S.K(f'a {t} without a page is refused (its FK points at pages)', tryx(c, f'INSERT INTO {tbl}(id{",name" if t == "person" else ""}{",side,currency" if t == "holding" else ""}) VALUES (?{",?" if t == "person" else ""}{",?,?" if t == "holding" else ""})',
        (e, 'x') if t == 'person' else (e, 'asset', 'EUR') if t == 'holding' else (e,)).startswith('ERR'))
m = memo(c, 'a memo')
S.K('a named page must be titled: a memo cannot be a person\'s page', tryx(c, "INSERT INTO pages(id,entity_type,kind,day) VALUES (?, 'person', 'memo', '2026-09-30')", (ent(c, 'person'),)).startswith('ERR'))
S.K('the page and the entity must agree on the type', tryx(c, "INSERT INTO pages(id,entity_type,kind,title,title_key) VALUES (?, 'place', 'page', 'Mismatch', 'mismatch')", (ent(c, 'person'),)).startswith('ERR'))
S.K('an event or a task cannot have a pages row', all(tryx(c, "INSERT INTO pages(id,entity_type,kind,title,title_key) VALUES (?, ?, 'page', ?, ?)", (ent(c, t), t, 'X' + t, 'x' + t)).startswith('ERR') for t in ('event', 'task')))
S.K('a person row with no people row, only a page, is what the orphan query is for (the FKs allow the page alone)',
    tryx(c, "INSERT INTO pages(id,entity_type,kind,title,title_key) VALUES (?, 'person', 'page', 'Half', 'half')", (ent(c, 'person'),)) == 'OK')
cols = lambda t: [r[1] for r in c.execute(f"pragma table_info('{t}')")]
S.K('places and holdings have no name column (the title is the name); people keep name; none has notes',
    'name' not in cols('places') and 'name' not in cols('holdings') and 'name' in cols('people') and all('notes' not in cols(t) for t in ('people', 'places', 'holdings')))
S.K('entities has no page_id column', 'page_id' not in cols('entities'))
# promotion and its limits
c = fresh(); g = page(c, 'Ana'); mm = memo(c, '[[Ana]]'); link(c, mm, g, 'wikilink')
r1 = tryx(c, "UPDATE entities SET type='person' WHERE id=? AND type='page'", (g,)); r2 = tryx(c, "INSERT INTO people(id,name) VALUES (?, 'Ana Example')", (g,))
S.K('a plain page is promoted: UPDATE entities.type cascades to pages.entity_type, then the people row', r1 == r2 == 'OK'
    and c.execute('select e.type, p.entity_type from entities e join pages p using(id) where id=?', (g,)).fetchone() == ('person', 'person'), (r1, r2))
S.K('...and the memo\'s link to it is kept (the id did not change)', c.execute("select to_id from links where from_id=?", (mm,)).fetchall() == [(g,)])
S.K('a promoted person cannot be turned back into a plain page (the people row\'s FK)', tryx(c, "UPDATE entities SET type='page' WHERE id=?", (g,)).startswith('ERR'))
S.K('...nor into a place', tryx(c, "UPDATE entities SET type='place' WHERE id=?", (g,)).startswith('ERR'))
S.K('a memo cannot be promoted (pages_named_titled)', 'pages_named_titled' in tryx(c, "UPDATE entities SET type='person' WHERE id=?", (mm,)))
S.K('a page cannot become an event (pages_entity_type)', 'pages_entity_type' in tryx(c, "UPDATE entities SET type='event' WHERE id=?", (page(c, 'Ev'),)))
ev = thing(c, 'event')
S.K('an event cannot change type (its FK has no cascade)', tryx(c, "UPDATE entities SET type='task' WHERE id=?", (ev,)).startswith('ERR'))
S.K('the page of a person cannot be deleted', tryx(c, 'DELETE FROM pages WHERE id=?', (g,)).startswith('ERR'))
S.K('tombstoning the person keeps its page', tryx(c, f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (g,)) == 'OK' and one(c, 'select title from pages where id=?', (g,)) == 'Ana')
S.K('integrity and foreign keys clean', integrity_ok(c))

# ---- B  §6.19 run literally
sts = statements(block('6.19'))
sel0 = [s for s in sts if code(s).upper().startswith('SELECT P.ID')]
create = sts[sts.index(sel0[0]) + 1:][:5] if sel0 else []
promo = sts[sts.index(sel0[0]) + 6:][:4] if sel0 else []
memos_q = [s for s in sts if 'm.kind = \'memo\'' in s]
S.K('§6.19 has a resolve, a five-statement create, a four-statement promotion and the memo query',
    len(sel0) == 1 and [re.match(r'\w+', code(s)).group(0).upper() for s in create] == ['BEGIN', 'INSERT', 'INSERT', 'INSERT', 'COMMIT']
    and [re.match(r'\w+', code(s)).group(0).upper() for s in promo] == ['BEGIN', 'UPDATE', 'INSERT', 'COMMIT'] and len(memos_q) == 1, [code(s)[:25] for s in sts])
if len(sel0) == 1 and len(memos_q) == 1 and len(create) == 5 and len(promo) == 4:
    c = fresh(); P = dict(handle_title='Bob Sample', handle_key='bob sample')
    S.K('step 0 finds nothing for a new handle', c.execute(sel0[0], {k: P[k] for k in ('handle_key',)}).fetchall() == [])
    for st in create: run_block(c, st, P)
    pid = P.get('person_id')
    S.K('create: one id, a person with its page titled by the handle', c.execute('select e.type, p.entity_type, p.title, pe.name from entities e join pages p using(id) join people pe using(id)').fetchall()
        == [('person', 'person', 'Bob Sample', 'Bob Sample')] and pid is not None)
    S.K('step 0 now reports the handle as taken (entity_type person)', c.execute(sel0[0], {'handle_key': 'bob sample'}).fetchall() == [(pid, 'person')])
    S.K('the same handle cannot be made twice, in any case', 'title_key' in tryx(c, "INSERT INTO pages(id,kind,title,title_key) VALUES (?, 'page', 'BOB SAMPLE', 'bob sample')", (ent(c, 'page'),)))
    S.K('a place and a holding are created the same way', named(c, 'place', 'Berlin') and named(c, 'holding', 'Main checking') and one(c, "select count(*) from pages where entity_type <> 'page'") == 3)
    # promote a ghost an earlier memo made
    c = fresh(); mid, _ = W.save_memo(c, 'Today I met [[Ana Example]]')
    gid = one(c, "select id from pages where title_key='ana example'")
    S.K('step 0 finds the ghost the memo made, as a plain page', c.execute(sel0[0], {'handle_key': 'ana example'}).fetchall() == [(gid, 'page')])
    for st in promo: run_block(c, st, dict(ghost_id=gid))
    S.K('the promotion block turns it into a person, one id', c.execute('select e.type, pe.name from entities e join people pe using(id) where id=?', (gid,)).fetchall() == [('person', 'Ana Example')])
    S.K('...and the old memo already names her, no re-save needed', [r[0] for r in c.execute(memos_q[0], {'person_id': gid})] == [mid])
    S.K('promoting a memo fails in the UPDATE', tryx(c, promo[1], {'ghost_id': mid}).startswith('ERR'))
    S.K('promoting a page that is already a person changes no row, and the people insert fails', tryx(c, promo[1], {'ghost_id': gid}) == 'OK'
        and c.execute('select changes()').fetchone()[0] == 0 and tryx(c, promo[2], {'ghost_id': gid}).startswith('ERR'))
    pl = named(c, 'place', 'Lisbon')
    S.K('promoting a place\'s page into a person changes no row, and the people insert fails on its FK', tryx(c, promo[1], {'ghost_id': pl}) == 'OK' and tryx(c, promo[2], {'ghost_id': pl}).startswith('ERR'))

# ---- C  the save contract reaches the person
c = fresh(); bod = named(c, 'person', 'Bob Sample', name='Bob Sample')
m1, _ = W.save_memo(c, 'Today I met [[Bob Sample]] and went with him for a coffee', '2026-09-28')
m2, _ = W.save_memo(c, 'Nothing about him today', '2026-09-29')
m3, _ = W.save_memo(c, '[[Bob Sample|Bob]] called', '2026-09-30')
m4, _ = W.save_memo(c, 'Bob was here, but I wrote [[Bbo Sample]]', '2026-09-30')
wiki = page(c, 'Coffee spots'); W.edit_body(c, wiki, 'Best one: [[Bob Sample]] goes there')
W.edit_body(c, bod, 'Colleague since 2019. Lives in [[Cluj]].')
ev = thing(c, 'event'); link(c, bod, ev, 'attended')
S.K('the memo links to the person\'s own id, and no new page was made', c.execute("select to_id from links where from_id=? and kind='wikilink'", (m1,)).fetchall() == [(bod,)]
    and one(c, "select count(*) from pages where title_key='bob sample'") == 1)
S.K('an alias does not change the target', c.execute('select to_id from links where from_id=?', (m3,)).fetchall() == [(bod,)])
S.K('the person\'s page body links out like any page', c.execute("select p.title from links l join pages p on p.id=l.to_id where l.from_id=? and l.kind='wikilink'", (bod,)).fetchall() == [('Cluj',)])
ark = c.execute(block('6.6'), {'entity_id': bod}).fetchall()
S.K('§6.6 finds the memos and the page that name him, what his page links to, and what he attended', sorted((r[0], r[2], r[3]) for r in ark)
    == sorted([('wikilink', m1, 'in'), ('wikilink', m3, 'in'), ('wikilink', wiki, 'in'), ('wikilink', one(c, "select id from pages where title='Cluj'"), 'out'), ('attended', ev, 'out')]), ark)
S.K('...and not the typo', m4 not in [r[2] for r in ark])
S.K('§6.6 is two legs (the third leg through a separate page is gone)', block('6.6').count('UNION ALL') == 1)
q = memos_q[0] if memos_q else 'select 1'
S.K('§6.19 lists the memos only, newest day first', [r[0] for r in c.execute(q, {'person_id': bod})] == [m3, m1])
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (m3,))
S.K('...and drops a tombstoned memo', [r[0] for r in c.execute(q, {'person_id': bod})] == [m1])
bl = c.execute(block('6.5'), {'page_id': bod}).fetchall()
S.K('§6.5 labels the backlinks of a person by title or memo text', sorted(r[3] for r in bl) == sorted(['Coffee spots', 'Today I met [[Bob Sample]] and went w']), bl)
before = sorted(c.execute("select from_id, to_id from links where kind='wikilink'").fetchall())
c.execute("UPDATE people SET name='Bob S.', nickname='Bobby' WHERE id=?", (bod,))
for pid, body in c.execute('select id, body from pages').fetchall():
    if body: W.edit_body(c, pid, body)
S.K('renaming the person (people.name) drops no link: the link is to the id, the handle is the title', sorted(c.execute("select from_id, to_id from links where kind='wikilink'").fetchall()) == before)
c.execute("DELETE FROM links WHERE kind='wikilink'")
for pid, body in c.execute('select id, body from pages').fetchall():
    if body: W.edit_body(c, pid, body)
S.K('a rebuild from the bodies alone gives back every link, people included', sorted(c.execute("select from_id, to_id from links where kind='wikilink'").fetchall()) == before)

# ---- D  two Sams, two Springfields, the ghost view
c = fresh(); s1 = named(c, 'person', 'Sam', name='Sam')
S.K('a second Sam cannot take the handle Sam, in any case', all('title_key' in tryx(c, "INSERT INTO pages(id,kind,title,title_key) VALUES (?, 'page', ?, ?)", (ent(c, 'page'), t, title_key(t))) for t in ('Sam', 'SAM', 'sam')))
s2 = named(c, 'person', 'Sam (barber)', name='Sam')
ma, _ = W.save_memo(c, 'Met [[Sam]]'); mb, _ = W.save_memo(c, 'Haircut with [[Sam (barber)]]')
S.K('two people named Sam, told apart in the handle; each memo reaches its own', c.execute('select to_id from links where from_id=?', (ma,)).fetchall() == [(s1,)] and c.execute('select to_id from links where from_id=?', (mb,)).fetchall() == [(s2,)])
S.K('two places are told apart only once: Springfield (IL) and Springfield (MA)', named(c, 'place', 'Springfield (IL)') and named(c, 'place', 'Springfield (MA)'))
named(c, 'holding', 'Flat (Berlin)'); gp = page(c, 'Typo page')
c.execute("UPDATE entities SET created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now','-40 day')")
gh = [r[1] for r in c.execute('select id, title from ghost_pages')]
S.K('ghost_pages lists the real ghost and none of the person, place or holding pages', gh == ['Typo page'], gh)
S.done()
