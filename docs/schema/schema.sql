-- ============================================================
-- Lifelog schema v1: the single init file, edited in place until the freeze;
-- numbered migrations begin only after the freeze (D13).
-- Each table's rules are comments INSIDE its CREATE statement, so .schema shows them (a comment
-- outside a statement is not stored in the file); the rules that span tables: SELECT * FROM lifelog_meta;
-- Every writer connection: SQLite >= 3.51.3; PRAGMA foreign_keys = ON;
-- PRAGMA recursive_triggers = ON; PRAGMA synchronous = FULL; PRAGMA trusted_schema = OFF;
-- and every write transaction starts with BEGIN IMMEDIATE (docs/contract/connections.md).
-- ============================================================
PRAGMA application_id = 0x4C494645;   -- 'LIFE' — recognizable to file(1) and tools
PRAGMA user_version  = 1;
PRAGMA journal_mode  = WAL;           -- persistent; readers (Datasette) don't block the writer

CREATE TABLE lifelog_meta (
  -- the rules that span tables, readable with a SELECT by someone who has only this file;
  -- the rules of one table are comments inside its own CREATE statement
  -- rows are rules the owner adds or removes; deleting one removes the rule from the file itself
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;
INSERT INTO lifelog_meta(key, value) VALUES
  ('schema',    'lifelog v1: the journal (one page per day), wiki, people, places, health metrics and the files kept as their text and a small picture, of one person: a life log, not a project manager; the rules of each table are comments inside its CREATE statement (.schema), the rules that span tables are these rows'),
  ('instants',  'every *_at column is a UTC ISO-8601 TEXT instant with milliseconds, e.g. 2026-06-09T21:14:03.482Z, written by the app, never by a SQLite default (CURRENT_TIMESTAMP has no milliseconds and is not ISO-8601; a trigger writes strftime(''%Y-%m-%dT%H:%M:%fZ'',''now'')); fixed width, so it sorts as text; CHECK strftime(''%Y-%m-%dT%H:%M:%fZ'', x) IS x; created_at, on every table that has it, is when the row was written to life.db, never back-dated (when a thing happened is its day or its other *_at)'),
  ('days',      'every *_day column (and day) is the LOCAL calendar date YYYY-MM-DD where the thing happened, written by the app from the local calendar of the device that captured it (never a server''s), never recomputed from an instant; CHECK date(x) IS x (IS, not =: a CHECK passes on NULL, and date(''2026-9-3'') is NULL)'),
  ('deletes',   'life data is never deleted except links rows: an entity is a tombstone (entities.deleted_at), a measurement is corrected by inserting a row; BEFORE DELETE triggers enforce it on entities and every domain row; every read path filters entities.deleted_at IS NULL; the registries (link_kinds, lifelog_meta) are the owner''s administrative rows, deletable while nothing references them (each CREATE comment says so)'),
  ('source',    'entities, links, measurements and habit_periods: source names the writer of the row (ui, cli, api, agent:<name>, import:<name>; schema for the rows this file seeds); written at insert, never changed; import_key, on entities and on measurements, is unique per source'),
  ('writers',   'one writing application; every connection sets foreign_keys=ON, recursive_triggers=ON, synchronous=FULL, trusted_schema=OFF and starts write transactions with BEGIN IMMEDIATE; every other tool opens the file read-only; imports use INSERT ... ON CONFLICT DO NOTHING, never OR IGNORE (skips CHECK/NOT NULL violations silently) or OR REPLACE (a delete)'),
  ('sqlite',    'writers need SQLite >= 3.51.3 (fixes a WAL race between concurrent writers and checkpoints); migrations need >= 3.53 (ALTER TABLE ADD/DROP CONSTRAINT); CHECKs use only functions every such version has'),
  ('evolution', 'after the freeze (the first row written that cannot be replayed from an import; before it a file is rebuilt, not migrated): numbered forward-only SQL migrations, additive only (new tables, columns and indexes; a named CHECK may be replaced with ALTER TABLE DROP/ADD CONSTRAINT, so every CHECK is named), counted in PRAGMA user_version');

CREATE TABLE entities (
  -- The shared spine: one row per linkable thing (page, person, place, metric, file). Its domain row has the
  -- SAME id: the app inserts this row first with INSERT ... RETURNING id and binds that id in the same
  -- transaction; never last_insert_rowid() across statements (any insert in between, a link or a ghost
  -- page, moves it). UNIQUE(id, entity_type) plus the composite FK (id, entity_type) of every domain table
  -- make a row's type and its table agree.
  -- A person, a place, a metric or a file is also a page (D20, D27, D9): one id, with a pages row whose title is the
  -- handle [[wikilinks]] write; a person also has a people row whose FK points at that pages row, a metric a metrics
  -- row, a file a files row, a place may have a places row (its point, D21). A ghost page is promoted by UPDATE entities SET entity_type = 'person' (the FK
  -- cascades it to pages.entity_type), then a person's people row is inserted.
  -- Nothing is ever deleted: deleted_at is the tombstone (D11), enforced by BEFORE DELETE triggers.
  -- import_key: the key a writer that may send the row twice gives it (an importer, an offline phone, a
  -- retrying agent); whether the row was imported is source, not import_key. Unique per source, written at
  -- insert and never changed. Insert with ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL
  -- DO NOTHING RETURNING id: no id back = imported before, so no domain row is inserted.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL CONSTRAINT entities_entity_type
                  CHECK (entity_type IN ('page','person','place','metric','file')),
  created_at  TEXT NOT NULL CONSTRAINT entities_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),   -- the write time (lifelog_meta.instants)
  updated_at  TEXT NOT NULL CONSTRAINT entities_updated_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', updated_at) IS updated_at),   -- kept by the *_touch triggers
  deleted_at  TEXT     CONSTRAINT entities_deleted_at CHECK (deleted_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', deleted_at) IS deleted_at),   -- the tombstone
  source      TEXT NOT NULL CONSTRAINT entities_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  import_key  TEXT,                        -- the sender's key, unique per source; NULL = sent once. Imported or not: source
  UNIQUE (id, entity_type)
) STRICT;
CREATE UNIQUE INDEX entities_import ON entities(source, import_key) WHERE import_key IS NOT NULL;

CREATE TABLE pages (
  -- All prose (D5): every page is titled, unique and linkable: an essay, a reference page, a tag, the page
  -- of a person, a place, a metric or a file (its entity_type says which, D20, D27, D9), and the journal. The journal is one
  -- DAY PAGE per local day, titled YYYY-MM-DD ('2026-09-29'): its title equals its day (pages_day_page), so
  -- [[2026-09-29]] reaches it. Capture appends to today's page, created on the first write. A day page is
  -- never a person or a place (pages_day_page_plain): a promotion of it is refused (D20).
  -- Where was I: the places the owner was at that day are links(kind='at') from its day page (D16).
  -- A title is permanent and a valid file name on every OS: a page is never renamed. Its text and typed links move
  -- to the page holding the new title (a new page, or an existing one only when the old page is empty), the old one
  -- becomes a '#REDIRECT [[New]]' stub with links(kind='redirect') (contract/titles-and-wikilinks).
  -- Uniqueness is on title_key = NFC(casefold(NFC(title))), computed by the app because SQLite cannot fold
  -- Unicode: 'Café' = 'CAFÉ' = NFD 'Café'. Look a page up with WHERE title_key = :key. The key is derived.
  -- links(kind='wikilink') from a page always equal the [[titles]] and #tags its CommonMark text names,
  -- rebuilt on every save; an invalid target makes no link and never blocks the save (D19).
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'page' CONSTRAINT pages_entity_type CHECK (entity_type IN ('page','person','place','metric','file')),   -- 'page', or the named entity this page is
  title       TEXT NOT NULL,              -- filename-safe, immutable; a day page's is its day
  title_key   TEXT NOT NULL,              -- NFC(casefold(NFC(title))), app-computed, unique
  day         TEXT,                       -- local day it was written: a day page's day; a page written on purpose has one (a file's: the day it was made, when the file says), a link target the app created has none
  body        TEXT NOT NULL DEFAULT '',   -- CommonMark; [[Wiki Links]] inline
  UNIQUE (id, entity_type),               -- the parent key of people
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type) ON UPDATE CASCADE,   -- a promoted page follows its entity's type
  CONSTRAINT pages_day_page CHECK (date(title) IS NOT title OR day IS title),   -- a page titled with a day is that day's page
  CONSTRAINT pages_day_page_plain CHECK (date(title) IS NOT title OR entity_type = 'page'),   -- ...and stays a plain page
  CONSTRAINT pages_key_folded CHECK (length(title_key) >= 1 AND title_key = trim(title_key)
                               AND title_key NOT GLOB '*[A-Z]*'),      -- a folded key has no ASCII capitals
  CONSTRAINT pages_key_ascii CHECK (title GLOB '*[^ -~]*' OR title_key = lower(title)),   -- pure-ASCII titles: the DB verifies the key
  CONSTRAINT pages_title_len CHECK (title = trim(title) AND length(title) >= 1
                           AND length(CAST(title AS BLOB)) <= 240),  -- bytes: a filename limit is 255 bytes
  CONSTRAINT pages_title_safe CHECK (   -- a title must be a valid file name on Linux, macOS and Windows: keep it safe
         title NOT GLOB '*[/\:*?"<>|]*'            -- path separators and Windows-reserved characters
         AND title NOT GLOB ('*[' || char(1) || '-' || char(31) || char(127) || '-' || char(159) || char(173) || char(1564)
                             || char(8203) || char(8206) || '-' || char(8207) || char(8234) || '-' || char(8238)
                             || char(8288) || '-' || char(8292) || char(8294) || '-' || char(8297) || char(65279) || ']*')
                                                   -- control characters (C0, DEL, C1) and invisible or bidi ones: soft hyphen, Arabic
                                                   -- letter mark, zero-width space, LRM/RLM, embeddings and overrides, word joiner and
                                                   -- invisible operators, isolates, BOM. ZWNJ/ZWJ (U+200C/D) stay: scripts and emoji need them
         AND instr(title, char(0)) = 0
         AND substr(title, 1, 1) <> '.' AND substr(title, -1) <> '.'   -- no hidden files, '..', trailing dot
         AND upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)
             NOT IN ('CON','PRN','AUX','NUL',                -- Windows device names, bare or before an extension (CON.backup)
               'COM1','COM2','COM3','COM4','COM5','COM6','COM7','COM8','COM9','COM¹','COM²','COM³',
               'LPT1','LPT2','LPT3','LPT4','LPT5','LPT6','LPT7','LPT8','LPT9','LPT¹','LPT²','LPT³')),
  CONSTRAINT pages_day CHECK (day IS NULL OR date(day) IS day)
) STRICT;
CREATE UNIQUE INDEX pages_title ON pages(title_key);
CREATE INDEX pages_day ON pages(day);
CREATE TRIGGER pages_title_fixed BEFORE UPDATE OF title ON pages
  WHEN NEW.title IS NOT OLD.title
