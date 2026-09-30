import sqlite3, os, tempfile, asyncio, sys
DDL = open(sys.argv[1], encoding='utf-8').read()
d = tempfile.mkdtemp(); path = os.path.join(d,'life.db')
w = sqlite3.connect(path, isolation_level=None); w.executescript(DDL); w.execute("INSERT INTO lifelog_meta VALUES ('probe','1')")
from datasette.app import Datasette
import datasette
res=[]
def K(l,c,dt=''):
    res.append(bool(c)); 
    if not c: print('  FAIL',l,dt)
async def main():
    ds = Datasette([path]); conn = ds.get_database('life').connect()
    K('datasette connection can read', conn.execute("select value from lifelog_meta where key='probe'").fetchone()==('1',))
    for st in ("INSERT INTO lifelog_meta VALUES ('x','y')","DELETE FROM lifelog_meta","DROP TABLE lifelog_meta"):
        try: conn.execute(st); K('datasette connection refuses '+st[:24], False)
        except sqlite3.Error as e: K('datasette connection refuses '+st[:24], 'readonly' in str(e), str(e))
    r = await ds.client.get("/life.json?sql=INSERT+INTO+lifelog_meta+VALUES+('a','b')&_shape=array"); K('SQL console rejects INSERT', r.status_code==400, r.text[:80])
    r = await ds.client.get("/life.json?sql=DELETE+FROM+lifelog_meta&_shape=array"); K('SQL console rejects DELETE', r.status_code==400, r.text[:80])
    r = await ds.client.get("/life.json?sql=select+count(*)+as+n+from+entities&_shape=array"); K('SQL console runs SELECT', r.status_code==200)
    r = await ds.client.get("/life/lifelog_meta.json?_shape=array"); K('table browsing works', r.status_code==200)
asyncio.run(main())
n_meta = w.execute("select count(*) from lifelog_meta").fetchone()[0]
K('file unchanged after every attempt (seed rows + the 1 probe row)', w.execute("select count(*) from lifelog_meta where key in ('a','x')").fetchone()[0]==0)
print(f'datasette {datasette.__version__}: {sum(res)}/{len(res)} met expectations')
