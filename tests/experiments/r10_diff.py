"""Old (v1.8 DDL) vs new (R8-01) title CHECK on the same corpus. Declared: the ONLY strings whose verdict changes are those whose part before the first '.'
(ASCII-uppercased) is a device name, incl. the six superscript names; each of those flips accepted -> rejected; nothing flips the other way."""
import sqlite3, random, sys, os, unicodedata
sys.path.insert(0, '/home/alex/Work/lifelog/tests/wikilinks')
from wikisave import title_key, NOW, DEVICES, _ascii_upper
def conn(ddl):
    c = sqlite3.connect(':memory:', isolation_level=None); c.execute('PRAGMA foreign_keys=ON'); c.executescript(open(ddl).read()); return c
old, new = conn('ddl_before.sql'), conn('ddl_v110.sql')
src = open('/home/alex/Work/lifelog/tests/wikilinks/title_fuzz.py').read()
ns = {'random': random}; exec(src[src.index('ALPHA ='):src.index('rng = random.Random(8)')], ns)
ALPHA, WORDS, rnd = ns['ALPHA'], ns['WORDS'], ns['rnd']
rng = random.Random(10); cases = set(WORDS) | {'a' * 240, '日' * 80, 'CON.backup', 'COM¹', 'LPT³.x', 'con.txt', 'Prn.a.b', 'Aux.', 'NUL.txt.md'}
cases |= {rnd(rng) for _ in range(60000)}
def accepts(c, t):
    k = title_key(t) if t else ''
    c.execute('SAVEPOINT x')
    try:
        c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES('page',{NOW},{NOW})"); i = c.execute('select last_insert_rowid()').fetchone()[0]
        c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES(?, 'wiki', ?, ?)", (i, t, k)); r = True
    except sqlite3.Error: r = False
    c.execute('ROLLBACK TO x'); c.execute('RELEASE x'); return r
flipped, back, changed_unexpected = [], [], []
for t in cases:
    a, b = accepts(old, t), accepts(new, t)
    if a and not b: flipped.append(t)
    if b and not a: back.append(t)
    if a != b and _ascii_upper(t.split('.', 1)[0]) not in DEVICES: changed_unexpected.append(t)
# every old-accepted string whose base is a device must now be rejected
must = [t for t in cases if accepts(old, t) and _ascii_upper(t.split('.', 1)[0]) in DEVICES]
print(f'{len(cases)} strings. accepted by old & rejected by new: {len(flipped)}; rejected by old & accepted by new: {len(back)}; changed outside the intended class: {len(changed_unexpected)}')
print('intended class (old accepted, base is a device name):', len(must), '-> all now rejected:', all(t in set(flipped) for t in must), '| sample:', sorted(flipped, key=len)[:8])
