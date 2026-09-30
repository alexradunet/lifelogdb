import os, sys; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from wikisave import targets
from vectors import V
bad = 0
for label, body, exp in V:
    ok, rej = targets(body)
    got = list(ok.values())
    flag = 'PASS' if got == exp else 'FAIL'
    if got != exp:
        bad += 1; print(f'{flag} {label}: got {got!r} expected {exp!r}  body={body!r}')
print(f'{len(V)-bad}/{len(V)} vectors')