BEGIN
  -- a rename would repoint every [[Old Title]] in decades of prose (D5); title_key is derived and may be recomputed
  SELECT RAISE(ABORT, 'titles are immutable: create the new page and make this one a #REDIRECT stub');
END;

CREATE VIRTUAL TABLE pages_fts USING fts5(
  -- derived: external-content FTS5 kept in sync by the two triggers below (a page is never deleted:
  -- pages_no_delete), checked by the FTS5 integrity-check (contract/integrity-checks); rebuild with
  -- INSERT INTO pages_fts(pages_fts) VALUES('rebuild'). Tokenizer unicode61 folds accents
  -- (Zurich finds Zürich); a CJK run is ONE token (a known limit of unicode61).
  title, body, content='pages', content_rowid='id'
);
CREATE TRIGGER pages_fts_insert AFTER INSERT ON pages BEGIN
  INSERT INTO pages_fts(rowid, title, body) VALUES (NEW.id, NEW.title, NEW.body);
END;
CREATE TRIGGER pages_fts_update AFTER UPDATE OF title, body ON pages BEGIN
  -- only the indexed columns: a promotion does not re-index the body
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES ('delete', OLD.id, OLD.title, OLD.body);
  INSERT INTO pages_fts(rowid, title, body) VALUES (NEW.id, NEW.title, NEW.body);
