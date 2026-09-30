"""SCHEMA.md states the current truth and nothing else.   python3 nohistory.py
The document has no review rounds, validation records, addenda, superseded statements, finding ids, version narrative or changelog:
a decision is rewritten when it changes, and git is the log. "Executed" claims are proven by the suites in tests/, not by a record
inside the document. This suite checks the document for the marks of history, then breaks a copy in seven ways and requires each
break to be noticed (a check that cannot fail proves nothing)."""
import os, re, sys
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
doc = docsql.doc_text()

FORBIDDEN = [
 ('a pointer into a removed section (§8, §9)', r'§[89]\b'), ('an addendum', r'[Aa]ddend'), ('a numbered round', r'\b[Rr]ound[ -]\d'),
 ('a finding id (R4-12, R8-01)', r'\bR\d+-\d+\b'), ('a validation record', r'[Vv]alidation record|record #\d|records #\d'),
 ('a superseded statement', r'[Ss]uperseded'), ('a version narrative (v1.14)', r'\bv1\.\d+'), ('a changelog', r'Document history'),
 ('a review narrative', r'cross-review|independent review|[Rr]eview round|[Rr]eview resolutions'), ('a withdrawal notice', r'[Ww]ithdrawn'),
 ('a "first draft" story', r'\b[Ff]irst draft\b'), ('a "(round N)" or "(Round N)" tag', r'\((?:[Rr]ound[ -]\d|review round)'),
]
def problems(text):
    out = [f'{name}: {m.group(0)!r}' for name, pat in FORBIDDEN for m in [re.search(pat, text)] if m]
    heads = re.findall(r'^## (\d+)\. (.+)$', text, re.M)
    if [int(n) for n, _ in heads] != list(range(1, 9)) or heads[-1][1] != 'References': out.append(f'sections are not 1..8 ending in References: {heads}')
    toc = re.findall(r'^\d+\. \[(.+?)\]\(#(.+?)\)$', text, re.M)
    if len(toc) != 8: out.append(f'the table of contents has {len(toc)} entries, not 8')
    ds = [int(x) for x in re.findall(r'^### D(\d+) — ', text, re.M)]
    if ds != list(range(1, 20)): out.append(f'decisions are not D1..D19 in order: {ds}')
    if re.search(r'^### D\d+ — .*\*\(', text, re.M): out.append('a decision title carries a parenthetical status')
    if not re.search(r'^\*\*Status:\*\* frozen pending external review\. No canonical database exists yet', text, re.M): out.append('the status line is not the one-sentence current status')
    return out

res = []
def K(label, cond, detail=''):
    res.append(bool(cond))
    if not cond: print('  FAIL', label, detail)
p = problems(doc)
K('the document has none of the marks of history', not p, p)
for name, _ in FORBIDDEN: K(f'no {name}', not any(x.startswith(name) for x in p))
K('the document ends with the prior-art appendix, not a changelog', re.search(r'\n## Appendix A[^\n]*\n(?:(?!\n## ).)*\Z', doc, re.S) is not None and not doc.rstrip().endswith('*'))
K('§8 is References and §7 is the non-goals table (the two sections the old §8/§9 numbering shifted)', re.search(r'^## 7\. Explicit non-goals', doc, re.M) and re.search(r'^## 8\. References', doc, re.M))

def mutant(name, text):
    q = problems(text); K(f'a broken copy is noticed: {name}', bool(q) and q != p, q[:1])
mutant('an addendum back in D3', doc.replace('- **Sources.** [R4][R26][R27].', '- **Sources.** [R4][R26][R27].\n\n- **Addendum (round 7, registries).** x', 1))
mutant('a pointer into §8', doc.replace('(executed on 0.65.5)', '(executed on 0.65.5, §8 #9)', 1))
mutant('a finding id', doc.replace('never reaches the `people` row (§7).', 'never reaches the `people` row (R4-11 e, still open).', 1))
mutant('a review section back', doc.replace('## 8. References', '## 8. Validation records and review resolutions\n\n## 9. References', 1))
mutant('a version in the status line', doc.replace('**Status:** frozen', '**Status:** v1.14 — frozen', 1))
mutant('a superseded note', doc.replace('### D4 — Text ownership: the database is canonical.', '### D4 — Text ownership: the database is canonical. *(Superseded by D5.)*', 1))
mutant('a decision deleted', doc.replace('### D7 — ', '### D7x — ', 1).replace('### D7x — ', '### DX — ', 1))
print(f'history checks: {sum(res)}/{len(res)} met expectations')
