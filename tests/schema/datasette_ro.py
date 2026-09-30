"""Optional (run_all.py --datasette): Datasette opens the file read-only and its SQL console accepts only SELECT (§2.6, D14)."""
import asyncio, os, sqlite3, sys, tempfile
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'lib'))
from kit import Suite, DDL
from datasette.app import Datasette
import datasette
S = Suite(f'datasette {datasette.__version__}')
path = os.path.join(tempfile.mkdtemp(), 'life.db')
w = sqlite3.connect(path, isolation_level=None); w.executescript(DDL); w.execute("INSERT INTO lifelog_meta VALUES ('probe','1')")
async def main():
    ds = Datasette([path]); conn = ds.get_database('life').connect()
    S.K('its connection can read', conn.execute("select value from lifelog_meta where key='probe'").fetchone() == ('1',))
    for st in ("INSERT INTO lifelog_meta VALUES ('x','y')", 'DELETE FROM lifelog_meta', 'DROP TABLE lifelog_meta'):
        try: conn.execute(st); S.K('its connection refuses ' + st[:24], False)
        except sqlite3.Error as e: S.K('its connection refuses ' + st[:24], 'readonly' in str(e), e)
    for st in ("INSERT+INTO+lifelog_meta+VALUES+('a','b')", 'DELETE+FROM+lifelog_meta'):
        r = await ds.client.get(f'/life.json?sql={st}&_shape=array'); S.K('the SQL console rejects ' + st[:12], r.status_code == 400, r.text[:80])
    r = await ds.client.get('/life.json?sql=select+count(*)+as+n+from+entities&_shape=array'); S.K('the SQL console runs a SELECT', r.status_code == 200)
asyncio.run(main())
S.K('the file is unchanged after every attempt', w.execute("select count(*) from lifelog_meta where key in ('a','x')").fetchone()[0] == 0)
S.done()