END;

CREATE TABLE people (
  -- people in the owner's life; relationships between them are links (friend, family, parent-of).
  -- A person is also a page with the same id (D20): its title is the permanent handle [[wikilinks]] write
  -- ('Sam (barber)' tells two Sams apart), its body holds the prose; name is the editable full name.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'person' CONSTRAINT people_entity_type CHECK (entity_type = 'person'),
  name        TEXT NOT NULL,
  birth_day   TEXT CONSTRAINT people_birth_day CHECK (birth_day IS NULL OR date(birth_day) IS birth_day),
  death_day   TEXT CONSTRAINT people_death_day CHECK (death_day IS NULL OR date(death_day) IS death_day),
  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),
  CONSTRAINT people_death_day_order CHECK (death_day IS NULL OR birth_day IS NULL OR death_day >= birth_day)
) STRICT;

CREATE TABLE places (
  -- where a place is (D21): one point and a radius, never where the owner was — that is the at links of the day
  -- pages (D16). Optional: a place without a row has no point and is never matched. A photo's position is matched
  -- when the photo is kept, to the live places whose circle holds it: the smallest radius first, then the nearest.
  -- The distance needs no math function: the app binds the metres per degree of longitude at the position's latitude
  -- (111320 * cos(lat)) and compares the squared equirectangular distance with radius_m squared; a circle is not
  -- matched across the 180th meridian. link_days = 1: the photo's day gets an at link to the place; 0: the place is
  -- recognised (its photos are never asked about) and not linked — home, work. The photo's position is never stored.
  -- A wrong point is fixed by UPDATE; never deleted: tombstone the entity (D11).
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'place' CONSTRAINT places_entity_type CHECK (entity_type = 'place'),
  lat         REAL NOT NULL CONSTRAINT places_lat CHECK (lat BETWEEN -90 AND 90),      -- WGS84 degrees
  lon         REAL NOT NULL CONSTRAINT places_lon CHECK (lon BETWEEN -180 AND 180),
  radius_m    INTEGER NOT NULL CONSTRAINT places_radius CHECK (radius_m BETWEEN 10 AND 100000),   -- a café ~100, a city ~10000
  link_days   INTEGER NOT NULL DEFAULT 1 CONSTRAINT places_link_days CHECK (link_days IN (0, 1)),
  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),
  CONSTRAINT places_not_null_island CHECK (lat <> 0 OR lon <> 0)   -- 0°, 0° is how photo metadata says "no location"
) STRICT;

