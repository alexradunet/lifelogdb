"""The graph (SCHEMA.md D8, D16): the closed link-kind registry, endpoint types, symmetric mirrors, immutability,
containment (§6.11) with its cycle guard, and the INSERT OR REPLACE trap."""
import os, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import *
import sqlite3
S = Suite('links')

c = fresh()
pa, pb = named(c, 'person'), named(c, 'person'); pl = named(c, 'place')
m1 = day_page(c); pw = page(c, 'Wiki')
for lbl, exp, f, t, k in [
    ('friend person-person', 'OK', pa, pb, 'friend'), ('an unregistered kind (Friend)', 'ERR', pa, pb, 'Friend'), ('lives-in is not a kind', 'ERR', pa, pl, 'lives-in'),
    ('attended is not a kind (no events, D22)', 'ERR', pa, pw, 'attended'), ('is-a is not a kind (no events, D22)', 'ERR', pw, pw, 'is-a'), ('friend person->place', 'ERR', pa, pl, 'friend'),
    ('subtask is not a kind (D23)', 'ERR', m1, pw, 'subtask'), ('spawned is not a kind (D23)', 'ERR', pw, m1, 'spawned'),
    ('wikilink day page->page', 'OK', m1, pw, 'wikilink'), ('wikilink day page->person (a person is a page)', 'OK', m1, pa, 'wikilink'),
    ('wikilink day page->place', 'OK', m1, pl, 'wikilink'), ('wikilink person page->page', 'OK', pa, pw, 'wikilink'),
    ('redirect page->page', 'OK', m1, pw, 'redirect'),
    ('redirect page->person', 'ERR', pw, pa, 'redirect'), ('about day page->person', 'OK', m1, pb, 'about'), ('about person->place', 'OK', pa, pl, 'about'),
    ('about day page->page', 'ERR', m1, pw, 'about'), ('related page-person', 'OK', pw, pa, 'related'),
    ('at day page->place', 'OK', m1, pl, 'at'), ('at person->place (at comes from a page)', 'ERR', pa, pl, 'at'), ('at page->person', 'ERR', pw, pa, 'at'),
    ('visited is not a kind (D16)', 'ERR', pa, pl, 'visited'), ('a dangling endpoint of a typed kind', 'ERR', 9999, pl, 'at'),
    ('located-in place->person', 'ERR', pl, pa, 'located-in'), ('parent-of person->place', 'ERR', pa, pl, 'parent-of')]:
    r = link(c, f, t, k); S.K(f'link {lbl}: {exp}', r.startswith(exp), r)
S.K('a duplicate edge is refused (UNIQUE from, to, kind)', link(c, m1, pl, 'at').startswith('ERR'))
S.K('a symmetric kind is stored in both directions', one(c, "select count(*) from links where kind='friend'") == 2)
S.K('links are immutable: kind', 'immutable' in tryx(c, "UPDATE links SET kind='related' WHERE kind='friend'"))
S.K('links are immutable: an endpoint', 'immutable' in tryx(c, 'UPDATE links SET to_id=? WHERE kind=\'at\'', (pb,)))
S.K('a full-row update that changes only the note passes', tryx(c, "UPDATE links SET note='hi', from_id=from_id, to_id=to_id, kind=kind WHERE kind='at'") == 'OK')
c.execute("DELETE FROM links WHERE kind='friend' AND from_id=?", (pa,))
S.K('deleting one side of a symmetric edge deletes its mirror', one(c, "select count(*) from links where kind='friend'") == 0)

# ---- the registry
S.K('link_kinds: symmetric is fixed (by the trigger: located-in could be symmetric by its CHECK)', 'fixed at registration' in tryx(c, "UPDATE link_kinds SET symmetric=1 WHERE kind='located-in'"))
S.K('link_kinds: to_types is fixed', 'fixed at registration' in tryx(c, "UPDATE link_kinds SET to_types='person' WHERE kind='about'"))
S.K('link_kinds: a note edit with symmetric=symmetric passes', tryx(c, "UPDATE link_kinds SET note='n', symmetric=symmetric, from_types=from_types WHERE kind='at'") == 'OK')
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

# ---- containment (§6.11) and one-way kinds
c = fresh(); japan, kanto, tokyo = named(c, 'place', 'Japan'), named(c, 'place', 'Kanto'), named(c, 'place', 'Tokyo')
S.K('Tokyo located-in Kanto located-in Japan', link(c, tokyo, kanto, 'located-in') == link(c, kanto, japan, 'located-in') == 'OK')
S.K('located-in is one-way (no mirror)', one(c, "select count(*) from links where kind='located-in'") == 2)
kid, par = named(c, 'person'), named(c, 'person')
S.K('parent-of keeps its direction', link(c, par, kid, 'parent-of') == 'OK' and one(c, "select count(*) from links where kind='parent-of'") == 1)
d1 = day_page(c, '2019-04-02', 'landed in Tokyo'); link(c, d1, tokyo, 'at')
d2 = day_page(c, '2019-04-05', 'a day in Japan and Kanto'); link(c, d2, japan, 'at'); link(c, d2, kanto, 'at')
d3 = day_page(c, '2018-12-31', 'Tokyo again'); link(c, d3, tokyo, 'at')
osaka = named(c, 'place', 'Osaka'); d4 = day_page(c, '2019-06-01', 'not linked to Japan: Osaka'); link(c, d4, osaka, 'at')
d5 = day_page(c, '2019-07-01', 'planning [[Tokyo]]'); link(c, d5, tokyo, 'wikilink')
P = dict(place_id=japan, from_day='2019-01-01', to_day='2019-12-31')
WANT = [('2019-04-02', 'Tokyo'), ('2019-04-05', 'Japan'), ('2019-04-05', 'Kanto')]
S.K('§6.11 finds the 2019 days at a place in Japan, not another year, a place outside or a day that only names it', c.execute(block('6.11'), P).fetchall() == WANT, c.execute(block('6.11'), P).fetchall())
link(c, japan, tokyo, 'located-in')
steps = [0]
def stop():
    steps[0] += 1; return 1 if steps[0] > 2000 else 0
c.set_progress_handler(stop, 1000)
try: r = c.execute(block('6.11'), P).fetchall()
except sqlite3.Error as e: r = 'ERR ' + str(e)
c.set_progress_handler(None, 0)
S.K('§6.11 terminates on a cycle (UNION)', r == WANT, r)

S.done()
