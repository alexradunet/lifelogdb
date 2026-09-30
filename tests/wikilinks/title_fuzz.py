"""Differential: wikisave.title_ok  vs  the DDL's own CHECKs on the same strings.
Expectation (declared first): zero disagreements in the direction 'app accepts, DB rejects' (that would
block saves); and, because title_ok mirrors the CHECKs one-for-one, zero in the other direction as well,
EXCEPT where I know they differ: unassigned code points (category Cn), which only the app can recognise —
the alphabet below holds none, and r15probes.py tests that difference on its own."""
import os, sqlite3, random, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from wikisave import title_ok, title_key, NOW
c = sqlite3.connect(':memory:', isolation_level=None)
c.execute('PRAGMA foreign_keys=ON'); c.executescript(open(os.environ['DDL']).read())
ALPHA = list('abcXYZ019 .-_#') + list('/\\:*?"<>|') + [chr(1), chr(9), chr(10), chr(31), chr(127), ' ', '　', ' ', 'é', 'É', 'ß', '日', '😀', 'İ', 'ſ', 'K', '́', '​',
                                                                                                                 '‮', '­', '﻿', '\x85', '\x9f', ' ', '‌', '‍', '⁦', '؜', '⁠', '️']
WORDS = ['COM¹','LPT³','LPT².txt','con.backup','Nul.tar.gz','COM1.x','CONX.txt','a.CON','a.con.b','.con','CON.','COM10.txt','CON .txt','CON\u00a0.txt','CON','con','Nul','PRN','aux','COM1','com9','LPT1','lpt9','COM0','LPT10','CONSOLE','NUL.txt','CON.backup','.','..','...','a.','.a','a b','a  b',' a','a ','a ',' a']
def rnd(rng):
    r = rng.random()
    if r < .15: return rng.choice(WORDS)
    if r < .30: return ''.join(rng.choice(ALPHA) for _ in range(rng.randint(1, 6)))
    if r < .40:  # length boundary, mixed byte widths
        n = rng.randint(236, 244); s = ''
        while len(s.encode()) < n:
            s += rng.choice(['a', 'é', '日', '😀'])
        return s
    return ''.join(rng.choice(ALPHA) for _ in range(rng.randint(1, 12)))
rng = random.Random(8)
cases = list(WORDS) + ['a' * 240, 'a' * 241, '日' * 80, '日' * 81, '😀' * 60, '😀' * 61, 'é' * 120, 'é' * 121]
cases += [rnd(rng) for _ in range(60000)]
seen, dis_block, dis_loose, n_ok = set(), [], [], 0
for t in cases:
    if t in seen: continue
    seen.add(t)
    app = title_ok(t)
    k = title_key(t) if t else ''
    c.execute('SAVEPOINT f')
    try:
        c.execute(f"INSERT INTO entities(type,created_at,updated_at) VALUES('page',{NOW},{NOW})")
        pid = c.execute('select last_insert_rowid()').fetchone()[0]
        c.execute("INSERT INTO pages(id,kind,title,title_key,day) VALUES(?, 'page', ?, ?, NULL)", (pid, t, k)); db = True
    except sqlite3.Error as e:
        db = False; why = str(e)[:40]
    c.execute('ROLLBACK TO f'); c.execute('RELEASE f')
    n_ok += db
    if app and not db: dis_block.append((t, why))
    if db and not app: dis_loose.append(t)
print(f'{len(seen)} distinct strings; DB accepts {n_ok}')
print('app accepts but DB rejects (would block a save):', len(dis_block), [ (repr(t[:30]), w) for t, w in dis_block[:5]])
print('DB accepts but app rejects (stricter app):', len(dis_loose), [repr(t[:30]) for t in dis_loose[:8]])
