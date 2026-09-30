"""Continuous writer process: each transaction = entity + page + measurement tagged 'live' (so a torn copy shows as a mismatch)."""
import sqlite3, sys, time, os
path, ckpt = sys.argv[1], int(sys.argv[2]); gap = float(sys.argv[3]) if len(sys.argv) > 3 else 0.004
c = sqlite3.connect(path, isolation_level=None, timeout=30)
c.execute('PRAGMA foreign_keys=ON'); c.execute('PRAGMA synchronous=FULL'); c.execute(f'PRAGMA wal_autocheckpoint={ckpt}')
mid = c.execute("select id from metrics where name='weight'").fetchone()[0]
n = 0; stop = path + '.stop'
while not os.path.exists(stop):
    c.execute('BEGIN IMMEDIATE')
    c.execute("INSERT INTO entities(type,created_at,updated_at) VALUES('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'))")
    pid = c.execute('select last_insert_rowid()').fetchone()[0]
    c.execute("INSERT INTO pages(id,kind,day,body) VALUES(?, 'memo', '2026-09-30', ?)", (pid, 'live ' + 'x' * 600))
    c.execute("INSERT INTO measurements(metric_id,day,value,recorded_at,source) VALUES(?, '2026-09-30', 1.0, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'live')", (mid,))
    c.execute('COMMIT'); n += 1
    time.sleep(gap)
print(n)
