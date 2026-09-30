"""Reference implementation of the wikilink/tag save contract (SCHEMA.md §2.4, §6.13, D19) — a test instrument, not the application.
Parameters `mutate=` switch single rules off so the probes can be shown to fail (mutation checks)."""
import re, json, sqlite3, unicodedata
from markdown_it import MarkdownIt

_md = MarkdownIt('commonmark')
WIKI = re.compile(r'\[\[([^\[\]\n]*)\]\]')
TAG = re.compile(r'(?<![\w/&#])#(\w+(?:-\w+)*)')   # only used by the 'ascii_word' mutant

def _tagchar(ch, mutate=()):
    if 'ascii_word' in mutate: return bool(re.match(r'\w', ch))
    return unicodedata.category(ch)[0] in 'LMN' or ch == '_'

def scan_tags(run, mutate=()):
    """[(pos, tag)]: '#' + words of letters/marks/digits/_ joined by single '-', not glued to a preceding tag char, '/' or '#'."""
    out, i, n = [], 0, len(run)
    while i < n:
        if run[i] == '#' and (i == 0 or 'tag_no_lookbehind' in mutate or not (_tagchar(run[i-1], mutate) or run[i-1] in '/#')):
            j = i + 1
            while True:
                k = j
                while k < n and _tagchar(run[k], mutate): k += 1
                j = k
                if j + 1 < n and run[j] == '-' and _tagchar(run[j+1], mutate) and k > i + 1: j += 1; continue
                break
            if j > i + 1: out.append((i, run[i+1:j]))
            i = max(j, i + 1)
        else:
            i += 1
    return out
STUB = re.compile(r'\A\s*#redirect\s+\[\[', re.I)
DEVICES = {'CON','PRN','AUX','NUL'} | {f'COM{i}' for i in range(1,10)} | {f'LPT{i}' for i in range(1,10)} | {'COM¹','COM²','COM³','LPT¹','LPT²','LPT³'}
NOW = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
INVISIBLE = {0xAD, 0x61C, 0x200B, 0x200E, 0x200F, 0xFEFF} | set(range(0x202A, 0x202F)) | set(range(0x2060, 0x2065)) | set(range(0x2066, 0x206A))

def title_key(t):
    return unicodedata.normalize('NFC', unicodedata.normalize('NFC', t).casefold())

def _ascii_upper(s):
    return ''.join(c.upper() if c.isascii() else c for c in s)

def title_ok(t, mutate=()):
    """The §3 filename CHECKs, as a predicate."""
    if not t or t != t.strip(' '): return False
    if len(t.encode('utf-8')) > 240: return False
    if any(ch in '/\\:*?"<>|' for ch in t): return False
    if any(ord(ch) <= 31 or 127 <= ord(ch) <= 159 for ch in t): return False   # C0 (incl. NUL), DEL, C1
    if any(ord(ch) in INVISIBLE for ch in t): return False                 # invisible and bidi characters (§2.5)
    if 'allow_unassigned' not in mutate and any(unicodedata.category(ch) == 'Cn' for ch in t): return False   # app only: a later Unicode may give it a fold
    if t[0] == '.' or t[-1] == '.': return False
    base = t if 'device_bare_only' in mutate else t.split('.', 1)[0]      # CON.backup is the device too [R58]
    if _ascii_upper(base) in DEVICES: return False
    return True

def text_runs(body, mutate=()):
    if 'no_parser' in mutate:               # mutant: scan the raw body (code spans, fences, URLs included)
        yield unicodedata.normalize('NFC', body); return
    for tok in _md.parse(body):
        if tok.type == 'inline':
            for c in tok.children:
                if c.type == 'text':
                    yield c.content if 'no_nfc' in mutate else unicodedata.normalize('NFC', c.content)