CREATE TABLE metrics (
  -- what is measured (D7). A metric is also a page with the same id (D27), as a person is: its title is
  -- its name, one series forever (title_key makes 'Weight' and 'weight' one name, and a title never
  -- changes), its body what the owner writes about it, and [[Weight]] in the journal reaches it. The unit
  -- gives every stored value its meaning, so it never changes (metrics_unit_fixed). Seeded with Mood (D6).
  -- A category is a page: a metric is filed in one by a part-of link from its page (D26).
  -- A habit is NOT a category: a metric is a habit while it has habit_periods (D24).
  -- A metric registered by mistake is tombstoned, never deleted (D11); its readings stay life data.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'metric' CONSTRAINT metrics_entity_type CHECK (entity_type = 'metric'),
  unit        TEXT NOT NULL DEFAULT '',   -- 'kg', 'bpm', 'h'; '' for 1-5 scales and 0/1 habits
  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type)
) STRICT;
CREATE TRIGGER metrics_unit_fixed BEFORE UPDATE OF unit ON metrics
  WHEN NEW.unit IS NOT OLD.unit
BEGIN
  -- changing the unit would silently reinterpret the whole series
  SELECT RAISE(ABORT, 'metrics.unit is fixed: it defines what every stored value means; register a new metric instead');
END;
INSERT INTO entities(id, entity_type, created_at, updated_at, source) VALUES
  (1, 'metric', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'schema');
INSERT INTO pages(id, entity_type, title, title_key, body) VALUES
  (1, 'metric', 'Mood', 'mood', '1-5; attached to its day page via measurements.captured_with_id when posted');
INSERT INTO metrics(id, unit) VALUES (1, '');

