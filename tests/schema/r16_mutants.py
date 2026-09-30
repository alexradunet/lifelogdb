"""Break the round-16 rules (D20, named entities are pages) on purpose and require r16probes.py to notice.   python3 r16_mutants.py  (tests venv)
Each mutant is a copy of SCHEMA.md with one rule taken out or put back the old way; the DDL and the text the probes read are
taken from that copy. A probe suite that cannot fail proves nothing."""
import os, re, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
doc = open(docsql.DOC, encoding='utf-8').read()

def mutate(old, new):
    n = doc.count(old)
    assert n == 1, f'mutation target found {n}x: {old[:70]!r}'
    return doc.replace(old, new, 1)

TAIL = "  UNIQUE (id, type),\n  CONSTRAINT entities_page_iff_named CHECK ((type IN ('person','place','holding')) = (page_id IS NOT NULL))\n"
LEG3 = (" WHERE l.from_id = :entity_id AND e.deleted_at IS NULL\nUNION ALL\nSELECT l.kind, e.type, l.from_id, 'in via page'       -- [[wikilinks]] to the entity's page\n"
        "  FROM entities me\n  JOIN links l    ON l.to_id = me.page_id AND l.kind = 'wikilink'\n  JOIN entities e ON e.id = l.from_id\n WHERE me.id = :entity_id AND e.deleted_at IS NULL;")
MUTANTS = [
 ('the CHECK that ties a named type to a page is gone',
  mutate(TAIL, "  UNIQUE (id, type)\n")),
 ('a named type may have no page (only the other direction is checked)',
  mutate("CHECK ((type IN ('person','place','holding')) = (page_id IS NOT NULL))", "CHECK (page_id IS NULL OR type IN ('person','place','holding'))")),
 ('an event may have a page (the CHECK only needs a page for named types)',
  mutate("CHECK ((type IN ('person','place','holding')) = (page_id IS NOT NULL))", "CHECK (type NOT IN ('person','place','holding') OR page_id IS NOT NULL)")),
 ('one page can be the page of two entities (page_id is not unique)',
  mutate("page_id    INTEGER UNIQUE REFERENCES pages(id),", "page_id    INTEGER REFERENCES pages(id),")),
 ('page_id points at nothing (no foreign key)',
  mutate("page_id    INTEGER UNIQUE REFERENCES pages(id),", "page_id    INTEGER UNIQUE,")),
 ('the page of an entity can be changed (no fixed trigger)',
  mutate("CREATE TRIGGER entities_page_fixed BEFORE UPDATE OF page_id ON entities\n  WHEN NEW.page_id IS NOT OLD.page_id", "CREATE TRIGGER entities_page_fixed BEFORE UPDATE OF page_id ON entities\n  WHEN 0")),
 ('a memo can be the page of a place',
  mutate("CREATE TRIGGER entities_page_is_a_page BEFORE INSERT ON entities\n  WHEN NEW.page_id IS NOT NULL", "CREATE TRIGGER entities_page_is_a_page BEFORE INSERT ON entities\n  WHEN 0")),
 ('ghost_pages lists the page of a person',
  mutate("     AND NOT EXISTS (SELECT 1 FROM entities n WHERE n.page_id = p.id)\n", "")),
 ('6.6 loses the leg that follows wikilinks into the page',
  mutate(LEG3, " WHERE l.from_id = :entity_id AND e.deleted_at IS NULL;")),
 ('6.20 lists wiki pages as memos',
  mutate("  JOIN pages m    ON m.id = l.from_id AND m.kind = 'memo'\n", "  JOIN pages m    ON m.id = l.from_id\n")),
 ('6.20 lists tombstoned memos',
  mutate("  JOIN entities em ON em.id = m.id AND em.deleted_at IS NULL\n", "  JOIN entities em ON em.id = m.id\n")),
 ('people have notes again',
  mutate("  death_day   TEXT CONSTRAINT people_death_day CHECK (death_day IS NULL OR date(death_day) IS death_day),\n  FOREIGN KEY",
         "  death_day   TEXT CONSTRAINT people_death_day CHECK (death_day IS NULL OR date(death_day) IS death_day),\n  notes       TEXT,\n  FOREIGN KEY")),
 ('the named_pages key is gone from lifelog_meta',
  mutate("  ('named_pages', 'every person", "  ('named_pg',    'every person")),
 ('a mention link kind is registered (the old way to reach a person)',
  mutate("  ('related',  1, NULL,        NULL,           'anything ↔ anything');", "  ('mention', 0, 'page', 'person', 'x'),\n  ('related',  1, NULL,        NULL,           'anything ↔ anything');")),
 ('6.13 no longer says the named pages are not ghosts',
  mutate("AND NOT EXISTS (SELECT 1 FROM entities n WHERE n.page_id = p.id);   -- a person's, place's or holding's page is not a ghost (D20)", ";")),
 ('D20 no longer says what it costs',
  mutate("- **Costs accepted.** Every person, place and holding needs a unique handle", "- **Cost.** Every person, place and holding needs a unique handle")),
]

def run(text):
    d = tempfile.mkdtemp(prefix='r16-mut-'); dp = os.path.join(d, 'SCHEMA.md'); ddl = os.path.join(d, 'ddl.sql')
    open(dp, 'w', encoding='utf-8').write(text); open(ddl, 'w', encoding='utf-8').write(docsql.ddl(text))
    p = subprocess.run([sys.executable, '-W', 'ignore', 'r16probes.py', ddl], cwd=HERE, env=dict(os.environ, DOC=dp, DDL=ddl), capture_output=True, text=True, stdin=subprocess.DEVNULL)
    out = p.stdout + p.stderr; m = re.search(r'round-16 probes: (\d+)/(\d+)', out)
    return (int(m.group(1)), int(m.group(2))) if m and p.returncode == 0 else (0, -1), out

(ok, n), out = run(doc)
assert ok == n > 0, f'the unmutated document must pass its own probes first: {ok}/{n}\n{out[-500:]}'
caught = 0
for name, text in MUTANTS:
    (ok, n), out = run(text)
    noticed = n != -1 and ok != n          # a crash is not 'noticed': the probes must fail cleanly
    caught += noticed
    print(f"  {'caught ' if noticed else 'MISSED '} {name:<72} {'crash' if n == -1 else f'{n - ok} of {n} probes fail'}")
    if n == -1: print('      ', out.strip().splitlines()[-1][:160])
print(f'{caught}/{len(MUTANTS)} broken documents were noticed')
sys.exit(0 if caught == len(MUTANTS) else 1)
