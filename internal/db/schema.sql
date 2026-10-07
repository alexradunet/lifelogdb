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
  ('schema',    'lifelog v1: the journal (one page per day), wiki, people, places, recorded life periods, health metrics and the files kept as their text and a small picture, of one person: a life log, not a project manager; the rules of each table are comments inside its CREATE statement (.schema), the rules that span tables are these rows'),
  ('instants',  'every *_at column is an exact 24-character ASCII UTC ISO-8601 TEXT instant, years 0000..9999 and hours 00..23, with milliseconds, e.g. 2026-06-09T21:14:03.482Z, written by the app, never by a SQLite default (CURRENT_TIMESTAMP has no milliseconds and is not ISO-8601; a trigger writes strftime(''%Y-%m-%dT%H:%M:%fZ'',''now'')); fixed width, so it sorts as text; CHECK strftime(''%Y-%m-%dT%H:%M:%fZ'', x) IS x; created_at, on every table that has it, is when the row was written to life.db, never back-dated (when a thing happened is its day or its other *_at)'),
  ('days',      'every *_day column (and day) is the LOCAL exact 10-character ASCII calendar date YYYY-MM-DD, years 0000..9999, where the thing happened, written by the app from the local calendar of the device that captured it (never a server''s), never recomputed from an instant; CHECK explicit ASCII shape and date(x) IS x (IS, not =: a CHECK passes on NULL, and date(''2026-9-3'') is NULL)'),
  ('measurement_scope', 'a new non-NULL measurement associated by session_id requires a live session; tombstones retain facts, NULL retraction remains allowed; active queries exclude tombstoned-session facts, explicit historical reads retain scope and lifecycle'),
  ('deletes',   'life data is never deleted except links rows: an entity is a tombstone (entities.deleted_at), a measurement is corrected by inserting a row; BEFORE DELETE triggers enforce it on entities and every domain row; ordinary active reads filter lifecycle tombstones; explicit historical and append-only audits retain them; the registries (link_kinds, lifelog_meta) are the owner''s administrative rows, deletable while nothing references them (each CREATE comment says so)'),
  ('source',    'entities, sessions, links, measurements and habit_periods: source names the writer of the row (ui, cli, api, agent:<name>, import:<name>; schema for the rows this file seeds); written at insert, never changed; import_key, on entities and on measurements, is unique per source'),
  ('edit_revisions', 'entities.revision and sessions.revision are monotonic integer edit tokens independent of updated_at; names, body, day, type, lifecycle, typed detail, habit period and incident link changes advance it; no-ops do not, rollback restores it, exhaustion refuses; clients compare the token inside BEGIN IMMEDIATE, never a timestamp; exact increment counts are not meaningful'),
  ('typed_links', 'both endpoints of every retained link must satisfy link_kinds; a type change refuses invalid incoming or outgoing edges atomically; semantic integrity checks the registry independently of structural integrity'),
  ('writers',   'one writing application; every connection sets foreign_keys=ON, recursive_triggers=ON, synchronous=FULL, trusted_schema=OFF and starts write transactions with BEGIN IMMEDIATE; every other tool opens the file read-only; imports use INSERT ... ON CONFLICT DO NOTHING, never OR IGNORE (skips CHECK/NOT NULL violations silently) or OR REPLACE (a delete)'),
  ('sqlite',    'writers need SQLite >= 3.51.3 (fixes a WAL race between concurrent writers and checkpoints); migrations need >= 3.53 (ALTER TABLE ADD/DROP CONSTRAINT); CHECKs use only functions every such version has'),
  ('evolution', 'after the freeze (the first row written that cannot be replayed from an import; before it a file is rebuilt, not migrated): numbered forward-only SQL migrations, additive only (new tables, columns and indexes; a named CHECK may be replaced with ALTER TABLE DROP/ADD CONSTRAINT, so every CHECK is named), counted in PRAGMA user_version');

