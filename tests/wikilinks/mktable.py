import os, sys; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from vectors import V
LABELS = ['plain','alias','trim','hash-in-title','slash','colon','empty','dup by key','wikilink NFD','no escape',
          'inline code','fenced code ~','markdown link','markup inside','ref definition','devices',
          'tag','tag case','tag punctuation','tag NFD','heading is not a tag','hash without space','C# F# a#b','url fragments',
          'numbers','tag beside wikilink','device tags','redirect stub','redirect not first']
by = {l: (b, e) for l, b, e in V}
def enc(body):
    s = body.replace('\\', '\\\\') if False else body
    s = s.replace('\n', '\\n').replace('\u0301', '\\u0301').replace('\u0308', '\\u0308')
    s = s.replace('|', '\\|')
    return ('`` ' + s + ' ``') if '`' in s else ('`' + s + '`')
def cell(e): return ', '.join('`' + t + '`' for t in e) if e else '—'
rows = ['| body | links to (`title`s, in order) |', '|---|---|']
for l in LABELS:
    b, e = by[l]; rows.append(f'| {enc(b)} | {cell(e)} |')
print('\n'.join(rows))
