"""Render every mermaid block of SCHEMA.md with mermaid-cli, so a syntax error cannot ship.   python3 render_diagrams.py
Optional (run_all.py --mermaid): needs node with `mmdc` on PATH (or MMDC=/path/to/mmdc) and a Chromium/Chrome
(PUPPETEER_EXECUTABLE_PATH, or `chromium` / `google-chrome` on PATH). Install once:  npm i -g @mermaid-js/mermaid-cli
Nothing is written into the repository: the SVGs go to a temporary directory."""
import json, os, re, shutil, subprocess, sys, tempfile
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, os.path.join(HERE, '..', 'lib'))
import docsql
mmdc = os.environ.get('MMDC') or shutil.which('mmdc')
chrome = os.environ.get('PUPPETEER_EXECUTABLE_PATH') or next((shutil.which(x) for x in ('chromium', 'chromium-browser', 'google-chrome', 'google-chrome-stable') if shutil.which(x)), None)
if not mmdc: sys.exit('render_diagrams: mmdc not found (npm i -g @mermaid-js/mermaid-cli, or set MMDC)')
D = tempfile.mkdtemp(prefix='mermaid-')
cfg = os.path.join(D, 'puppeteer.json')
json.dump({**({'executablePath': chrome} if chrome else {}), 'args': ['--no-sandbox', '--disable-gpu']}, open(cfg, 'w'))
blocks = re.findall(r'```mermaid\n(.*?)\n```', docsql.doc_text(), re.S)
ok = 0
for i, b in enumerate(blocks, 1):
    name = (re.match(r'%% diagram: ([a-z-]+)', b) or [None, f'block{i}'])[1]
    src = os.path.join(D, name + '.mmd'); out = os.path.join(D, name + '.svg'); open(src, 'w', encoding='utf-8').write(b + '\n')
    p = subprocess.run([mmdc, '-i', src, '-o', out, '-p', cfg], capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=180)
    good = p.returncode == 0 and os.path.exists(out) and os.path.getsize(out) > 500 and 'Syntax error' not in open(out, encoding='utf-8', errors='ignore').read()
    ok += good
    print(('PASS ' if good else 'FAIL ') + name + ('' if good else '  ' + (p.stderr or p.stdout).strip()[-300:]), flush=True)
shutil.rmtree(D, ignore_errors=True)
print(f'{ok}/{len(blocks)} diagrams rendered')
sys.exit(0 if ok == len(blocks) and blocks else 1)
