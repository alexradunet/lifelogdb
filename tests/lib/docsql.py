"""Pull the executable parts out of SCHEMA.md: §3 DDL, §6 cookbook blocks, any section. Used by every suite.
CLI:  python3 docsql.py ddl OUTFILE"""
import os, re, sys

DOC = os.environ.get('DOC') or os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', 'SCHEMA.md')

def doc_text(path=None):
    return open(path or DOC, encoding='utf-8').read()

def section(text, start_pat, end_pat):
    s = re.search(start_pat, text, re.M)
    e = re.search(end_pat, text[s.end():], re.M)
    return text[s.end(): s.end() + e.start()] if e else text[s.end():]

def ddl(text=None):
    """§3: the first ```sql block after the '## 3.' heading."""
    text = text or doc_text()
    sec = section(text, r"^## 3\. ", r"^## 4\. ")
    return re.search(r"```sql\n(.*?)\n```", sec, re.S).group(1) + "\n"

def cookbook_blocks(text=None):
    """[(heading, sql)] for every ```sql block in §6."""
    text = text or doc_text()
    sec = section(text, r"^## 6\. ", r"^## 7\. ")
    out, head = [], None
    for part in re.split(r"(^### .*$)", sec, flags=re.M):
        if part.startswith("### "):
            head = part[4:].strip()
        else:
            for m in re.finditer(r"```sql\n(.*?)\n```", part, re.S):
                out.append((head, m.group(1)))
    return out

if __name__ == "__main__":
    if sys.argv[1:2] == ["ddl"] and len(sys.argv) == 3:
        open(sys.argv[2], "w", encoding="utf-8").write(ddl())
    else:
        sys.exit("usage: docsql.py ddl OUTFILE")