CREATE TABLE files (
  -- a file the owner keeps (D9): a recording, a PDF, a scan, a photo, a video. A file is also a page with the same
  -- id, as a person is (D20): its title is its handle (![[2026-10-04 Lake.jpg]] in a day page is a wikilink to it),
  -- its body the file's text — a transcript, the text of a PDF or a scan, a caption — so search finds it.
  -- The ORIGINAL is never stored in life.db and never managed by its writer: it stays outside (a photo library), or
  -- is deleted once its text is kept here. sha256 names the original: one page per original, whatever writer sends
  -- it — look it up first (WHERE sha256 = :sha256) and link the page found; files_sha256 refuses a second row.
  -- preview is the picture kept for good: a JPEG, its long edge at most 1600 px (the writer scales it), at most 1 MB,
  -- with no metadata (a photo's GPS is the location history D21 leaves out); NULL where the text is the point (a
  -- recording, a PDF). A video keeps one frame. sha256 and mime never change
  -- (files_original_fixed); a missing preview may be added later. Never deleted: tombstone the entity (D11).
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'file' CONSTRAINT files_entity_type CHECK (entity_type = 'file'),
  sha256      TEXT NOT NULL CONSTRAINT files_sha256 CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),   -- of the original's bytes, lowercase hex
  mime        TEXT NOT NULL CONSTRAINT files_mime CHECK (length(mime) <= 127 AND mime GLOB '[a-z]*/[a-z0-9]*'
                           AND mime NOT GLOB '*[^a-z0-9/.+-]*' AND mime NOT GLOB '*/*/*'),   -- of the original, lowercase: 'audio/mp4', 'image/heic', 'application/pdf'
  preview     BLOB CONSTRAINT files_preview CHECK (preview IS NULL OR (substr(preview, 1, 3) IS x'FFD8FF'
                           AND length(preview) <= 1048576)),   -- a JPEG; IS, not =: substr of an empty blob is NULL
  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type)
) STRICT;
CREATE UNIQUE INDEX files_sha256 ON files(sha256);   -- one page per original
CREATE TRIGGER files_original_fixed BEFORE UPDATE OF sha256, mime ON files
  WHEN NEW.sha256 IS NOT OLD.sha256 OR NEW.mime IS NOT OLD.mime
BEGIN
  -- the hash and the type name the original; another original is another file
  SELECT RAISE(ABORT, 'files.sha256 and mime name the original and never change: another original is another file');
END;

CREATE TABLE measurements (
  -- one row per data point (the FxLifeSheet shape). The table is append-only, enforced by triggers: never
  -- UPDATE or DELETE. A correction is a new row whose supersedes_id names the row it corrects, at most one
  -- per row (chain: correct the correction); a correction with a NULL value RETRACTS the row it corrects.
  -- Read through the view measurement_values. day is when the value was true, created_at when it was written
  -- down, taken_at + tz when and where it was measured (tz: IANA zone; NULL = unknown).
  -- Imports: INSERT ... ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING;
  -- never OR IGNORE (it silently skips CHECK / NOT NULL violations).
  id               INTEGER PRIMARY KEY,
  metric_id        INTEGER NOT NULL REFERENCES metrics(id),
  day              TEXT NOT NULL CONSTRAINT measurements_day CHECK (date(day) IS day),  -- local date the value refers to
  taken_at         TEXT CONSTRAINT measurements_taken_at CHECK (taken_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', taken_at) IS taken_at),
  tz               TEXT CONSTRAINT measurements_tz CHECK (tz IS NULL OR (length(tz) BETWEEN 1 AND 64 AND tz NOT GLOB '*[^A-Za-z0-9_/+-]*')),
  value            REAL,                        -- numeric only, by design (D7); NULL only on a correction: it RETRACTS the row it supersedes
  created_at       TEXT NOT NULL CONSTRAINT measurements_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source           TEXT NOT NULL CONSTRAINT measurements_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  import_key       TEXT,                        -- importer's dedup key, unique per (source, metric)
  captured_with_id INTEGER REFERENCES entities(id),      -- provenance: the page (a day page) this reading was captured with
  supersedes_id    INTEGER REFERENCES measurements(id),  -- optional: corrects an earlier row
  CONSTRAINT measurements_not_self CHECK (supersedes_id IS NULL OR supersedes_id <> id),
  CONSTRAINT measurements_first_has_value CHECK (value IS NOT NULL OR supersedes_id IS NOT NULL),   -- a first reading has a value; only a correction may retract
  CONSTRAINT measurements_value_finite CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)   -- finite: rejects ±Infinity (a NaN arrives as NULL)
) STRICT;
CREATE INDEX measurements_series ON measurements(metric_id, day);
CREATE UNIQUE INDEX measurements_import
  ON measurements(source, import_key, metric_id) WHERE import_key IS NOT NULL;
CREATE UNIQUE INDEX measurements_one_correction
  ON measurements(supersedes_id) WHERE supersedes_id IS NOT NULL;  -- one correction per row; also serves measurement_values
CREATE INDEX measurements_day ON measurements(day);                -- day view
CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements
BEGIN
  SELECT RAISE(ABORT, 'measurements are append-only: correct by inserting a row with supersedes_id');
