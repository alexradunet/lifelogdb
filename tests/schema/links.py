"""The graph (SCHEMA.md D8, D16): the closed link-kind registry, endpoint types, symmetric mirrors, immutability,
containment (§6.18) and subtasks with their cycle guards, and the INSERT OR REPLACE trap."""
import os, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('links')

c = fresh()
pa, pb = named(c, 'person'), named(c, 'person'); pl = named(c, 'place'); ho = named(c, 'holding')
ta, tb = thing(c, 'task'), thing(c, 'task'); m1 = day_page(c); pw = page(c, 'Wiki')
for lbl, exp, f, t, k in [
    ('friend person-person', 'OK', pa, pb, 'friend'), ('an unregistered kind (Friend)', 'ERR', pa, pb, 'Friend'), ('lives-in is not a kind', 'ERR', pa, pl, 'lives-in'),
    ('attended is not a kind (no events, D22)', 'ERR', pa, pw, 'attended'), ('is-a is not a kind (no events, D22)', 'ERR', pw, pw, 'is-a'), ('friend person->place', 'ERR', pa, pl, 'friend'),
    ('subtask task->task', 'OK', ta, tb, 'subtask'), ('subtask page->page', 'ERR', m1, pw, 'subtask'), ('spawned task->day page', 'OK', ta, m1, 'spawned'),
    ('spawned task->task', 'ERR', ta, tb, 'spawned'), ('wikilink day page->page', 'OK', m1, pw, 'wikilink'), ('wikilink day page->person (a person is a page)', 'OK', m1, pa, 'wikilink'),
    ('wikilink day page->place', 'OK', m1, pl, 'wikilink'), ('wikilink day page->holding', 'OK', m1, ho, 'wikilink'), ('wikilink person page->page', 'OK', pa, pw, 'wikilink'),
    ('wikilink page->task', 'ERR', m1, ta, 'wikilink'), ('wikilink task->page', 'ERR', ta, pw, 'wikilink'), ('redirect page->page', 'OK', m1, pw, 'redirect'),
    ('redirect page->person', 'ERR', pw, pa, 'redirect'), ('about day page->person', 'OK', m1, pb, 'about'), ('about task->holding', 'OK', ta, ho, 'about'),
    ('about day page->page', 'ERR', m1, pw, 'about'), ('about day page->task', 'ERR', m1, ta, 'about'), ('related task-person', 'OK', ta, pa, 'related'),
    ('visited person->place', 'OK', pa, pl, 'visited'), ('visited page->place (a day page names the place instead)', 'ERR', m1, pl, 'visited'),
    ('visited place->person', 'ERR', pl, pa, 'visited'), ('a dangling endpoint of a typed kind', 'ERR', 9999, pl, 'visited'),
    ('located-in place->person', 'ERR', pl, pa, 'located-in'), ('parent-of person->place', 'ERR', pa, pl, 'parent-of')]:
    r = link(c, f, t, k); S.K(f'link {lbl}: {exp}', r.startswith(exp), r)
S.K('a duplicate edge is refused (UNIQUE from, to, kind)', link(c, pa, pl, 'visited').startswith('ERR'))
S.K('a symmetric kind is stored in both directions', one(c, "select count(*) from links where kind='friend'") == 2)
S.K('links are immutable: kind', 'immutable' in tryx(c, "UPDATE links SET kind='related' WHERE kind='friend'"))
S.K('links are immutable: an endpoint', 'immutable' in tryx(c, 'UPDATE links SET to_id=? WHERE kind=\'visited\'', (pb,)))
S.K('a full-row update that changes only the note passes', tryx(c, "UPDATE links SET note='hi', from_id=from_id, to_id=to_id, kind=kind WHERE kind='visited'") == 'OK')
c.execute("DELETE FROM links WHERE kind='friend' AND from_id=?", (pa,))
S.K('deleting one side of a symmetric edge deletes its mirror', one(c, "select count(*) from links where kind='friend'") == 0)

# ---- the registry
S.K('link_kinds: symmetric is fixed (by the trigger: subtask could be symmetric by its CHECK)', 'fixed at registration' in tryx(c, "UPDATE link_kinds SET symmetric=1 WHERE kind='subtask'"))
S.K('link_kinds: to_types is fixed', 'fixed at registration' in tryx(c, "UPDATE link_kinds SET to_types='person' WHERE kind='about'"))
S.K('link_kinds: a note edit with symmetric=symmetric passes', tryx(c, "UPDATE link_kinds SET note='n', symmetric=symmetric, from_types=from_types WHERE kind='visited'") == 'OK')
S.K('a kind name in upper case is refused', tryx(c, "INSERT INTO link_kinds(kind,symmetric) VALUES ('Boss',0)").startswith('ERR'))
S.K('a symmetric kind with different endpoint types is refused', tryx(c, "INSERT INTO link_kinds(kind,symmetric,from_types,to_types) VALUES ('mentor',1,'person','place')").startswith('ERR'))
S.K('a malformed type list is refused', tryx(c, "INSERT INTO link_kinds(kind,symmetric,from_types,to_types) VALUES ('k2',0,'Person','page')").startswith('ERR'))
S.K('a misspelt type token fails closed: every link of that kind is refused', tryx(c, "INSERT INTO link_kinds(kind,symmetric,from_types,to_types) VALUES ('godparent',0,'persn','person')") == 'OK'
    and link(c, pa, pb, 'godparent').startswith('ERR'))
