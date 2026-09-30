"""The save contract (SCHEMA.md §2.4, §6.13, D19) through the reference implementation, against the real DDL. Expected outcome in each label.
mutate=('rule',...) disables reference-implementation rules; with --mutant the probes must fail."""
import sqlite3, sys, json, random, threading, time, os, tempfile
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import wikisave as W
from wikisave import save_memo, edit_body, sync_wikilinks, NOW
from vectors import V

MUT = tuple(a for a in sys.argv[1:] if not a.startswith('--'))
def fresh(path=':memory:'):
    c = sqlite3.connect(path, isolation_level=None)
    c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA recursive_triggers=ON'); c.execute('PRAGMA busy_timeout=5000')
    if path == ':memory:' or not os.path.getsize(path): c.executescript(open(os.environ['DDL']).read())
    return c
res, fails = [], []
def check(name, cond, detail=''):
    res.append(bool(cond))
    if not cond: fails.append(name)
    print(('PASS ' if cond else 'FAIL ') + name + (f' — {detail}' if detail else ''))
def links_of(c, pid): return sorted(r[0] for r in c.execute("SELECT p.title FROM links l JOIN pages p ON p.id=l.to_id WHERE l.from_id=? AND l.kind='wikilink'", (pid,)))
def orphans(c): return c.execute("SELECT count(*) FROM entities e WHERE type='page' AND NOT EXISTS (SELECT 1 FROM pages p WHERE p.id=e.id)").fetchone()[0]
def integrity(c):
    return c.execute('pragma integrity_check').fetchone()[0] == 'ok' and not c.execute('pragma foreign_key_check').fetchall()

# P1 the R4-12 scenario: valid + invalid targets in one memo — the memo IS saved, valid links made, invalid ones skipped
c = fresh()
try:
  pid, (ids, skipped) = save_memo(c, 'Diet: [[Health/Diet]], [[Re: plan]], [[Target|alias]], [[Good page]], #health #con #C', mutate=MUT)
except Exception as e:
  check('P1 save with invalid targets', False, repr(e)); pid, ids, skipped = 0, [], []
check('P1a memo is saved although two targets are invalid', c.execute('select body from pages where id=?', (pid,)).fetchone() is not None)
check('P1b links = C, Good page, Target, health (#C is a valid one-letter tag)', links_of(c, pid) == ['C', 'Good page', 'Target', 'health'], str(links_of(c, pid)))
check('P1c skipped (reported, not stored): Health/Diet, Re: plan, con', sorted(skipped) == ['Health/Diet', 'Re: plan', 'con'], str(skipped))
check('P1d no orphan entities row, DB consistent', orphans(c) == 0 and integrity(c))
check('P1e the body keeps the text verbatim', c.execute('select body from pages where id=?', (pid,)).fetchone()[0].startswith('Diet: [[Health/Diet]]'))

# P2 backstop: even with the validation predicate OFF the save survives, and leaves no orphan (SAVEPOINT)
c = fresh()
try:
    pid, (ids, skipped) = save_memo(c, 'x [[Health/Diet]] [[fine]]', mutate=tuple(set(MUT) | {'no_validation'}))
except Exception as e:
    check('P2 save survives a bad target even with the predicate off', False, repr(e)); pid, ids, skipped = c.execute('select coalesce(max(id),0) from pages').fetchone()[0], [], []
check('P2a predicate off: memo still committed', c.execute('select count(*) from pages where id=?', (pid,)).fetchone()[0] == 1)
check('P2b predicate off: bad target skipped, good target linked', links_of(c, pid) == ['fine'] and 'Health/Diet' in skipped, f'{links_of(c, pid)} {skipped}')
check('P2c predicate off: no orphan entities row', orphans(c) == 0, f'orphans={orphans(c)}')

# P3 sync = set equality: adding, removing, re-saving
c = fresh(); pid, _ = save_memo(c, '[[A]] [[B]] #t', mutate=MUT)
check('P3a initial links A, B, t', links_of(c, pid) == ['A', 'B', 't'])
edit_body(c, pid, '[[A]] [[C]]', mutate=MUT)
check('P3b edit: B and t dropped, C added', links_of(c, pid) == ['A', 'C'], str(links_of(c, pid)))
n1 = c.execute('select count(*), max(id) from links').fetchone(); edit_body(c, pid, '[[A]] [[C]]', mutate=MUT); n2 = c.execute('select count(*), max(id) from links').fetchone()
check('P3c re-saving the same body changes nothing (idempotent, no new rows)', n1 == n2, f'{n1} {n2}')
edit_body(c, pid, 'no links at all', mutate=MUT)
check('P3d body without links: all wikilinks removed', links_of(c, pid) == [])
check('P3e dropped targets survive as pages (and may become ghosts, §6.12)', c.execute("select count(*) from pages where kind='page'").fetchone()[0] == 4)