END;
CREATE TRIGGER measurements_no_delete BEFORE DELETE ON measurements
BEGIN
  SELECT RAISE(ABORT, 'measurements are never deleted: correct by inserting a row with supersedes_id');
END;
CREATE TRIGGER measurements_supersede_metric AFTER INSERT ON measurements
  WHEN NEW.supersedes_id IS NOT NULL
BEGIN
  -- a correction must correct an EXISTING row of the SAME metric (IS NOT, not <>: a dangling
  -- supersedes_id yields NULL, and NULL <> x is NULL = pass). supersedes_id is set only at INSERT
  -- and must name an existing row, so correction chains can never form a cycle.
  SELECT RAISE(ABORT, 'supersedes_id must reference a measurement of the same metric')
   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;
END;
CREATE VIEW measurement_values AS
  -- the canonical read rule: rows nothing has corrected, minus retractions
  SELECT me.*
    FROM measurements me
   WHERE me.value IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM measurements x WHERE x.supersedes_id = me.id);

CREATE TABLE habit_periods (
  -- a metric is a HABIT while it has a period: the local days the owner meant to do it (D24). Check-ins
  -- stay in measurements (1 = done, 0 = not done that day); a day inside a period with no check-in is
  -- NOT RECORDED, never assumed done or not done. A habit is unitless (0/1). A restarted habit has several
  -- periods, which never overlap. A wrong period is corrected by UPDATE, never deleted.
  id         INTEGER PRIMARY KEY,
  metric_id  INTEGER NOT NULL REFERENCES metrics(id),
  start_day  TEXT NOT NULL CONSTRAINT habit_periods_start_day CHECK (date(start_day) IS start_day),
  end_day    TEXT CONSTRAINT habit_periods_end_day CHECK (end_day IS NULL OR date(end_day) IS end_day),   -- the last day, inclusive; NULL = still going
  source     TEXT NOT NULL CONSTRAINT habit_periods_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  CONSTRAINT habit_periods_order CHECK (end_day IS NULL OR end_day >= start_day),
  UNIQUE (metric_id, start_day)
) STRICT;
CREATE TRIGGER habit_periods_check_insert BEFORE INSERT ON habit_periods
BEGIN
  SELECT RAISE(ABORT, 'a habit is a unitless metric (0 = not done, 1 = done): this metric has a unit')
   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';
  -- a period with the same start is left to UNIQUE, so ON CONFLICT DO NOTHING re-runs it (this trigger fires first)
  SELECT RAISE(ABORT, 'periods of one habit never overlap: end the open one first')
   WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id
                    AND p.start_day IS NOT NEW.start_day
                    AND p.start_day <= coalesce(NEW.end_day, '9999-12-31')
                    AND coalesce(p.end_day, '9999-12-31') >= NEW.start_day);
END;
CREATE TRIGGER habit_periods_check_update BEFORE UPDATE OF metric_id, start_day, end_day ON habit_periods
BEGIN
  SELECT RAISE(ABORT, 'a habit is a unitless metric (0 = not done, 1 = done): this metric has a unit')
   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';
  SELECT RAISE(ABORT, 'periods of one habit never overlap')
   WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id
                    AND p.start_day <= coalesce(NEW.end_day, '9999-12-31')
                    AND coalesce(p.end_day, '9999-12-31') >= NEW.start_day);
END;
CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods
BEGIN
  SELECT RAISE(ABORT, 'habit periods are never deleted: correct a wrong one with UPDATE');
END;
CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods
  WHEN NEW.source IS NOT OLD.source
BEGIN
  -- provenance is written at insert and never changed, as on entities and links (lifelog_meta.source);
  -- the WHEN clause lets full-row updates through
  SELECT RAISE(ABORT, 'habit_periods.source is written at insert and never changed');
END;