CREATE TABLE entities (
  -- One named identity and prose body per linkable thing (page, person, place, metric, file, period).
  -- Insert this row first with RETURNING id, then its owned preferred entity_names row in the same
  -- BEGIN IMMEDIATE transaction; preferred ownership is NOT NULL and checked by the deferred composite FK.
  -- Never carry last_insert_rowid() across statements. Typed extension rows use this same id and
  -- (id, entity_type) FK directly; a place's point is optional. Promotion changes type, not identity.
  -- A canonical date key is the one plain journal-day identity for that local day: its day equals its
  -- preferred key, neither may transfer, and its only registry spelling is that date. Other objects
  -- may have a day without being journal days. Their aliases and preferred names remain editable.
  -- Body saves synchronize wikilinks by resolved entity id (D19); invalid names do not block a save.
  -- Type changes refuse any incoming or outgoing link whose registered endpoint types would become invalid;
  -- no retained edge is silently removed. Nothing is ever deleted: deleted_at is the tombstone (D11).
  -- import_key: the key a writer that may send the row twice gives it (an importer, an offline phone, a
  -- retrying agent); whether the row was imported is source, not import_key. Unique per source, written at
  -- insert and never changed. Insert with ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL
  -- DO NOTHING RETURNING id: no id back = imported before, so no domain row is inserted.
  preferred_name_key TEXT NOT NULL,       -- selects exactly one owned registry row; spelling lives only there
  day         TEXT CONSTRAINT entities_day CHECK (day IS NULL OR length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day),
  body        TEXT NOT NULL DEFAULT '',   -- CommonMark, including journal and file text
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL CONSTRAINT entities_entity_type
                  CHECK (entity_type IN ('page','person','place','metric','file','period')),
  created_at  TEXT NOT NULL CONSTRAINT entities_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),   -- the write time (lifelog_meta.instants)
  updated_at  TEXT NOT NULL CONSTRAINT entities_updated_at CHECK (length(updated_at) = 24 AND updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(updated_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', updated_at) IS updated_at),   -- kept by the *_touch triggers
  revision    INTEGER NOT NULL DEFAULT 1 CONSTRAINT entities_revision CHECK (revision >= 1), -- edit token, independent of the clock; overflow refuses the write
  deleted_at  TEXT     CONSTRAINT entities_deleted_at CHECK (deleted_at IS NULL OR length(deleted_at) = 24 AND deleted_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(deleted_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', deleted_at) IS deleted_at),   -- the tombstone
  source      TEXT NOT NULL CONSTRAINT entities_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  import_key  TEXT,                        -- the sender's key, unique per source; NULL = sent once. Imported or not: source
  UNIQUE (id, entity_type),
  CONSTRAINT entities_day_page CHECK (NOT (length(preferred_name_key) = 10 AND date(preferred_name_key) IS preferred_name_key) OR day IS preferred_name_key),
  CONSTRAINT entities_day_page_plain CHECK (NOT (length(preferred_name_key) = 10 AND date(preferred_name_key) IS preferred_name_key) OR entity_type = 'page'),
  FOREIGN KEY (id, preferred_name_key) REFERENCES entity_names(entity_id, name_key) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE UNIQUE INDEX entities_import ON entities(source, import_key) WHERE import_key IS NOT NULL;

CREATE TABLE entity_names (
  -- The sole authoritative spellings: one preferred name selected by entities, plus direct retained aliases.
  -- Globally unique name_key = NFC(casefold(NFC(title))) is computed by the writer; SQLite verifies ASCII.
  -- Every name maps directly to one entity_id. Keys and owners never change and names are never deleted,
  -- including after tombstoning; rename is not merge, even into an empty ghost. A case-only spelling edit
  -- retains its key; selecting an already-owned alias creates no new registry identity. Old prose is not rewritten.
  -- Canonical journal dates are reserved to their matching plain owner and permit no additional aliases.
  -- Filename checks below are supplemented by the pinned Unicode/raw+NFC CommonMark addressability
  -- predicate in contract/titles-and-wikilinks.md. Display text after | registers no alias.
  id          INTEGER PRIMARY KEY,
  entity_id   INTEGER NOT NULL REFERENCES entities(id),
  title       TEXT NOT NULL,              -- preferred spelling or retained alias; case-only edits retain the key
  name_key    TEXT NOT NULL UNIQUE,       -- NFC(casefold(NFC(title))), computed by the writer
  UNIQUE (entity_id, name_key),
  CONSTRAINT entity_names_key_folded CHECK (length(name_key) >= 1 AND name_key = trim(name_key)
                               AND name_key NOT GLOB '*[A-Z]*'),      -- a folded key has no ASCII capitals
  CONSTRAINT entity_names_key_ascii CHECK (title GLOB '*[^ -~]*' OR name_key = lower(title)),   -- pure-ASCII titles: the DB verifies the key
  CONSTRAINT entity_names_title_len CHECK (title = trim(title) AND length(title) >= 1
                           AND length(CAST(title AS BLOB)) <= 240),  -- bytes: a filename limit is 255 bytes
  CONSTRAINT entity_names_title_safe CHECK (   -- a title must be a valid file name on Linux, macOS and Windows: keep it safe
         title NOT GLOB '*[/\:*?"<>|]*' AND instr(title, '[') = 0 AND instr(title, ']') = 0            -- path separators and Windows-reserved characters
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
               'LPT1','LPT2','LPT3','LPT4','LPT5','LPT6','LPT7','LPT8','LPT9','LPT¹','LPT²','LPT³'))
) STRICT;
CREATE INDEX entities_day ON entities(day);
CREATE TRIGGER entity_names_fixed BEFORE UPDATE ON entity_names
 WHEN NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key
BEGIN SELECT RAISE(ABORT, 'name keys and ownership are immutable'); END;
CREATE TRIGGER entity_names_no_delete BEFORE DELETE ON entity_names
BEGIN SELECT RAISE(ABORT, 'names remain reserved, including tombstones'); END;
CREATE TRIGGER entity_names_day_insert BEFORE INSERT ON entity_names BEGIN
 SELECT RAISE(ABORT, 'a journal day has only its canonical date name')
 WHERE EXISTS (SELECT 1 FROM entities e WHERE e.id = NEW.entity_id
   AND ((length(e.preferred_name_key) = 10 AND date(e.preferred_name_key) IS e.preferred_name_key
         AND (NEW.name_key IS NOT e.preferred_name_key OR NEW.title IS NOT e.preferred_name_key))
     OR (length(NEW.name_key) = 10 AND date(NEW.name_key) IS NEW.name_key AND e.preferred_name_key IS NOT NEW.name_key)));
END;
CREATE TRIGGER entity_names_day_update BEFORE UPDATE OF title ON entity_names BEGIN
 SELECT RAISE(ABORT, 'a journal day spelling is its canonical date')
 WHERE length(OLD.name_key) = 10 AND date(OLD.name_key) IS OLD.name_key AND NEW.title IS NOT OLD.name_key;
END;
CREATE TRIGGER entities_day_identity BEFORE UPDATE OF preferred_name_key ON entities
 WHEN NEW.preferred_name_key IS NOT OLD.preferred_name_key
BEGIN
 SELECT RAISE(ABORT, 'journal day identity cannot be renamed or acquired through aliases')
 WHERE (length(OLD.preferred_name_key) = 10 AND date(OLD.preferred_name_key) IS OLD.preferred_name_key)
    OR (length(NEW.preferred_name_key) = 10 AND date(NEW.preferred_name_key) IS NEW.preferred_name_key
        AND EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id = OLD.id AND n.name_key IS NOT NEW.preferred_name_key));
END;

CREATE VIEW entity_search_content AS
 SELECT e.id, n.title AS preferred,
        (SELECT group_concat(title, char(10) ORDER BY name_key) FROM entity_names WHERE entity_id = e.id) AS all_names,
        e.body
   FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key;
CREATE VIRTUAL TABLE entities_fts USING fts5(
  -- Derived, one document per entity: body once, all names deterministically ordered.
  -- A phrase may cross adjacent aliases in all_names; exact resolution uses entity_names instead.
  -- unicode61 folds accents; a CJK run is one token. Rebuild from entity_search_content.
  preferred, all_names, body, content='entity_search_content', content_rowid='id'
);
CREATE TRIGGER entity_names_fts_insert AFTER INSERT ON entity_names BEGIN
  -- AFTER an actual insert: skipped INSERT/UPSERT must not delete an existing document.
  -- Reconstruct the prior aggregate by excluding this row; the first preferred name had no prior document.
  INSERT INTO entities_fts(entities_fts, rowid, preferred, all_names, body)
  SELECT 'delete', e.id, p.title,
         (SELECT group_concat(title, char(10) ORDER BY name_key) FROM entity_names WHERE entity_id = e.id AND id <> NEW.id), e.body
    FROM entities e JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
   WHERE e.id = NEW.entity_id AND p.id <> NEW.id;
  INSERT INTO entities_fts(rowid, preferred, all_names, body)
  SELECT id, preferred, all_names, body FROM entity_search_content WHERE id = NEW.entity_id;
END;
CREATE TRIGGER entity_names_fts_before_update BEFORE UPDATE OF title ON entity_names WHEN NEW.title IS NOT OLD.title BEGIN
  INSERT INTO entities_fts(entities_fts, rowid, preferred, all_names, body)
  SELECT 'delete', id, preferred, all_names, body FROM entity_search_content WHERE id = OLD.entity_id;
END;
CREATE TRIGGER entity_names_fts_update AFTER UPDATE OF title ON entity_names WHEN NEW.title IS NOT OLD.title BEGIN
  INSERT INTO entities_fts(rowid, preferred, all_names, body)
  SELECT id, preferred, all_names, body FROM entity_search_content WHERE id = NEW.entity_id;
END;
CREATE TRIGGER entities_fts_before_update BEFORE UPDATE OF body, preferred_name_key ON entities
 WHEN NEW.body IS NOT OLD.body OR NEW.preferred_name_key IS NOT OLD.preferred_name_key BEGIN
  INSERT INTO entities_fts(entities_fts, rowid, preferred, all_names, body)
  SELECT 'delete', id, preferred, all_names, body FROM entity_search_content WHERE id = OLD.id;
END;
CREATE TRIGGER entities_fts_update AFTER UPDATE OF body, preferred_name_key ON entities
 WHEN NEW.body IS NOT OLD.body OR NEW.preferred_name_key IS NOT OLD.preferred_name_key BEGIN
  INSERT INTO entities_fts(rowid, preferred, all_names, body)
  SELECT id, preferred, all_names, body FROM entity_search_content WHERE id = NEW.id;
END;

CREATE TABLE people (
  -- people in the owner's life; relationships between them are links (friend, family, parent-of).
  -- A person is also a page with the same id (D20): registry names are the handles [[wikilinks]] write
  -- ('Sam (barber)' tells two Sams apart), its body holds the prose; name is the editable full name.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'person' CONSTRAINT people_entity_type CHECK (entity_type = 'person'),
  name        TEXT NOT NULL,
  birth_day   TEXT CONSTRAINT people_birth_day CHECK (birth_day IS NULL OR length(birth_day) = 10 AND birth_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(birth_day) IS birth_day),
  death_day   TEXT CONSTRAINT people_death_day CHECK (death_day IS NULL OR length(death_day) = 10 AND death_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(death_day) IS death_day),
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),
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
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),
  CONSTRAINT places_not_null_island CHECK (lat <> 0 OR lon <> 0)   -- 0°, 0° is how photo metadata says "no location"
) STRICT;