def candidates(body, mutate=()):
    """Titles in order of appearance (valid or not, not de-duplicated)."""
    if 'no_stub_rule' not in mutate and STUB.match(body):
        return []
    out = []
    for run in text_runs(body, mutate):
        found = []
        for m in WIKI.finditer(run):
            found.append((m.start(), m.group(1) if 'alias_kept' in mutate else m.group(1).split('|', 1)[0].strip(' ')))
        blanked = run if 'tags_inside_wikilinks' in mutate else WIKI.sub(lambda m: ' ' * len(m.group(0)), run)
        for pos, g in scan_tags(blanked, mutate):
            if all(unicodedata.category(ch) == 'Nd' for ch in g) and 'numeric_tags' not in mutate: continue
            if g.lower() == 'redirect' and 'no_stub_rule' not in mutate: continue
            found.append((pos, g))
        out += [t for _, t in sorted(found)]
    return out

def targets(body, own_key=None, mutate=()):
    """(valid {key: title in first-seen spelling}, rejected [title])"""
    ok, bad = {}, []
    for t in candidates(body, mutate):
        if 'no_validation' not in mutate and not title_ok(t, mutate):
            bad.append(t); continue
        k = title_key(t)
        if k == own_key and 'self_links' not in mutate: continue
        ok.setdefault(k, t)
    return ok, bad

def sync_wikilinks(c, page_id, body, mutate=()):
    """Runs INSIDE the caller's BEGIN IMMEDIATE transaction, after the body is saved. Never raises for a bad target."""
    own = c.execute('SELECT title_key FROM pages WHERE id=?', (page_id,)).fetchone()[0]
    ok, bad = targets(body, own, mutate)
    ids, skipped = [], list(bad)
    for key, title in ok.items():
        if 'no_savepoint' not in mutate: c.execute('SAVEPOINT target')
        try:
            row = c.execute("SELECT p.id, e.deleted_at FROM pages p JOIN entities e ON e.id=p.id "
                            "WHERE p.title_key=?", (key,)).fetchone()
            if row is None:
                tid = c.execute(f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES('page',{NOW},{NOW},'ui') RETURNING id").fetchone()[0]
                c.execute("INSERT INTO pages(id,kind,title,title_key) VALUES(?, 'page', ?, ?)", (tid, title, key))
            else:
                tid = row[0]
                if row[1] is not None and 'no_revive' not in mutate: c.execute('UPDATE entities SET deleted_at=NULL WHERE id=?', (tid,))
            c.execute(f"INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?, 'wikilink', {NOW}, 'ui') "
                      "ON CONFLICT(from_id,to_id,kind) DO NOTHING", (page_id, tid))
            if 'no_savepoint' not in mutate: c.execute('RELEASE target')
        except sqlite3.Error:
            if 'no_savepoint' in mutate: raise
            c.execute('ROLLBACK TO target'); c.execute('RELEASE target'); skipped.append(title); continue
        ids.append(tid)
    if 'no_delete_sync' not in mutate:
        c.execute("DELETE FROM links WHERE from_id=? AND kind='wikilink' AND to_id NOT IN (SELECT value FROM json_each(?))",
                  (page_id, json.dumps(ids)))
    return ids, skipped

def save_memo(c, body, day='2026-09-30', mutate=()):
    c.execute('BEGIN IMMEDIATE')
    try:
        pid = c.execute(f"INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES('page',{NOW},{NOW},'ui') RETURNING id").fetchone()[0]
        c.execute("INSERT INTO pages(id,kind,day,body) VALUES(?, 'memo', ?, ?)", (pid, day, body))
        r = sync_wikilinks(c, pid, body, mutate)
        c.execute('COMMIT')
    except BaseException:
        c.execute('ROLLBACK'); raise
    return pid, r

def edit_body(c, page_id, body, mutate=()):
    c.execute('BEGIN IMMEDIATE')
    try:
        c.execute('UPDATE pages SET body=? WHERE id=?', (body, page_id))
        r = sync_wikilinks(c, page_id, body, mutate)
        c.execute('COMMIT')
    except BaseException:
        c.execute('ROLLBACK'); raise
    return r