CREATE TABLE link_kinds (
  -- the CLOSED registry of link kinds: a link's kind must be registered first (FK), and a kind's
  -- structure (symmetric flag, allowed endpoint entity types) is fixed at registration and enforced
  -- by a trigger on every link. Registering a kind is a deliberate INSERT, so a typo cannot create
  -- one. from_types / to_types: NULL = any entity type, else a comma list of entities.entity_type values
  -- ('person,place'); a misspelt token fails CLOSED (every link of that kind is rejected).
  -- an unreferenced kind may be deleted by the owner; a used one is refused by the links FK
  kind       TEXT PRIMARY KEY CONSTRAINT link_kinds_kind CHECK (kind = lower(kind) AND length(kind) > 0 AND kind NOT GLOB '*[^a-z0-9_-]*'),
  symmetric  INTEGER NOT NULL DEFAULT 0 CONSTRAINT link_kinds_symmetric CHECK (symmetric IN (0,1)),
  from_types TEXT CONSTRAINT link_kinds_from_types CHECK (from_types IS NULL OR (from_types NOT GLOB '*[^a-z,]*' AND from_types NOT GLOB ',*'
                         AND from_types NOT GLOB '*,' AND from_types NOT GLOB '*,,*')),
  to_types   TEXT CONSTRAINT link_kinds_to_types CHECK (to_types   IS NULL OR (to_types   NOT GLOB '*[^a-z,]*' AND to_types   NOT GLOB ',*'
                         AND to_types   NOT GLOB '*,' AND to_types   NOT GLOB '*,,*')),
  note       TEXT,
  CONSTRAINT link_kinds_mirror_valid CHECK (symmetric = 0 OR from_types IS to_types)   -- a mirrored edge must be valid in both directions
) STRICT;
INSERT INTO link_kinds(kind, symmetric, from_types, to_types, note) VALUES
  ('wikilink', 0, 'page,person,place,metric,file', 'page,person,place,metric,file', 'extracted from [[body]] on save (an embed ![[...]] is one); body is the truth'),
  ('redirect', 0, 'page',      'page,person,place,metric,file', 'old stub page → its replacement, a page, person, place, metric or file; renames, D5'),
  ('about',    0, NULL,        'person,place', 'entity → person/place it is about'),
  ('at',       0, 'page',      'place',        'day page → a place the owner was at that day; from a day page only, which the app checks (D16); a photo kept makes one to the place its position is in (D21)'),
  ('located-in', 0, 'place',   'place',        'containment: Tokyo → Japan; transitive — walk it with a recursive CTE'),
  ('parent-of', 0, 'person',   'person',       'parent → child; ''family'' stays the symmetric catch-all'),
  ('part-of',  0, NULL,        'page',         'anything → the page of a category it is filed in (Ferritin → Iron → Biomarkers, D26); walk it with a recursive CTE'),
  ('friend',   1, 'person',    'person',       NULL),
  ('family',   1, 'person',    'person',       NULL),
  ('related',  1, NULL,        NULL,           'anything ↔ anything');
CREATE TRIGGER link_kinds_structure_fixed BEFORE UPDATE OF symmetric, from_types, to_types ON link_kinds
  WHEN NEW.symmetric IS NOT OLD.symmetric OR NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types
BEGIN
  -- changing a kind's structure would leave edges that violate it, or half-edges; register a new kind
  SELECT RAISE(ABORT, 'a link kind''s structure (symmetric, endpoint types) is fixed at registration; register a new kind instead');
END;

CREATE TABLE links (
  -- one graph for everything: wiki backlinks, relationships, where the owner was, containment,
  -- redirects. Rows are hard-deleted (the one such table, D11) and immutable otherwise (delete and
  -- re-insert). Symmetric kinds are mirrored by trigger on insert AND delete, so a half-edge cannot
  -- exist and backlinks need only to_id. Cycles (e.g. located-in) are not prevented (D8).
  -- source names the writer, as on entities; written at insert, never changed.
  id         INTEGER PRIMARY KEY,
  from_id    INTEGER NOT NULL REFERENCES entities(id),
  to_id      INTEGER NOT NULL REFERENCES entities(id),
  kind       TEXT NOT NULL REFERENCES link_kinds(kind),  -- closed registry
  note       TEXT,
  created_at TEXT NOT NULL CONSTRAINT links_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source     TEXT NOT NULL CONSTRAINT links_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  UNIQUE (from_id, to_id, kind)
) STRICT;
CREATE INDEX links_to ON links(to_id);   -- backlinks query (from_id is served by the UNIQUE index)

CREATE TRIGGER links_fixed BEFORE UPDATE OF from_id, to_id, kind, source ON links
  WHEN NEW.from_id IS NOT OLD.from_id OR NEW.to_id IS NOT OLD.to_id OR NEW.kind IS NOT OLD.kind
    OR NEW.source IS NOT OLD.source
BEGIN
  -- only note may change; to change anything else, delete and re-insert
  SELECT RAISE(ABORT, 'links are immutable: delete and re-insert');