CREATE TABLE periods (
  -- Named recorded spans. Boundaries follow contract/period-boundaries.md, not exact entities.day.
  -- Unknown is NULL; only an end may be ongoing '..'. Partial precision and qualifiers retain evidence.
  -- Overlap is allowed; comparisons derive ranges, never store dates guessed from partial observations.
  -- Classify with ordinary optional part-of links. No primary kind, duration or membership is stored.
  id INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'period' CONSTRAINT periods_entity_type CHECK (entity_type='period'),
  start_boundary TEXT CONSTRAINT periods_start_boundary CHECK (start_boundary IS NULL
      OR (instr(start_boundary, char(0)) = 0
      AND (((CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END) GLOB '[0-9][0-9][0-9][0-9]'
      OR (CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'
      OR (CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')
      AND date((CASE length((CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END)) WHEN 4
        THEN (CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END)||'-01-01' WHEN 7
        THEN (CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END)||'-01'
        ELSE (CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END) END)) IS (CASE length((CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END)) WHEN 4
        THEN (CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END)||'-01-01' WHEN 7
        THEN (CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END)||'-01'
        ELSE (CASE WHEN substr(start_boundary,-1) IN ('?','~','%')
        THEN substr(start_boundary,1,length(start_boundary)-1)
        ELSE start_boundary END) END)))),
  end_boundary TEXT CONSTRAINT periods_end_boundary CHECK (end_boundary IS NULL
      OR (instr(end_boundary, char(0)) = 0
      AND (end_boundary='..'
      OR (((CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END) GLOB '[0-9][0-9][0-9][0-9]'
      OR (CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'
      OR (CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')
      AND date((CASE length((CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END)) WHEN 4
        THEN (CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END)||'-01-01' WHEN 7
        THEN (CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END)||'-01'
        ELSE (CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END) END)) IS (CASE length((CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END)) WHEN 4
        THEN (CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END)||'-01-01' WHEN 7
        THEN (CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END)||'-01'
        ELSE (CASE WHEN substr(end_boundary,-1) IN ('?','~','%')
        THEN substr(end_boundary,1,length(end_boundary)-1)
        ELSE end_boundary END) END))))),
  CONSTRAINT periods_order CHECK (start_boundary IS NULL OR end_boundary IS NULL OR end_boundary='..'
    OR substr(start_boundary,-1) IN ('?','~','%') OR substr(end_boundary,-1) IN ('?','~','%')
    OR (CASE length(start_boundary) WHEN 4 THEN start_boundary||'-01-01' WHEN 7 THEN start_boundary||'-01' ELSE start_boundary END) <= (CASE length(end_boundary) WHEN 4 THEN end_boundary||'-12-31' WHEN 7 THEN CASE WHEN substr(end_boundary,6,2)='12' THEN end_boundary||'-31' ELSE date(end_boundary||'-01','+1 month','-1 day') END ELSE end_boundary END)),
  FOREIGN KEY(id,entity_type) REFERENCES entities(id,entity_type)
) STRICT;
CREATE TRIGGER periods_fixed BEFORE UPDATE ON periods
 WHEN NEW.id IS NOT OLD.id OR NEW.entity_type IS NOT OLD.entity_type
BEGIN SELECT RAISE(ABORT,'period identity is immutable'); END;
CREATE TRIGGER periods_no_delete BEFORE DELETE ON periods
BEGIN SELECT RAISE(ABORT,'periods are tombstoned through their named identity'); END;
CREATE TRIGGER periods_touch_insert AFTER INSERT ON periods BEGIN
 UPDATE entities SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=NEW.id;
END;
CREATE TRIGGER periods_touch_update AFTER UPDATE OF start_boundary,end_boundary ON periods
 WHEN NEW.start_boundary IS NOT OLD.start_boundary OR NEW.end_boundary IS NOT OLD.end_boundary
BEGIN
 UPDATE entities SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=NEW.id;
END;

CREATE TABLE metrics (
  -- what is measured (D7). A metric is also a page with the same id (D27), as a person is: its title is
  -- its preferred name, one series forever under stable id and retained aliases; its body is what the owner
  -- writes about it, and [[Weight]] in the journal reaches it after renaming. The unit
  -- gives every stored value its meaning, so it never changes (metrics_unit_fixed). Seeded with Mood (D6).
  -- A category is a page: a metric is filed in one by a part-of link from its page (D26).
  -- A habit is NOT a category: a metric is a habit while it has habit_periods (D24).
  -- A metric registered by mistake is tombstoned, never deleted (D11); its readings stay life data.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'metric' CONSTRAINT metrics_entity_type CHECK (entity_type = 'metric'),
  unit        TEXT NOT NULL DEFAULT '',   -- 'kg', 'bpm', 'h'; '' for 1-5 scales and 0/1 habits
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type)
) STRICT;
CREATE TRIGGER metrics_unit_fixed BEFORE UPDATE OF unit ON metrics
  WHEN NEW.unit IS NOT OLD.unit
BEGIN
  -- changing the unit would silently reinterpret the whole series
  SELECT RAISE(ABORT, 'metrics.unit is fixed: it defines what every stored value means; register a new metric instead');
END;
BEGIN IMMEDIATE;
INSERT INTO entities(id, entity_type, preferred_name_key, body, created_at, updated_at, source) VALUES
  (1, 'metric', 'mood', '1-5; attached to its day page via measurements.captured_with_id when posted', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'schema');
INSERT INTO entity_names(entity_id, title, name_key) VALUES (1, 'Mood', 'mood');
COMMIT;
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
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type)
) STRICT;
CREATE UNIQUE INDEX files_sha256 ON files(sha256);   -- one page per original
CREATE TRIGGER files_original_fixed BEFORE UPDATE OF sha256, mime ON files
  WHEN NEW.sha256 IS NOT OLD.sha256 OR NEW.mime IS NOT OLD.mime
BEGIN
  -- the hash and the type name the original; another original is another file
  SELECT RAISE(ABORT, 'files.sha256 and mime name the original and never change: another original is another file');
END;

CREATE TABLE sessions (
 -- Lightweight recorded sessions, not named/graph objects. kind is a plain non-journal page.
 -- UTC and unresolved local endpoints are exclusive; start required, end may be unavailable.
 -- Reporting day is independent attribution. Zone labels are supplied UNVERIFIED claims, never inferred.
 -- Source keys are lossless TEXT, unique per source, not parsed numbers or portable local IDs.
 -- Current editable contents plus snapshots; versioned lifecycle, immutable provenance and no hard deletes.
 id INTEGER PRIMARY KEY,
 kind_id INTEGER NOT NULL,
 kind_entity_type TEXT NOT NULL DEFAULT 'page' CONSTRAINT sessions_kind_entity_type CHECK (kind_entity_type='page'),
 day TEXT NOT NULL CONSTRAINT sessions_day CHECK (length(day)=10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day),
 start_at TEXT CONSTRAINT sessions_start_at CHECK (start_at IS NULL
      OR (length(start_at)=24
      AND start_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z'
      AND substr(start_at,12,2) BETWEEN '00'
      AND '23'
      AND strftime('%Y-%m-%dT%H:%M:%fZ',start_at) IS start_at)),
 start_local TEXT CONSTRAINT sessions_start_local CHECK (start_local IS NULL
      OR (length(start_local)=23
      AND (length((start_local||'Z'))=24
      AND (start_local||'Z') GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z'
      AND substr((start_local||'Z'),12,2) BETWEEN '00'
      AND '23'
      AND strftime('%Y-%m-%dT%H:%M:%fZ',(start_local||'Z')) IS (start_local||'Z')))),
 start_offset TEXT CONSTRAINT sessions_start_offset CHECK (start_offset IS NULL
      OR (instr(start_offset, char(0)) = 0
      AND length(start_offset)=6
      AND start_offset GLOB '[+-][0-9][0-9]:[0-9][0-9]'
      AND substr(start_offset,2,2) BETWEEN '00'
      AND '23'
      AND substr(start_offset,5,2) BETWEEN '00'
      AND '59'
      AND start_offset<>'-00:00'
      AND start_at IS NOT NULL)),
 start_zone_unverified TEXT CONSTRAINT sessions_start_zone_unverified CHECK (start_zone_unverified IS NULL
      OR (instr(start_zone_unverified, char(0)) = 0
      AND length(start_zone_unverified) BETWEEN 1
      AND 64
      AND start_zone_unverified NOT GLOB '*[^A-Za-z0-9_/+-]*'
      AND (start_at IS NOT NULL
      OR start_local IS NOT NULL))),
 end_at TEXT CONSTRAINT sessions_end_at CHECK (end_at IS NULL
      OR (length(end_at)=24
      AND end_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z'
      AND substr(end_at,12,2) BETWEEN '00'
      AND '23'
      AND strftime('%Y-%m-%dT%H:%M:%fZ',end_at) IS end_at)),
 end_local TEXT CONSTRAINT sessions_end_local CHECK (end_local IS NULL
      OR (length(end_local)=23
      AND (length((end_local||'Z'))=24
      AND (end_local||'Z') GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z'
      AND substr((end_local||'Z'),12,2) BETWEEN '00'
      AND '23'
      AND strftime('%Y-%m-%dT%H:%M:%fZ',(end_local||'Z')) IS (end_local||'Z')))),
 end_offset TEXT CONSTRAINT sessions_end_offset CHECK (end_offset IS NULL
      OR (instr(end_offset, char(0)) = 0
      AND length(end_offset)=6
      AND end_offset GLOB '[+-][0-9][0-9]:[0-9][0-9]'
      AND substr(end_offset,2,2) BETWEEN '00'
      AND '23'
      AND substr(end_offset,5,2) BETWEEN '00'
      AND '59'
      AND end_offset<>'-00:00'
      AND end_at IS NOT NULL)),
 end_zone_unverified TEXT CONSTRAINT sessions_end_zone_unverified CHECK (end_zone_unverified IS NULL
      OR (instr(end_zone_unverified, char(0)) = 0
      AND length(end_zone_unverified) BETWEEN 1
      AND 64
      AND end_zone_unverified NOT GLOB '*[^A-Za-z0-9_/+-]*'
      AND (end_at IS NOT NULL
      OR end_local IS NOT NULL))),
 created_at TEXT NOT NULL CONSTRAINT sessions_created_at CHECK (length(created_at)=24
      AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z'
      AND substr(created_at,12,2) BETWEEN '00'
      AND '23'
      AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS created_at),
 updated_at TEXT NOT NULL CONSTRAINT sessions_updated_at CHECK (length(updated_at)=24
      AND updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z'
      AND substr(updated_at,12,2) BETWEEN '00'
      AND '23'
      AND strftime('%Y-%m-%dT%H:%M:%fZ',updated_at) IS updated_at),
 deleted_at TEXT CONSTRAINT sessions_deleted_at CHECK (deleted_at IS NULL
      OR length(deleted_at)=24
      AND deleted_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z'
      AND substr(deleted_at,12,2) BETWEEN '00'
      AND '23'
      AND strftime('%Y-%m-%dT%H:%M:%fZ',deleted_at) IS deleted_at),
 revision INTEGER NOT NULL DEFAULT 1 CONSTRAINT sessions_revision CHECK (revision>=1),
 source TEXT NOT NULL CONSTRAINT sessions_source CHECK (instr(source, char(0)) = 0 AND length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),
 import_key TEXT,
 FOREIGN KEY(kind_id,kind_entity_type) REFERENCES entities(id,entity_type),
 CONSTRAINT sessions_start_basis CHECK ((start_at IS NULL)<>(start_local IS NULL)),
 CONSTRAINT sessions_end_basis CHECK (end_at IS NULL OR end_local IS NULL),
 CONSTRAINT sessions_utc_order CHECK (start_at IS NULL OR end_at IS NULL OR end_at>=start_at)
) STRICT;
CREATE UNIQUE INDEX sessions_import ON sessions(source,import_key) WHERE import_key IS NOT NULL;
CREATE INDEX sessions_day_kind ON sessions(day,kind_id);
CREATE TRIGGER sessions_fixed BEFORE UPDATE ON sessions
 WHEN NEW.id IS NOT OLD.id OR NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key
 OR NEW.created_at IS NOT OLD.created_at OR NEW.kind_entity_type IS NOT OLD.kind_entity_type
BEGIN SELECT RAISE(ABORT,'session identity and provenance are immutable'); END;
CREATE TRIGGER sessions_no_delete BEFORE DELETE ON sessions
BEGIN SELECT RAISE(ABORT,'sessions are tombstoned, never deleted'); END;
CREATE TRIGGER sessions_kind_insert BEFORE INSERT ON sessions BEGIN
 SELECT RAISE(ABORT,'session kind must be a live non-journal plain page') WHERE NOT EXISTS
 (SELECT 1 FROM entities e WHERE e.id=NEW.kind_id AND e.entity_type='page' AND e.deleted_at IS NULL
 AND NOT (length(e.preferred_name_key)=10 AND date(e.preferred_name_key) IS e.preferred_name_key));
END;
CREATE TRIGGER sessions_kind_update BEFORE UPDATE OF kind_id ON sessions WHEN NEW.kind_id IS NOT OLD.kind_id BEGIN
 SELECT RAISE(ABORT,'session kind must be a live non-journal plain page') WHERE NOT EXISTS
 (SELECT 1 FROM entities e WHERE e.id=NEW.kind_id AND e.entity_type='page' AND e.deleted_at IS NULL
 AND NOT (length(e.preferred_name_key)=10 AND date(e.preferred_name_key) IS e.preferred_name_key));
END;
CREATE TRIGGER sessions_touch AFTER UPDATE OF kind_id,day,start_at,start_local,start_offset,start_zone_unverified,end_at,end_local,end_offset,end_zone_unverified,deleted_at ON sessions WHEN NEW.kind_id IS NOT OLD.kind_id
      OR NEW.day IS NOT OLD.day
      OR NEW.start_at IS NOT OLD.start_at
      OR NEW.start_local IS NOT OLD.start_local
      OR NEW.start_offset IS NOT OLD.start_offset
      OR NEW.start_zone_unverified IS NOT OLD.start_zone_unverified
      OR NEW.end_at IS NOT OLD.end_at
      OR NEW.end_local IS NOT OLD.end_local
      OR NEW.end_offset IS NOT OLD.end_offset
      OR NEW.end_zone_unverified IS NOT OLD.end_zone_unverified
      OR NEW.deleted_at IS NOT OLD.deleted_at BEGIN
 UPDATE sessions SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=NEW.id;
END;
CREATE TRIGGER sessions_revision_monotonic BEFORE UPDATE OF revision ON sessions WHEN NEW.revision<OLD.revision
BEGIN SELECT RAISE(ABORT,'session revisions never decrease'); END;

CREATE TABLE measurements (
  -- one row per data point (the FxLifeSheet shape). The table is append-only, enforced by triggers: never
  -- UPDATE or DELETE. A correction is a new row whose supersedes_id names the row it corrects, at most one
  -- per row (chain: correct the correction); a correction with a NULL value RETRACTS the row it corrects.
  -- session_id is explicit scope, never captured_with provenance; corrections retain metric and NULL-safe scope.
  -- Cross-table liveness is lifelog_meta.measurement_scope. Relocation is retract/new-root, not a cross-scope chain.
  -- Read through the view measurement_values. day is when the value was true, created_at when it was written
  -- down, taken_at + tz when and where it was measured (tz: IANA zone; NULL = unknown).
  -- Imports: INSERT ... ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING;
  -- never OR IGNORE (it silently skips CHECK / NOT NULL violations).
  id               INTEGER PRIMARY KEY,
  metric_id        INTEGER NOT NULL REFERENCES metrics(id),
  session_id       INTEGER REFERENCES sessions(id), -- explicit nullable scope; NULL is unassociated, not a daily total
  day              TEXT NOT NULL CONSTRAINT measurements_day CHECK (length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day),  -- local date the value refers to
  taken_at         TEXT CONSTRAINT measurements_taken_at CHECK (taken_at IS NULL OR length(taken_at) = 24 AND taken_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(taken_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', taken_at) IS taken_at),
  tz               TEXT CONSTRAINT measurements_tz CHECK (tz IS NULL OR (length(tz) BETWEEN 1 AND 64 AND tz NOT GLOB '*[^A-Za-z0-9_/+-]*')),
  value            REAL,                        -- numeric only, by design (D7); NULL only on a correction: it RETRACTS the row it supersedes
  created_at       TEXT NOT NULL CONSTRAINT measurements_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
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
CREATE TRIGGER measurements_supersede_scope AFTER INSERT ON measurements WHEN NEW.supersedes_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT,'correction must retain the same session scope')
 WHERE (SELECT session_id FROM measurements WHERE id=NEW.supersedes_id) IS NOT NEW.session_id;
END;
CREATE TRIGGER measurements_session_live BEFORE INSERT ON measurements
 WHEN NEW.value IS NOT NULL AND NEW.session_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT,'new associated value requires a live session')
 WHERE NOT EXISTS (SELECT 1 FROM sessions s WHERE s.id=NEW.session_id AND s.deleted_at IS NULL);
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
  -- The seeded Mood identity (metric id 1) is never a habit: its 1-5 readings have a different meaning.
  id         INTEGER PRIMARY KEY,
  metric_id  INTEGER NOT NULL REFERENCES metrics(id),
  start_day  TEXT NOT NULL CONSTRAINT habit_periods_start_day CHECK (length(start_day) = 10 AND start_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(start_day) IS start_day),
  end_day    TEXT CONSTRAINT habit_periods_end_day CHECK (end_day IS NULL OR length(end_day) = 10 AND end_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(end_day) IS end_day),   -- the last day, inclusive; NULL = still going
  source     TEXT NOT NULL CONSTRAINT habit_periods_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  CONSTRAINT habit_periods_order CHECK (end_day IS NULL OR end_day >= start_day),
  UNIQUE (metric_id, start_day)
) STRICT;
CREATE TRIGGER habit_periods_check_insert BEFORE INSERT ON habit_periods
BEGIN
  SELECT RAISE(ABORT, 'the seeded Mood metric cannot be a habit') WHERE NEW.metric_id = 1;
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
  SELECT RAISE(ABORT, 'the seeded Mood metric cannot be a habit') WHERE NEW.metric_id = 1;
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
  ('wikilink', 0, 'page,person,place,metric,file,period', 'page,person,place,metric,file,period', 'extracted from [[body]] on save (an embed ![[...]] is one); body is the truth'),
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
  -- general context. kind='at' records the places the owner was at; see D16 for the journal capture path.
  -- Rows are hard-deleted (the one such table, D11) and immutable otherwise (delete and
  -- re-insert). Symmetric kinds are mirrored by trigger on insert AND delete, so a half-edge cannot
  -- exist and backlinks need only to_id. A symmetric relationship has one shared note: editing either
  -- direction updates the other atomically; directional kinds keep independent notes. Cycles are not prevented (D8).
  -- source names the writer, as on entities; written at insert, never changed.
  id         INTEGER PRIMARY KEY,
  from_id    INTEGER NOT NULL REFERENCES entities(id),
  to_id      INTEGER NOT NULL REFERENCES entities(id),
  kind       TEXT NOT NULL REFERENCES link_kinds(kind),  -- closed registry
  note       TEXT,
  created_at TEXT NOT NULL CONSTRAINT links_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source     TEXT NOT NULL CONSTRAINT links_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  UNIQUE (from_id, to_id, kind)
) STRICT;
CREATE INDEX links_to ON links(to_id);   -- backlinks query (from_id is served by the UNIQUE index)

CREATE TRIGGER links_fixed BEFORE UPDATE ON links
  WHEN NEW.id IS NOT OLD.id OR NEW.from_id IS NOT OLD.from_id OR NEW.to_id IS NOT OLD.to_id OR NEW.kind IS NOT OLD.kind
    OR NEW.created_at IS NOT OLD.created_at OR NEW.source IS NOT OLD.source
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
CREATE TRIGGER links_mirror_note AFTER UPDATE OF note ON links
  WHEN NEW.note IS NOT OLD.note AND NEW.from_id <> NEW.to_id
   AND (SELECT symmetric FROM link_kinds WHERE kind = NEW.kind) = 1
BEGIN
  UPDATE links SET note = NEW.note
   WHERE from_id = NEW.to_id AND to_id = NEW.from_id AND kind = NEW.kind
     AND note IS NOT NEW.note;
END;
CREATE TRIGGER links_mirror_delete AFTER DELETE ON links
  WHEN OLD.from_id <> OLD.to_id
   AND (SELECT symmetric FROM link_kinds WHERE kind = OLD.kind) = 1
BEGIN
  DELETE FROM links WHERE from_id = OLD.to_id AND to_id = OLD.from_id AND kind = OLD.kind;
END;

CREATE VIEW ghost_pages AS
  -- Empty, unlinked live plain objects older than 30 days; never a typed identity. Owner tombstones them.
  SELECT e.id, n.title, e.created_at
    FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
   WHERE e.entity_type = 'page' AND e.body = '' AND e.deleted_at IS NULL
     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')
     AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = e.id OR l.from_id = e.id);

CREATE TRIGGER entity_names_touch_insert AFTER INSERT ON entity_names BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.entity_id;
END;
CREATE TRIGGER entity_names_touch_update AFTER UPDATE OF title ON entity_names WHEN NEW.title IS NOT OLD.title BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.entity_id;
END;
CREATE TRIGGER people_touch AFTER UPDATE ON people
 WHEN NEW.name IS NOT OLD.name OR NEW.birth_day IS NOT OLD.birth_day OR NEW.death_day IS NOT OLD.death_day
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER places_touch AFTER UPDATE ON places
 WHEN NEW.lat IS NOT OLD.lat OR NEW.lon IS NOT OLD.lon OR NEW.radius_m IS NOT OLD.radius_m OR NEW.link_days IS NOT OLD.link_days
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER files_touch AFTER UPDATE ON files WHEN NEW.preview IS NOT OLD.preview
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER entities_touch AFTER UPDATE OF body, day, preferred_name_key, deleted_at, entity_type ON entities
  WHEN NEW.body IS NOT OLD.body OR NEW.day IS NOT OLD.day OR NEW.preferred_name_key IS NOT OLD.preferred_name_key
    OR NEW.deleted_at IS NOT OLD.deleted_at OR NEW.entity_type IS NOT OLD.entity_type
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER entities_revision_monotonic BEFORE UPDATE OF revision ON entities
 WHEN NEW.revision < OLD.revision
BEGIN SELECT RAISE(ABORT, 'edit revisions never decrease'); END;
CREATE TRIGGER people_touch_insert AFTER INSERT ON people BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER places_touch_insert AFTER INSERT ON places BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER metrics_touch_insert AFTER INSERT ON metrics BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER files_touch_insert AFTER INSERT ON files BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER habit_periods_touch_insert AFTER INSERT ON habit_periods BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.metric_id;
END;
CREATE TRIGGER habit_periods_touch_update AFTER UPDATE OF start_day,end_day,metric_id ON habit_periods
 WHEN NEW.start_day IS NOT OLD.start_day OR NEW.end_day IS NOT OLD.end_day OR NEW.metric_id IS NOT OLD.metric_id
BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id IN (OLD.metric_id,NEW.metric_id);
END;
CREATE TRIGGER links_touch_insert AFTER INSERT ON links BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id IN (NEW.from_id,NEW.to_id);
END;
CREATE TRIGGER links_touch_delete AFTER DELETE ON links BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id IN (OLD.from_id,OLD.to_id);
END;
CREATE TRIGGER links_touch_note AFTER UPDATE OF note ON links WHEN NEW.note IS NOT OLD.note BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id IN (NEW.from_id,NEW.to_id);
END;
CREATE TRIGGER entities_endpoint_types BEFORE UPDATE OF entity_type ON entities
  WHEN NEW.entity_type IS NOT OLD.entity_type
BEGIN
  SELECT RAISE(ABORT, 'type change would invalidate a retained link endpoint')
   WHERE EXISTS (
     SELECT 1 FROM links l JOIN link_kinds k ON k.kind = l.kind
      WHERE (l.from_id = OLD.id AND k.from_types IS NOT NULL
             AND instr(',' || k.from_types || ',', ',' || NEW.entity_type || ',') = 0)
         OR (l.to_id = OLD.id AND k.to_types IS NOT NULL
             AND instr(',' || k.to_types || ',', ',' || NEW.entity_type || ',') = 0));
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
CREATE TRIGGER people_no_delete BEFORE DELETE ON people
BEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER places_no_delete BEFORE DELETE ON places
BEGIN SELECT RAISE(ABORT, 'places are never deleted: fix a wrong point by UPDATE, tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER metrics_no_delete BEFORE DELETE ON metrics
BEGIN SELECT RAISE(ABORT, 'metrics are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER files_no_delete BEFORE DELETE ON files
BEGIN SELECT RAISE(ABORT, 'files are never deleted: tombstone the entity (entities.deleted_at)'); END;
