"""Shared helpers for the suites: the DDL and the document under test, fresh databases, the insert conventions of
the docs (entity first, ids by RETURNING, a named entity is one id with a page), and the expectation counter.

A suite reads the DDL from argv[1] or $DDL and the docs from $DOCS (default: ../../docs), so the mutant runner can
point it at a broken copy. It ends with `<name>: X/Y met expectations` and exits 1 unless X == Y."""
import os, re, sqlite3, sys
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE); sys.path.insert(0, os.path.join(HERE, '..', 'wikilinks'))
import docsql

DDL_PATH = sys.argv[1] if len(sys.argv) > 1 and sys.argv[1].endswith('.sql') else os.environ.get('DDL')
DOC = docsql.doc_text()                         # every current-truth page
DDL = open(DDL_PATH, encoding='utf-8').read() if DDL_PATH else docsql.ddl()
LIVE = docsql.doc_text(skip=('research/',))     # the same, without the references
doc_page = docsql.page
NOW = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
NAMED = ('person', 'place')


class Suite:
    def __init__(self, name):
        self.name, self.res = name, []
        sys.excepthook = self._stopped

    def _stopped(self, et, ev, tb):
        """A broken schema may make a later step impossible: that is a failed expectation, reported, not a bare crash."""
        import traceback
        print('  FAIL the suite stopped:', ''.join(traceback.format_exception_only(et, ev)).strip()[:300])
        self.res.append(False); ok, n = sum(self.res), len(self.res)
        print(f'{self.name}: {ok}/{n} met expectations', flush=True); os._exit(1)

    def K(self, label, cond, detail=''):
        """Record one expectation; the label states the expected outcome."""
        self.res.append(bool(cond))
        if not cond:
            print('  FAIL', label, str(detail)[:300])
        return bool(cond)

    def done(self):
        ok, n = sum(self.res), len(self.res)
        print(f'{self.name}: {ok}/{n} met expectations')
        sys.exit(0 if n and ok == n else 1)


def fresh(path=':memory:', hardened=False, fk=True, rt=True, ddl=None):
    """A connection in autocommit mode with the schema applied and the contract/connections pragmas set."""
    c = sqlite3.connect(path, isolation_level=None)
    if hardened:
        c.setconfig(sqlite3.SQLITE_DBCONFIG_DEFENSIVE, True); c.execute('PRAGMA trusted_schema = OFF')
    c.executescript(ddl or DDL)
    c.execute(f'PRAGMA foreign_keys={"ON" if fk else "OFF"}'); c.execute(f'PRAGMA recursive_triggers={"ON" if rt else "OFF"}')
    return c


def tryx(c, sql, args=()):
    """'OK' or 'ERR <message>' — a refused statement is a result, not a crash."""
    try:
        c.execute(sql, args); return 'OK'
    except sqlite3.Error as e:
        return 'ERR ' + str(e)


def one(c, sql, args=()):
    """First column of the first row, or None (also when the statement is refused)."""
    try:
        r = c.execute(sql, args).fetchone()
    except sqlite3.Error:
        return None
    return None if r is None else r[0]


def title_key(t):
    import unicodedata
    return unicodedata.normalize('NFC', unicodedata.normalize('NFC', t).casefold())


def ent(c, typ, source='ui', created=None):
    """The entities row, id by RETURNING."""
    at = created or NOW
    return c.execute(f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES (?,{at},{at},?) RETURNING id", (typ, source)).fetchone()[0]


def day_page(c, day='2026-09-30', body='x', created=None):
    """The journal page of a local day (D5): titled with the day, its day the title."""
    return page(c, day, day=day, body=body, created=created)


def page(c, title, day=None, body='', created=None, key=None):
    i = ent(c, 'page', created=created)
    c.execute("INSERT INTO pages(id,title,title_key,day,body) VALUES (?, ?, ?, ?, ?)",
              (i, title, key if key is not None else title_key(title), day, body)); return i


_n = [0]
def named(c, typ, handle=None, **cols):
    """A person or a place (D20): the entity and its page (entity_type = typ), and a person's people row — one id."""
    if handle is None:
        _n[0] += 1; handle = f'{typ.title()} {_n[0]}'
    i = ent(c, typ)
    c.execute("INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, ?, ?, ?)", (i, typ, handle, title_key(handle)))
    domain(c, typ, i, **cols); return i


def domain(c, typ, i, **cols):
    if typ == 'place': return                      # a place is its page: no row of its own (D16)
    if typ == 'person': cols.setdefault('name', 'P')
    table = dict(person='people')[typ]
    cols = dict(id=i, **cols)
    c.execute(f"INSERT INTO {table}({','.join(cols)}) VALUES ({','.join('?' * len(cols))})", tuple(cols.values()))


def thing(c, typ, **cols):
    """Any entity with its domain row: a page, or a named one."""
    if typ in NAMED: return named(c, typ, **cols)
    if typ == 'page': _n[0] += 1; return page(c, f'Page {_n[0]}', body='x')
    i = ent(c, typ); domain(c, typ, i, **cols); return i


def link(c, f, t, kind, source='ui'):
    return tryx(c, f"INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,?,{NOW},?)", (f, t, kind, source))


def measure(c, metric, day, value, source='ui', **cols):
    cols = dict(metric_id=metric, day=day, value=value, source=source, **cols)
    return tryx(c, f"INSERT INTO measurements({','.join(cols)},created_at) VALUES ({','.join('?' * len(cols))},{NOW})", tuple(cols.values()))


def habit(c, metric, start, end=None, source='ui'):
    """A habit period (D24): 'OK' or 'ERR <message>'."""
    return tryx(c, 'INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES (?,?,?,?)', (metric, start, end, source))


def statements(sql):
    """Split a block into complete statements, dropping comment-only ones."""
    out, acc = [], ''
    for line in sql.splitlines(keepends=True):
        acc += line
        if sqlite3.complete_statement(acc):
            if re.sub(r'--[^\n]*', '', acc).strip(): out.append(acc)
            acc = ''
    return out


def code(st):
    return re.sub(r'--[^\n]*', '', st).strip()


def run_block(c, sql, P, after=None, only=None):
    """Execute a document block statement by statement, as the app would: bind the :params it names, and keep an id a
    statement RETURNs under the :name its comment gives ('RETURNING id;  -- ... :page_id'). Returns each statement's rows."""
    out = []
    for st in statements(sql):
        if only and not only(code(st)): continue
        cur = c.execute(st, {k: P[k] for k in re.findall(r':(\w+)', code(st)) if k in P})
        rows = cur.fetchall() if cur.description else None
        m = re.search(r'RETURNING id;[^\n]*?:(\w+)', st)
        if m and rows: P[m.group(1)] = rows[0][0]
        out.append(rows)
        if after: after(code(st))
    return out


def blocks():
    """{'capture': sql, ...}: the first sql block of each cookbook recipe, by its key."""
    out = {}
    for k, s in docsql.cookbook_blocks():
        out.setdefault(k, s)
    return out


def block(key):
    return blocks().get(key, '')


def sql_blocks(text):
    return re.findall(r'```sql\n(.*?)\n```', text, re.S)


def integrity_ok(c):
    return c.execute('PRAGMA integrity_check').fetchall() == [('ok',)] and c.execute('PRAGMA foreign_key_check').fetchall() == []