# P4 self link, stub
c = fresh()
c.execute('BEGIN IMMEDIATE'); wid = c.execute(f"INSERT INTO entities(type,created_at,updated_at,source) VALUES('page',{NOW},{NOW},'ui') RETURNING id").fetchone()[0]
c.execute("INSERT INTO pages(id,kind,title,title_key,body) VALUES(?, 'page','Diet','diet','')", (wid,)); c.execute('COMMIT')
edit_body(c, wid, 'About [[Diet]] and [[diet]] and [[Food]]', mutate=MUT)
check('P4a a page never links to itself', links_of(c, wid) == ['Food'], str(links_of(c, wid)))
c.execute('BEGIN IMMEDIATE'); old = c.execute(f"INSERT INTO entities(type,created_at,updated_at,source) VALUES('page',{NOW},{NOW},'ui') RETURNING id").fetchone()[0]
c.execute("INSERT INTO pages(id,kind,title,title_key,body) VALUES(?, 'page','Old diet','old diet','[[Food]] [[Stuff]]')", (old,)); c.execute('COMMIT')
edit_body(c, old, '[[Food]] [[Stuff]]', mutate=MUT)
n_before = len(links_of(c, old))
edit_body(c, old, '#REDIRECT [[Diet]]', mutate=MUT)
c.execute('BEGIN IMMEDIATE'); c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?, 'redirect', {NOW}, 'ui')", (old, wid)); c.execute('COMMIT')
check('P4b a stub has no wikilink edges (old links dropped, new not made) and no page "REDIRECT"', n_before == 2 and links_of(c, old) == [] and c.execute("select count(*) from pages where title_key='redirect'").fetchone()[0] == 0, f'{n_before} {links_of(c, old)}')
check('P4c the stub\'s one edge is the redirect link', c.execute("select count(*) from links where from_id=? and kind='redirect'", (old,)).fetchone()[0] == 1)

# P5 tombstoned target is revived, not duplicated
c = fresh(); pid, _ = save_memo(c, '[[Junk]]', mutate=MUT)
jid = c.execute("select id from pages where title='Junk'").fetchone()[0]
c.execute(f"UPDATE entities SET deleted_at={NOW} WHERE id=?", (jid,))
pid2, _ = save_memo(c, 'again [[JUNK]]', mutate=MUT)
check('P5a one page, revived', c.execute("select count(*) from pages where title_key='junk'").fetchone()[0] == 1 and c.execute('select deleted_at from entities where id=?', (jid,)).fetchone()[0] is None)
check('P5b both memos link to it', links_of(c, pid) == ['Junk'] and links_of(c, pid2) == ['Junk'])

# P6 incremental == rebuild, over random edit sequences
frags = ['[[Alpha]]', '[[alpha|a]]', '#beta', '`[[code]]`', '[[Bad/Name]]', '```\n[[fence]]\n```', '[[Ünï]]', '[[UNÏ]]', 'text', '#Beta', '[[Gamma delta]]', '#12', '[[CON]]', '#REDIRECTED']
rng = random.Random(6)
c = fresh(); pages = [save_memo(c, 'seed', mutate=MUT)[0] for _ in range(6)]
for step in range(400):
    p = rng.choice(pages); body = ' '.join(rng.choice(frags) for _ in range(rng.randint(0, 6)))
    edit_body(c, p, body, mutate=MUT)
inc = {p: links_of(c, p) for p in pages}
# rebuild into a fresh DB from the final bodies only
c2 = fresh(); rb = {}
for p in pages:
    body = c.execute('select body from pages where id=?', (p,)).fetchone()[0]
    q, _ = save_memo(c2, body, mutate=MUT); rb[p] = links_of(c2, q)
check('P6a incremental maintenance == rebuild from bodies (6 pages, 400 random edits)', inc == rb)
check('P6b no orphans, integrity ok after 400 edits', orphans(c) == 0 and integrity(c))
check('P6c at most one page per title_key', c.execute("select count(*) from (select title_key from pages where title_key is not null group by 1 having count(*)>1)").fetchone()[0] == 0)

# P7 two real writers save memos with the same new tag/target at the same time
path = os.path.join(tempfile.mkdtemp(dir=os.environ.get('SCRATCH') or None), 'life.db')
c0 = fresh(path); c0.execute('PRAGMA journal_mode=WAL'); c0.close()
errs, out = [], []
def writer(i):
    try:
        cc = fresh(path); t0 = time.time()
        pid, _ = save_memo(cc, f'writer {i} about [[Shared topic]] #shared', mutate=MUT); out.append((i, pid, time.time() - t0))
    except Exception as e: errs.append(repr(e))
ths = [threading.Thread(target=writer, args=(i,)) for i in range(4)]
[t.start() for t in ths]; [t.join() for t in ths]
cv = fresh(path)
check('P7a four concurrent saves: no errors', not errs, str(errs))
check('P7b one page per shared target, four memos linked to each', cv.execute("select count(*) from pages where title_key in ('shared topic','shared')").fetchone()[0] == 2 and cv.execute("select count(*) from links where kind='wikilink'").fetchone()[0] == 8)

# P8 the vectors, end to end through the DB (links == vector result)
ok = True
for label, body, exp in V:
    c = fresh(); pid, _ = save_memo(c, body, mutate=MUT)
    if sorted(links_of(c, pid)) != sorted(exp): ok = False; print('   vector via DB differs:', label, links_of(c, pid), exp)
check(f'P8 all {len(V)} vectors give the same links through a real save', ok)
# P9 the extraction vectors alone
bad = [l for l, b, e in V if list(W.targets(b, None, MUT)[0].values()) != e]
check('P9 extraction vectors', not bad, str(bad[:6]))
print(f'{sum(res)}/{len(res)} probes passed' + (f'; failing: {fails}' if fails else ''))
