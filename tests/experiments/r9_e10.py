"""E10 Does VACUUM INTO hide damage in the LIVE file? Declared: (i) a damaged index entry is rebuilt, so the copy passes integrity_check while the source fails;
(ii) a zeroed table page makes VACUUM INTO fail (or the copy fail); (iii) a flipped content byte is copied silently. Also: time of integrity_check / quick_check on the 50 MB file."""
import subprocess, shutil, os, time
D = '/home/alex/.cache/lifelog-r9/e10'; shutil.rmtree(D, ignore_errors=True); os.makedirs(D)
def sq(db, sql, *flags):
    r = subprocess.run(['sqlite3', '-readonly', *flags, db, sql], capture_output=True, stdin=subprocess.DEVNULL); return r.stdout.decode().strip(), r.stderr.decode().strip(), r.returncode
base = f'{D}/clean.db'; shutil.copy('/home/alex/.cache/lifelog-r9/y5.db', base)
data = open(base, 'rb').read(); psz = int(sq(base, 'pragma page_size')[0])
def variant(name, mutate):
    p = f'{D}/{name}.db'; open(p, 'wb').write(mutate(bytearray(data))); return p
def flip(at):
    def f(b): b[at] ^= 0x01; return bytes(b)
    return f
j = data.find(b'topic 12'); i = data.find(b'Topic 12')
cases = {'index-key flip': variant('idx', flip(j + 7)), 'content flip': variant('txt', flip(i + 7)),
         'zeroed page 40': variant('zero', lambda b: bytes(b[:39 * psz]) + b'\x00' * psz + bytes(b[40 * psz:]))}
for name, p in cases.items():
    live_ic = sq(p, 'pragma integrity_check')[0].replace('\n', ' | ')[:70]
    out, err, rc = sq(p, f"VACUUM INTO '{D}/{name.replace(' ', '_')}.snap'")
    if rc == 0:
        snap_ic = sq(f"{D}/{name.replace(' ', '_')}.snap", 'pragma integrity_check')[0][:70]
        print(f'{name:16s} live integrity_check: {live_ic!r:75s} VACUUM INTO rc=0, COPY integrity_check: {snap_ic!r}')
    else:
        print(f'{name:16s} live integrity_check: {live_ic!r:75s} VACUUM INTO FAILED rc={rc}: {err[:70]}')
for chk in ('integrity_check', 'quick_check'):
    t = time.time(); sq(base, f'pragma {chk}'); print(f'{chk} on the clean 50 MB file: {time.time()-t:.2f}s')