c2 = fresh(fk=False); x, y = named(c2, 'person'), named(c2, 'person')
S.K('with foreign_keys=OFF an unregistered kind is still refused (the trigger, in autocommit)', 'not registered' in link(c2, x, y, 'nemesis'))
S.K('the mirrors terminate under recursive_triggers=ON', link(c2, x, y, 'family') == 'OK' and one(c2, "select count(*) from links where kind='family'") == 2
    and tryx(c2, "DELETE FROM links WHERE kind='family' AND from_id=?", (x,)) == 'OK' and one(c2, "select count(*) from links") == 0)
link(c2, x, y, 'friend')
S.K('INSERT OR REPLACE on a symmetric link: too many levels of trigger recursion', 'too many levels of trigger recursion' in
    tryx(c2, f"INSERT OR REPLACE INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'friend',{NOW},'ui')", (x, y)))
S.K('ON CONFLICT DO NOTHING is the way', tryx(c2, f"INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'friend',{NOW},'ui') ON CONFLICT(from_id,to_id,kind) DO NOTHING", (x, y)) == 'OK')

# ---- containment (§6.18) and one-way kinds
c = fresh(); japan, kanto, tokyo = named(c, 'place', 'Japan'), named(c, 'place', 'Kanto'), named(c, 'place', 'Tokyo')
S.K('Tokyo located-in Kanto located-in Japan', link(c, tokyo, kanto, 'located-in') == link(c, kanto, japan, 'located-in') == 'OK')
S.K('located-in is one-way (no mirror)', one(c, "select count(*) from links where kind='located-in'") == 2)
kid, par = named(c, 'person'), named(c, 'person')
S.K('parent-of keeps its direction', link(c, par, kid, 'parent-of') == 'OK' and one(c, "select count(*) from links where kind='parent-of'") == 1)
d1 = day_page(c, '2019-04-02', 'landed in [[Tokyo]]'); link(c, d1, tokyo, 'wikilink')
d2 = day_page(c, '2019-04-05', 'a day in [[Japan]] and [[Kanto]]'); link(c, d2, japan, 'wikilink'); link(c, d2, kanto, 'wikilink')
d3 = day_page(c, '2018-12-31', '[[Tokyo]] again'); link(c, d3, tokyo, 'wikilink')
osaka = named(c, 'place', 'Osaka'); d4 = day_page(c, '2019-06-01', 'not linked to Japan: [[Osaka]]'); link(c, d4, osaka, 'wikilink')
pg = page(c, 'Tokyo guide', day='2019-05-01'); link(c, pg, tokyo, 'wikilink')
P = dict(place_id=japan, from_day='2019-01-01', to_day='2019-12-31')
WANT = [('2019-04-02', 'Tokyo'), ('2019-04-05', 'Japan'), ('2019-04-05', 'Kanto')]
S.K('§6.18 finds the 2019 days in Japan by the places inside it, not another year, a place outside or a page that is not a day', c.execute(block('6.18'), P).fetchall() == WANT, c.execute(block('6.18'), P).fetchall())
link(c, japan, tokyo, 'located-in')
steps = [0]
def stop():
    steps[0] += 1; return 1 if steps[0] > 2000 else 0
c.set_progress_handler(stop, 1000)
try: r = c.execute(block('6.18'), P).fetchall()
except sqlite3.Error as e: r = 'ERR ' + str(e)
c.set_progress_handler(None, 0)
S.K('§6.18 terminates on a cycle (UNION)', r == WANT, r)

# ---- subtasks (§6.11) and the cycle cap
c = fresh(); root, a, b = thing(c, 'task'), thing(c, 'task'), thing(c, 'task')
link(c, a, root, 'subtask'); link(c, b, a, 'subtask')
S.K('§6.11 walks the subtree with depths', [r[1] for r in c.execute(block('6.11'), {'task_id': root})] == [0, 1, 2])
link(c, a, b, 'subtask')                                          # a cycle below the root
S.K('§6.11 terminates on a cycle (depth cap)', len(c.execute(block('6.11'), {'task_id': root}).fetchall()) > 3)
uncapped = block('6.11').replace(' AND subtree.depth < 32', '')
steps = [0]
def stop():
    steps[0] += 1; return 1 if steps[0] > 2000 else 0
c.set_progress_handler(stop, 1000)
S.K('without the cap the same walk does not terminate (stopped by a progress handler)', 'interrupted' in tryx(c, uncapped, {'task_id': root}))
c.set_progress_handler(None, 0)
S.done()