END;
CREATE TRIGGER links_endpoint_types BEFORE INSERT ON links
BEGIN
  -- the registry is closed even on a connection that forgot PRAGMA foreign_keys=ON; an unknown
  -- endpoint id counts as type '?' and so is rejected for typed kinds
  SELECT RAISE(ABORT, 'link kind is not registered in link_kinds')
   WHERE NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);
  SELECT RAISE(ABORT, 'link endpoint type not allowed for this kind (see link_kinds.from_types / to_types)')
   WHERE EXISTS (SELECT 1 FROM link_kinds k
                  WHERE k.kind = NEW.kind
                    AND ((k.from_types IS NOT NULL
                          AND instr(',' || k.from_types || ',',
                                    ',' || coalesce((SELECT entity_type FROM entities WHERE id = NEW.from_id), '?') || ',') = 0)
                      OR (k.to_types IS NOT NULL
                          AND instr(',' || k.to_types || ',',
                                    ',' || coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), '?') || ',') = 0)));
END;
CREATE TRIGGER links_mirror_insert AFTER INSERT ON links
  WHEN NEW.from_id <> NEW.to_id
   AND (SELECT symmetric FROM link_kinds WHERE kind = NEW.kind) = 1
BEGIN
  -- mirror a symmetric edge; OR IGNORE makes the re-fire find the row present and stop
  INSERT OR IGNORE INTO links(from_id, to_id, kind, note, created_at, source)
  VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NEW.source);
END;
CREATE TRIGGER links_mirror_delete AFTER DELETE ON links
  WHEN OLD.from_id <> OLD.to_id
   AND (SELECT symmetric FROM link_kinds WHERE kind = OLD.kind) = 1
BEGIN
  DELETE FROM links WHERE from_id = OLD.to_id AND to_id = OLD.from_id AND kind = OLD.kind;
END;

CREATE VIEW ghost_pages AS
  -- empty plain pages nobody points at, 30 days old: a link target created by a capture-time typo and
  -- never written (a page anything links to, a rename's target included, is not one). The page of a person, a place, a metric or a file is never a ghost,
  -- however empty (D20). The UI lists them; tombstoning is the owner's act.
  SELECT p.id, p.title, e.created_at
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.entity_type = 'page' AND p.body = '' AND e.deleted_at IS NULL
     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')
     AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id)
     AND NOT EXISTS (SELECT 1 FROM links l WHERE l.from_id = p.id);

CREATE TRIGGER pages_touch AFTER UPDATE ON pages BEGIN
  -- updated_at is kept in the DB, so every writer (CLI, agents, scripts) gets it right
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER people_touch AFTER UPDATE ON people BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER places_touch AFTER UPDATE ON places BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER files_touch AFTER UPDATE ON files BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities
  WHEN NEW.deleted_at IS NOT OLD.deleted_at
BEGIN
  -- tombstoning and un-tombstoning are changes too; watching deleted_at only, it cannot re-fire itself
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER entities_provenance_fixed BEFORE UPDATE OF source, import_key ON entities
  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key
BEGIN
  -- provenance is captured at insert, and a changed key would let a re-run import the row again;
  -- the WHEN clause lets full-row updates through
  SELECT RAISE(ABORT, 'entities.source and import_key are written at insert and never changed');
END;

CREATE TRIGGER entities_no_delete BEFORE DELETE ON entities
BEGIN
  -- no hard deletes (D11): an entity and its domain rows are tombstoned, never removed; under
  -- PRAGMA recursive_triggers=ON these triggers also stop REPLACE from deleting a row. Only links rows are deleted.
  SELECT RAISE(ABORT, 'entities are never deleted: set entities.deleted_at (tombstone)');
END;
CREATE TRIGGER pages_no_delete BEFORE DELETE ON pages
BEGIN SELECT RAISE(ABORT, 'pages are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER people_no_delete BEFORE DELETE ON people
BEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER places_no_delete BEFORE DELETE ON places
BEGIN SELECT RAISE(ABORT, 'places are never deleted: fix a wrong point by UPDATE, tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER metrics_no_delete BEFORE DELETE ON metrics
BEGIN SELECT RAISE(ABORT, 'metrics are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER files_no_delete BEFORE DELETE ON files
BEGIN SELECT RAISE(ABORT, 'files are never deleted: tombstone the entity (entities.deleted_at)'); END;
