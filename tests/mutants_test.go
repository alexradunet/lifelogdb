package tests

// A suite that cannot fail proves nothing. Each mutant is the docs tree with one rule broken — in schema.sql, in a
// cookbook block or in the text of a page — and the suite that owns the rule must notice: it must run to its end
// and fail its explicitly named rule witness (stopping on the broken document is not "noticed"). Every suite named
// here must first pass on the unmutated document. A new rule gets a mutant here (tests/README.md keeps the count).

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// change is one broken rule: replace the nth occurrence of old (counted over schema.sql and then the pages in
// reading order) by new; or make one named CHECK always true; or make one trigger never fire.
type change struct {
	kind, a, b string
	nth        int
}

func edit(old, new string) change             { return change{"edit", old, new, 0} }
func editNth(old, new string, nth int) change { return change{"edit", old, new, nth} }
func nocheck(name string) change              { return change{"nocheck", name, "", 0} }
func notrigger(name string) change            { return change{"notrigger", name, "", 0} }

// apply is the files the change breaks, by their path relative to docs/.
func (ch change) apply(d *Docs) (map[string]string, error) {
	files := append([]string{"schema/schema.sql"}, d.Pages()...)
	replace := func(old, new string, nth int) (map[string]string, error) {
		seen := 0
		for _, r := range files {
			t := d.Page(r)
			for i := 0; ; {
				j := strings.Index(t[i:], old)
				if j < 0 {
					break
				}
				if seen == nth {
					at := i + j
					return map[string]string{r: t[:at] + new + t[at+len(old):]}, nil
				}
				seen++
				i += j + len(old)
			}
		}
		return nil, fmt.Errorf("mutation target found %dx: %q", seen, clip(old, 70))
	}
	ddl := d.DDL()
	switch ch.kind {
	case "nocheck": // its condition is always true; the CHECK's parenthesis is found by counting
		m := regexp.MustCompile(`CONSTRAINT ` + ch.a + `\s+CHECK\s*\(`).FindStringIndex(ddl)
		if m == nil {
			return nil, fmt.Errorf("no CHECK %s", ch.a)
		}
		i, depth := m[1], 1
		for ; depth > 0 && i < len(ddl); i++ {
			switch ddl[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		return replace(ddl[m[0]:i], "CONSTRAINT "+ch.a+" CHECK (1)", 0)
	case "notrigger": // its WHEN becomes WHEN 0, or WHEN 0 is added before BEGIN
		m := regexp.MustCompile(`(?s)CREATE TRIGGER ` + ch.a + `\b(.*?)BEGIN`).FindStringSubmatch(ddl)
		if m == nil {
			return nil, fmt.Errorf("no trigger %s", ch.a)
		}
		head := m[1]
		if w := strings.Index(head, "WHEN "); w >= 0 {
			head = head[:w]
		} else {
			head = strings.TrimRight(head, " \t\n") + " "
		}
		return replace(m[0], "CREATE TRIGGER "+ch.a+head+"WHEN 0 BEGIN", 0)
	}
	return replace(ch.a, ch.b, ch.nth)
}

var mutants = []struct {
	suite, name, witness string
	change               change
}{
	{"schema-safeguards", "people identity guard omitted", "people id combined=false identity refusal is atomic", notrigger("people_identity_fixed")},
	{"schema-safeguards", "files identity guard omitted", "files id combined=false identity refusal is atomic", notrigger("files_identity_fixed")},
	{"schema-safeguards", "metrics identity guard omitted", "metrics id combined=false identity refusal is atomic", notrigger("metrics_identity_fixed")},
	{"schema-safeguards", "habit_periods identity guard omitted", "habit_periods id combined=false identity refusal is atomic", notrigger("habit_periods_identity_fixed")},
	{"schema-safeguards", "entity creation time can change", "entity creation time immutable", edit(" OR NEW.created_at IS NOT OLD.created_at\nBEGIN\n  -- provenance", "\nBEGIN\n  -- provenance")},
	{"schema-safeguards", "FTS compound accents retained", "FTS strips compound Latin diacritics", edit("tokenize='unicode61 remove_diacritics 2'", "tokenize='unicode61 remove_diacritics 1'")},
	{"measurement-query-plan", "sessions_kind lookup index omitted", "sessions_kind supports retained reference lookup", edit("CREATE INDEX sessions_kind ON sessions(kind_id);", "")},
	{"measurement-query-plan", "measurements_capture lookup index omitted", "measurements_capture supports retained reference lookup", edit("CREATE INDEX measurements_capture ON measurements(captured_with_id) WHERE captured_with_id IS NOT NULL;", "")},
	{"measurement-query-plan", "measurements_session lookup index omitted", "measurements_session supports retained reference lookup", edit("CREATE INDEX measurements_session ON measurements(session_id,metric_id,day) WHERE session_id IS NOT NULL;", "")},
	{"integrity", "symmetric integrity ignores absent reverse links", "symmetric integrity detects a missing reverse link", edit("WHERE k.symmetric=1 AND (r.id IS NULL OR r.note IS NOT l.note)", "WHERE k.symmetric=1 AND (r.note IS NOT l.note)")},
	{"integrity", "symmetric integrity ignores shared-note disagreement", "symmetric integrity detects unequal shared notes", edit("WHERE k.symmetric=1 AND (r.id IS NULL OR r.note IS NOT l.note)", "WHERE k.symmetric=1 AND (r.id IS NULL)")},
	{"integrity", "correction integrity treats every row as rooted", "correction integrity detects a rootless cycle", edit(" SELECT id FROM measurements WHERE supersedes_id IS NULL\n UNION\n", " SELECT id FROM measurements\n UNION\n")},
	{"integrity", "habit integrity ignores overlapping periods", "habit integrity detects overlapping periods", edit("p.metric_id=h.metric_id AND p.id<>h.id\n", "0 AND p.metric_id=h.metric_id AND p.id<>h.id\n")},
	{"integrity", "habit integrity ignores nonbinary check-ins", "habit integrity detects current nonbinary check-ins", edit("SELECT v.id FROM measurement_values v WHERE v.value NOT IN (0,1)", "SELECT v.id FROM measurement_values v WHERE 0")},
	{"integrity", "journal integrity ignores extra aliases", "journal integrity detects extra journal aliases", edit("AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key)) ORDER BY n.id", "AND 0 AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key)) ORDER BY n.id")},
	{"integrity", "journal integrity ignores a date name on another owner", "journal integrity detects a date name on another owner", edit("(e.entity_type<>'page' OR e.preferred_name_key IS NOT n.name_key OR e.day IS NOT n.name_key OR n.title IS NOT n.name_key)", "(n.title IS NOT n.name_key)")},
	{"document", "in-file summary omits writer conformance dependency", "Q1: the answer says [lifelog create statement conforming writer vectors]", edit("a conforming writer also needs the matching docs contract and vectors", "a reader needs no other specification")},
	{"document", "in-file instants omit commit-order limitation", "Q2: the answer says [utc iso-8601 commit order]", edit("supplied clock timestamps may coincide or regress and do not encode commit order", "timestamps establish chronology")},
	{"document", "in-file deletion summary omits revival limitation", "Q6: the answer says [tombstone links registries revival]", edit("revival clears the tombstone", "restoration clears the tombstone")},
	{"document", "in-file evolution omits preservation boundary", "Q16: the answer says [additive user_version data-preserving explicit columns]", edit("additive means data-preserving", "additive means unrestricted")},
	{"document", "in-file provenance omits last-editor limitation", "Q19: the answer says [written at insert agent last editor]", edit("not the last editor or an audit trail", "the current modifier")},
	{"document", "in-file period summary omits qualifier meaning", "Q30: the answer says [? uncertain ~ approximate % both incomparable as_of]", edit("Qualifiers: ? uncertain, ~ approximate, % both.", "Qualifiers are punctuation.")},
	{"document", "in-file reminder summary reverses repeated-clock policy", "Q31: the answer says [earlier instant gap entire local date unresolved]", edit("A repeated clock uses the earlier instant", "A repeated clock uses the later instant")},
	{"cookbook", "metric series ignores metric tombstones", "active measurement recipes hide tombstoned metrics", edit("JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'", "JOIN entities e ON e.id = me.metric_id\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'")},
	{"cookbook", "mood series ignores metric tombstones", "active measurement recipes hide tombstoned metrics", edit("JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'", "JOIN entities e ON e.id = me.metric_id\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'")},
	{"cookbook", "day measurement read ignores metric tombstones", "active measurement recipes hide tombstoned metrics", edit("FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n    JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL", "FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n    JOIN entities e ON e.id = m.id")},
	{"cookbook", "labeled measurement read ignores metric tombstones", "active measurement recipes hide tombstoned metrics", edit("WHERE :include_deleted=1 OR (e.deleted_at IS NULL AND (me.session_id IS NULL OR s.deleted_at IS NULL))", "WHERE :include_deleted=1 OR (me.session_id IS NULL OR s.deleted_at IS NULL)")},
	{"cookbook", "recorded-time history ignores cutoff", "recorded-time cutoff excludes later recorded facts", edit("WHERE me.created_at <= :as_of", "WHERE 1")},
	{"cookbook", "recorded-time history only follows immediate corrections", "recorded-time cutoff follows eligible descendants across a clock regression", edit("SELECT me.supersedes_id FROM measurements me JOIN overridden o ON me.id = o.id\n  WHERE me.supersedes_id IS NOT NULL", "SELECT me.supersedes_id FROM measurements me JOIN overridden o ON me.id = o.id\n  WHERE 0")},
	{"cookbook", "recorded-time history exposes retractions as readings", "recorded-time cutoff applies retractions at the cutoff", edit("WHERE me.value IS NOT NULL AND NOT EXISTS (SELECT 1 FROM overridden o WHERE o.id = me.id)", "WHERE NOT EXISTS (SELECT 1 FROM overridden o WHERE o.id = me.id)")},
	{"cookbook", "recorded-time cutoff excludes equal timestamps", "recorded-time cutoff resolves equal timestamps by correction ancestry", edit("WHERE me.created_at <= :as_of", "WHERE me.created_at < :as_of")},
	{"habits", "habit daily read ignores invalid binary data", "habit day labels non-binary current check-ins invalid", edit("SELECT m.title,\n       CASE WHEN EXISTS", "SELECT m.title,\n       CASE WHEN 0 AND EXISTS")},
	{"habits", "habit completion drops invalid days", "habit completion counts invalid days separately and reconciles active days", edit("max(value NOT IN (0, 1)) AS invalid", "max(0) AS invalid")},
	{"habits", "day view labels invalid habit data unrecorded", "day view labels non-binary current habit check-ins invalid", edit("SELECT 'habit', NULL, m.title || ': ' ||\n         CASE WHEN EXISTS", "SELECT 'habit', NULL, m.title || ': ' ||\n         CASE WHEN 0 AND EXISTS")},
	{"places", "place matching leaves equal circles unordered", "equal place circles resolve by stable id, including link_days", edit(" ORDER BY radius_m, d2, id\n", " ORDER BY radius_m, d2\n")},
	{"document", "planning metadata key omitted", "Q29: every place named is a lifelog_meta key or a schema object", edit("('planning', 'task project", "('planning-missing', 'task project")},
	{"planning-cookbook", "planning undated slot regenerated", "cookbook undated persisted slot suppresses original virtual deadline", edit("WHERE o.task_id=s.task_id AND o.occurrence_key=s.occurrence_key)", "WHERE o.task_id=s.task_id AND o.occurrence_key=s.occurrence_key AND o.due_day=s.occurrence_key)")},
	{"planning-cookbook", "planning tombstoned task generates virtual slots", "cookbook task tombstone suppresses virtual and persisted work", edit("WHERE t.id=:planning_task_id AND t.deleted_at IS NULL AND t.repeat_unit IS NOT NULL", "WHERE t.id=:planning_task_id AND t.repeat_unit IS NOT NULL")},
	{"planning-boundaries", "planning weekly vector claims wrong date", "calendar every-two-weeks key 2026-10-19", edit("| 2026-10-05,2026-10-19,2026-11-02 |", "| 2026-10-05,2026-10-20,2026-11-02 |")},
	{"planning-boundaries", "planning repeated clock selects later instant", "reminder clock repeated-hour", edit("| 2026-11-01T05:30:00.000Z |", "| 2026-11-01T06:30:00.000Z |")},
	{"planning-boundaries", "planning missing clock rounds to gap end", "reminder clock missing-hour", edit("| 2026-03-08T07:30:00.000Z |", "| 2026-03-08T07:00:00.000Z |")},
	{"planning-boundaries", "planning half-hour gap shifts a full hour", "reminder clock missing-half-hour", edit("| 2026-10-03T15:45:00.000Z |", "| 2026-10-03T16:15:00.000Z |")},
	{"planning-boundaries", "planning skipped date silently shifts to tomorrow", "reminder clock skipped-date", edit("| Pacific/Apia | unresolved |", "| Pacific/Apia | 2011-12-30T19:00:00.000Z |")},
	{"planning", "planning project admission insert omitted", "task project insert refuses journal", notrigger("tasks_project_insert")},
	{"planning", "planning project admission update omitted", "task project update refuses journal", notrigger("tasks_project_update")},
	{"planning", "planning project foreign key loses type", "retained task project prevents typed promotion", edit("FOREIGN KEY(project_page_id,project_entity_type) REFERENCES entities(id,entity_type)", "FOREIGN KEY(project_page_id) REFERENCES entities(id)")},
	{"planning", "planning project ghost retention omitted", "ghost pages exclude retained task projects", edit("     AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.project_page_id = e.id)\n", "")},
	{"planning", "planning once deferred ownership omitted", "one-off definition cannot commit without its once occurrence", edit("once_key TEXT GENERATED ALWAYS AS (CASE WHEN repeat_unit IS NULL THEN 'once' END) VIRTUAL", "once_key TEXT GENERATED ALWAYS AS (NULL) VIRTUAL")},
	{"planning", "planning task identity guard omitted", "tasks rowid identity immutable", notrigger("tasks_fixed")},
	{"planning", "planning occurrence identity guard omitted", "task_occurrences rowid identity immutable", notrigger("task_occurrences_fixed")},
	{"planning", "planning task tombstone guard omitted", "tasks refuses hard delete", notrigger("tasks_no_delete")},
	{"planning", "planning occurrence tombstone guard omitted", "task_occurrences refuses hard delete", notrigger("task_occurrences_no_delete")},
	{"planning", "planning task touch omitted", "tasks content edit advances revision", notrigger("tasks_touch")},
	{"planning", "planning occurrence touch omitted", "task_occurrences content edit advances revision", notrigger("task_occurrences_touch")},
	{"planning", "planning task monotonic revision omitted", "tasks revision cannot decrease", notrigger("tasks_revision_monotonic")},
	{"planning", "planning occurrence monotonic revision omitted", "task_occurrences revision cannot decrease", notrigger("task_occurrences_revision_monotonic")},
	{"planning", "planning end may extend", "ended recurrence cannot extend end", notrigger("tasks_end_shortens")},
	{"planning", "planning ending fails to skip persisted opens", "ending skips persisted open slots after original-key cutoff", notrigger("tasks_end_skip")},
	{"planning", "planning occurrence admission omitted", "new occurrence requires live task", notrigger("task_occurrences_admit")},
	{"planning", "planning ended occurrence may reopen", "ended slot cannot reopen", notrigger("task_occurrences_open")},
	{"planning", "planning task source key not unique", "task source-key uniqueness", edit("CREATE UNIQUE INDEX tasks_import", "CREATE INDEX tasks_import")},
	{"planning", "planning occurrence source key not unique", "occurrence source-key uniqueness", edit("CREATE UNIQUE INDEX task_occurrences_import", "CREATE INDEX task_occurrences_import")},
	{"planning", "planning duplicate slot silently ignored", "occurrence task-key uniqueness", edit("UNIQUE(task_id,occurrence_key),", "UNIQUE(task_id,occurrence_key) ON CONFLICT IGNORE,")},
	{"planning", "planning project semantic check omitted", "planning project semantic query detects journal reference damage", edit("WHERE t.project_page_id IS NOT NULL AND (e.id IS NULL", "WHERE 0 AND (e.id IS NULL")},
	{"planning", "planning once semantic check omitted", "planning one-off semantic query detects missing once occurrence", edit("WHERE t.repeat_unit IS NULL AND (SELECT count(*)", "WHERE 0 AND (SELECT count(*)")},
	{"planning", "planning calendar semantic check omitted", "planning occurrence semantic query detects calendar nonmember", edit("SELECT o.id FROM task_occurrences o WHERE NOT EXISTS", "SELECT o.id FROM task_occurrences o WHERE 0 AND NOT EXISTS")},
	{"planning-boundaries", "planning label check omitted", "tasks_label rejects invalid value", nocheck("tasks_label")},
	{"planning-boundaries", "planning day interval ignored", "calendar every-three-days key 2026-10-06", edit("CAST(julianday(NEW.occurrence_key)-julianday(t.anchor_day) AS INTEGER)%t.repeat_every=0", "CAST(julianday(NEW.occurrence_key)-julianday(t.anchor_day) AS INTEGER)%1=0")},
	{"planning-boundaries", "planning week interval ignored", "calendar every-two-weeks key 2026-10-12", edit("(CAST(julianday(NEW.occurrence_key)-julianday(t.anchor_day) AS INTEGER)/7)%t.repeat_every=0", "(CAST(julianday(NEW.occurrence_key)-julianday(t.anchor_day) AS INTEGER)/7)%1=0")},
	{"planning-boundaries", "planning month interval ignored", "calendar upper-bound-month key 9999-12-30", edit("+ CAST(substr(NEW.occurrence_key,6,2) AS INTEGER)-CAST(substr(t.anchor_day,6,2) AS INTEGER))%t.repeat_every=0", "+ CAST(substr(NEW.occurrence_key,6,2) AS INTEGER)-CAST(substr(t.anchor_day,6,2) AS INTEGER))%1=0")},
	{"planning-boundaries", "planning year interval ignored", "calendar huge-interval key 2027-01-01", edit("(CAST(substr(NEW.occurrence_key,1,4) AS INTEGER)-CAST(substr(t.anchor_day,1,4) AS INTEGER))%t.repeat_every=0", "(CAST(substr(NEW.occurrence_key,1,4) AS INTEGER)-CAST(substr(t.anchor_day,1,4) AS INTEGER))%1=0")},
	{"planning-boundaries", "planning monthly clamp omitted", "calendar month-clamp key 2026-02-28", edit("min(CAST(substr(t.anchor_day,9,2) AS INTEGER)", "max(CAST(substr(t.anchor_day,9,2) AS INTEGER)")},
	{"planning-boundaries", "planning key before anchor admitted", "calendar before-anchor key 2026-09-01", edit("NEW.occurrence_key>=t.anchor_day", "1")},
	{"planning-boundaries", "planning project discriminator check omitted", "tasks_project_entity_type rejects invalid value", nocheck("tasks_project_entity_type")},
	{"planning-boundaries", "planning repeat unit check omitted", "tasks_repeat_unit rejects invalid value", nocheck("tasks_repeat_unit")},
	{"planning-boundaries", "planning repeat interval check omitted", "tasks_repeat_every rejects invalid value", nocheck("tasks_repeat_every")},
	{"planning-boundaries", "planning anchor day check omitted", "tasks_anchor_day rejects invalid value", nocheck("tasks_anchor_day")},
	{"planning-boundaries", "planning until day check omitted", "tasks_repeat_until_day rejects invalid value", nocheck("tasks_repeat_until_day")},
	{"planning-boundaries", "planning recurrence pair check omitted", "tasks_recurrence rejects invalid value", nocheck("tasks_recurrence")},
	{"planning-boundaries", "planning reminder clock check omitted", "tasks_reminder_local_time rejects invalid value", nocheck("tasks_reminder_local_time")},
	{"planning-boundaries", "planning reminder zone check omitted", "tasks_reminder_zone rejects invalid value", nocheck("tasks_reminder_zone")},
	{"planning-boundaries", "planning reminder default pair check omitted", "tasks_reminder_pair rejects invalid value", nocheck("tasks_reminder_pair")},
	{"planning-boundaries", "planning task revision check omitted", "tasks_revision rejects invalid value", nocheck("tasks_revision")},
	{"planning-boundaries", "planning task source check omitted", "tasks_source rejects invalid value", nocheck("tasks_source")},
	{"planning-boundaries", "planning occurrence key check omitted", "task_occurrences_key rejects invalid value", nocheck("task_occurrences_key")},
	{"planning-boundaries", "planning due day check omitted", "task_occurrences_due_day rejects invalid value", nocheck("task_occurrences_due_day")},
	{"planning-boundaries", "planning state check omitted", "task_occurrences_state rejects invalid value", nocheck("task_occurrences_state")},
	{"planning-boundaries", "planning completion pair check omitted", "task_occurrences_completion rejects invalid value", nocheck("task_occurrences_completion")},
	{"planning-boundaries", "planning reminder mode check omitted", "task_occurrences_reminder_mode rejects invalid value", nocheck("task_occurrences_reminder_mode")},
	{"planning-boundaries", "planning reminder override pair check omitted", "task_occurrences_reminder_pair rejects invalid value", nocheck("task_occurrences_reminder_pair")},
	{"planning-boundaries", "planning occurrence revision check omitted", "task_occurrences_revision rejects invalid value", nocheck("task_occurrences_revision")},
	{"planning-boundaries", "planning occurrence source check omitted", "task_occurrences_source rejects invalid value", nocheck("task_occurrences_source")},
	{"planning-boundaries", "planning task created instant check omitted", "exact tasks.created_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("tasks_created_at")},
	{"planning-boundaries", "planning task updated instant check omitted", "exact tasks.updated_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("tasks_updated_at")},
	{"planning-boundaries", "planning task tombstone instant check omitted", "exact tasks.deleted_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("tasks_deleted_at")},
	{"planning-boundaries", "planning occurrence created instant check omitted", "exact task_occurrences.created_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("task_occurrences_created_at")},
	{"planning-boundaries", "planning occurrence updated instant check omitted", "exact task_occurrences.updated_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("task_occurrences_updated_at")},
	{"planning-boundaries", "planning occurrence tombstone instant check omitted", "exact task_occurrences.deleted_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("task_occurrences_deleted_at")},
	{"planning-boundaries", "planning completion instant check omitted", "exact task_occurrences.completed_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("task_occurrences_completed_at")},
	{"planning-boundaries", "planning override instant check omitted", "exact task_occurrences.reminder_override_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("task_occurrences_reminder_override_at")},
	{"planning-boundaries", "planning task label NUL check omitted", "tasks_label rejects embedded NUL", edit("instr(label,char(0))=0 AND ", "")},
	{"planning-boundaries", "planning local clock NUL check omitted", "tasks_reminder_local_time rejects embedded NUL", edit("instr(reminder_local_time,char(0))=0 AND ", "")},
	{"planning-boundaries", "planning zone NUL check omitted", "tasks_reminder_zone rejects embedded NUL", edit("instr(reminder_zone,char(0))=0 AND ", "")},
	{"planning-boundaries", "planning task source NUL check omitted", "tasks_source rejects embedded NUL", edit("CONSTRAINT tasks_source CHECK (instr(source,char(0))=0 AND ", "CONSTRAINT tasks_source CHECK (")},
	{"planning-boundaries", "planning occurrence source NUL check omitted", "task_occurrences_source rejects embedded NUL", edit("CONSTRAINT task_occurrences_source CHECK (instr(source,char(0))=0 AND ", "CONSTRAINT task_occurrences_source CHECK (")},
	{"schema-safeguards", "entities_source NUL guard omitted", "entities_source rejects embedded NUL", edit("CONSTRAINT entities_source CHECK (instr(source, char(0)) = 0 AND ", "CONSTRAINT entities_source CHECK (")},
	{"schema-safeguards", "entity_names_key_folded NUL guard omitted", "entity_names_key_folded rejects embedded NUL", edit("CONSTRAINT entity_names_key_folded CHECK (instr(name_key, char(0)) = 0 AND ", "CONSTRAINT entity_names_key_folded CHECK (")},
	{"schema-safeguards", "files_sha256 NUL guard omitted", "files_sha256 rejects embedded NUL", edit("CONSTRAINT files_sha256 CHECK (instr(sha256, char(0)) = 0 AND ", "CONSTRAINT files_sha256 CHECK (")},
	{"schema-safeguards", "files_mime NUL guard omitted", "files_mime rejects embedded NUL", edit("CONSTRAINT files_mime CHECK (instr(mime, char(0)) = 0 AND ", "CONSTRAINT files_mime CHECK (")},
	{"schema-safeguards", "measurements_tz NUL guard omitted", "measurements_tz rejects embedded NUL", edit("CONSTRAINT measurements_tz CHECK (tz IS NULL OR (instr(tz, char(0)) = 0 AND ", "CONSTRAINT measurements_tz CHECK (tz IS NULL OR (")},
	{"schema-safeguards", "measurements_source NUL guard omitted", "measurements_source rejects embedded NUL", edit("CONSTRAINT measurements_source CHECK (instr(source, char(0)) = 0 AND ", "CONSTRAINT measurements_source CHECK (")},
	{"schema-safeguards", "habit_periods_source NUL guard omitted", "habit_periods_source rejects embedded NUL", edit("CONSTRAINT habit_periods_source CHECK (instr(source, char(0)) = 0 AND ", "CONSTRAINT habit_periods_source CHECK (")},
	{"schema-safeguards", "links_source NUL guard omitted", "links_source rejects embedded NUL", edit("CONSTRAINT links_source CHECK (instr(source, char(0)) = 0 AND ", "CONSTRAINT links_source CHECK (")},
	{"schema-safeguards", "link_kinds_kind NUL guard omitted", "link_kinds_kind rejects embedded NUL", edit("CONSTRAINT link_kinds_kind CHECK (instr(kind, char(0)) = 0 AND ", "CONSTRAINT link_kinds_kind CHECK (")},
	{"schema-safeguards", "link_kinds_from_types NUL guard omitted", "link_kinds_from_types rejects embedded NUL", edit("CONSTRAINT link_kinds_from_types CHECK (from_types IS NULL OR (instr(from_types, char(0)) = 0 AND ", "CONSTRAINT link_kinds_from_types CHECK (from_types IS NULL OR (")},
	{"schema-safeguards", "link_kinds_to_types NUL guard omitted", "link_kinds_to_types rejects embedded NUL", edit("CONSTRAINT link_kinds_to_types CHECK (to_types   IS NULL OR (instr(to_types, char(0)) = 0 AND ", "CONSTRAINT link_kinds_to_types CHECK (to_types   IS NULL OR (")},
	{"schema-safeguards", "place point ownership guard omitted", "place rowid combined=true ownership refusal is atomic", notrigger("places_fixed")},
	{"schema-safeguards", "ghost view ignores session references", "ghost pages exclude retained session kinds", edit("     AND NOT EXISTS (SELECT 1 FROM sessions s WHERE s.kind_id = e.id)\n", "")},
	{"schema-safeguards", "ghost view ignores measurement references", "ghost pages exclude measurement capture provenance", edit("     AND NOT EXISTS (SELECT 1 FROM measurements m WHERE m.captured_with_id = e.id)", "")},
	{"recorded-periods", "period tombstone selection claimed legal", "period category selection vector 1", edit(`{"deleted":true,"accepted":false}`, `{"deleted":true,"accepted":true}`)},
	{"recorded-periods", "NUL start_boundary guard omitted", "period raw insert vector 1", edit("instr(start_boundary, char(0)) = 0", "1")},
	{"recorded-periods", "NUL end_boundary guard omitted", "period raw insert vector 7", edit("instr(end_boundary, char(0)) = 0", "1")},
	{"recorded-sessions", "NUL start_offset guard omitted", "session endpoint vector 22", edit("instr(start_offset, char(0)) = 0", "1")},
	{"recorded-sessions", "NUL end_offset guard omitted", "session endpoint vector 23", edit("instr(end_offset, char(0)) = 0", "1")},
	{"recorded-sessions", "NUL start_zone_unverified guard omitted", "session endpoint vector 24", edit("instr(start_zone_unverified, char(0)) = 0", "1")},
	{"recorded-sessions", "NUL end_zone_unverified guard omitted", "session endpoint vector 25", edit("instr(end_zone_unverified, char(0)) = 0", "1")},
	{"recorded-sessions", "NUL source guard omitted", "session endpoint vector 26", edit("CONSTRAINT sessions_source CHECK (instr(source, char(0)) = 0", "CONSTRAINT sessions_source CHECK (1")},
	{"recorded-sessions", "session-kind semantic check omitted", "session kind semantic query detects journal damage", edit("(length(e.preferred_name_key)=10 AND date(e.preferred_name_key) IS e.preferred_name_key) ORDER BY s.id;", "(0) ORDER BY s.id;")},
	{"measurement-scopes", "scope semantic check omitted", "scope semantic query detects correction damage", edit("(p.id IS NULL OR p.metric_id IS NOT m.metric_id OR p.session_id IS NOT m.session_id)", "(0)")},
	{"dates", "session created_at calendar check absent", "exact sessions.created_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("sessions_created_at")},
	{"dates", "session updated_at calendar check absent", "exact sessions.updated_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("sessions_updated_at")},
	{"dates", "session deleted_at calendar check absent", "exact sessions.deleted_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("sessions_deleted_at")},
	{"dates", "session start_at calendar check absent", "exact sessions.start_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("sessions_start_at")},
	{"dates", "session end_at calendar check absent", "exact sessions.end_at \"2026-01-01T24:00:00.000Z\" accepted=false", nocheck("sessions_end_at")},
	{"dates", "session reporting-day check absent", "exact sessions.day \"2026-02-31\" accepted=false", nocheck("sessions_day")},
	{"recorded-sessions", "session start_local check absent", "session endpoint vector 16", nocheck("sessions_start_local")},
	{"recorded-sessions", "session end_local check absent", "session endpoint vector 21", nocheck("sessions_end_local")},
	{"recorded-sessions", "session start_offset check absent", "session endpoint vector 13", nocheck("sessions_start_offset")},
	{"recorded-sessions", "session end_offset check absent", "session endpoint vector 20", nocheck("sessions_end_offset")},
	{"recorded-sessions", "session start_zone_unverified check absent", "session endpoint vector 19", nocheck("sessions_start_zone_unverified")},
	{"recorded-sessions", "session end_zone_unverified check absent", "session endpoint vector 15", nocheck("sessions_end_zone_unverified")},
	{"recorded-sessions", "session start_basis check absent", "session endpoint vector 7", nocheck("sessions_start_basis")},
	{"recorded-sessions", "session end_basis check absent", "session endpoint vector 18", nocheck("sessions_end_basis")},
	{"recorded-sessions", "session utc_order check absent", "session endpoint vector 3", nocheck("sessions_utc_order")},
	{"recorded-sessions", "session kind_entity_type check absent", "session kind discriminator requires page", nocheck("sessions_kind_entity_type")},
	{"recorded-sessions", "session revision check absent", "session revision must be positive", nocheck("sessions_revision")},
	{"recorded-sessions", "session source check absent", "session source syntax enforced", nocheck("sessions_source")},
	{"recorded-sessions", "session fixed protection absent", "session rowid immutable", notrigger("sessions_fixed")},
	{"recorded-sessions", "session no_delete protection absent", "session cannot be deleted", notrigger("sessions_no_delete")},
	{"recorded-sessions", "session kind_insert protection absent", "session kind refuses journal page", notrigger("sessions_kind_insert")},
	{"recorded-sessions", "session kind_update protection absent", "session edit refuses journal kind", notrigger("sessions_kind_update")},
	{"recorded-sessions", "session touch protection absent", "session metadata edit advances revision", notrigger("sessions_touch")},
	{"recorded-sessions", "session revision_monotonic protection absent", "session revision cannot decrease", notrigger("sessions_revision_monotonic")},
	{"recorded-sessions", "session kind FK downgraded", "session kind FK checks structural type", edit("FOREIGN KEY(kind_id,kind_entity_type) REFERENCES entities(id,entity_type)", "FOREIGN KEY(kind_id) REFERENCES entities(id)")},
	{"recorded-sessions", "session import uniqueness removed", "session duplicate source key refuses", edit("CREATE UNIQUE INDEX sessions_import", "CREATE INDEX sessions_import")},
	{"measurement-scopes", "correction may change scope", "measurement correction refuses changed session scope", notrigger("measurements_supersede_scope")},
	{"measurement-scopes", "associated values may target tombstone", "new scoped value requires live session", notrigger("measurements_session_live")},
	{"recorded-periods", "period discriminator omitted", "period discriminator refuses another valid owner type", nocheck("periods_entity_type")},
	{"recorded-periods", "period typed owner downgraded", "period detail requires typed owner", edit("FOREIGN KEY(id,entity_type) REFERENCES entities(id,entity_type)", "FOREIGN KEY(id) REFERENCES entities(id)")},
	{"recorded-periods", "period start calendar constraint absent", "period refuses invalid start", nocheck("periods_start_boundary")},
	{"recorded-periods", "period end calendar constraint absent", "period refuses invalid end", nocheck("periods_end_boundary")},
	{"recorded-periods", "period reversal constraint absent", "period refuses reversed span", nocheck("periods_order")},
	{"recorded-periods", "period id aliases mutable", "period rowid immutable", notrigger("periods_fixed")},
	{"recorded-periods", "period extension delete allowed", "period cannot be deleted", notrigger("periods_no_delete")},
	{"recorded-periods", "period insertion preserves stale revision", "period detail insertion advances revision", notrigger("periods_touch_insert")},
	{"recorded-periods", "period update preserves stale revision", "period boundary edit advances revision", notrigger("periods_touch_update")},
	{"recorded-periods", "horizon treated as observed end", "period membership vector 3", edit(`"as_of":"2018-09-15","result":"possible"`, `"as_of":"2018-09-15","result":"definite"`)},
	{"id-guards", "links ignore rowid alias updates", "links rowid combined=true identity refusal is atomic", edit("CREATE TRIGGER links_fixed BEFORE UPDATE ON links", "CREATE TRIGGER links_fixed BEFORE UPDATE OF id, from_id, to_id, kind, created_at, source ON links")},
	{"id-guards", "registry ignores rowid alias updates", "entity_names rowid combined=true identity refusal is atomic", edit("CREATE TRIGGER entity_names_fixed BEFORE UPDATE ON entity_names", "CREATE TRIGGER entity_names_fixed BEFORE UPDATE OF id, entity_id, name_key ON entity_names")},
	{"identity", "alias insertion preserves stale revision", "owned alias insertion advances revision", notrigger("entity_names_touch_insert")},
	{"identity", "equivalent spelling preserves stale revision", "equivalent owned spelling edit advances revision", notrigger("entity_names_touch_update")},
	{"identity", "owned preferred selection preserves stale revision", "already-owned preferred selection advances revision", edit(" OR NEW.preferred_name_key IS NOT OLD.preferred_name_key\n    OR NEW.deleted_at", "\n    OR NEW.deleted_at")},
	{"name-grammar", "registry permits opening brackets", "registry rejects bracket spelling 0", edit("AND instr(title, '[') = 0 ", "")},
	{"name-grammar", "registry permits closing brackets", "registry rejects bracket spelling 1", edit("AND instr(title, ']') = 0", "")},
	{"name-grammar", "NFC syntax pair is addressable", "reference-name predicate agrees with a`b`c", edit("{\"name\":\"a\\u1fefb\\u1fefc\",\"accepted\":false}", "{\"name\":\"a\\u1fefb\\u1fefc\",\"accepted\":true}")},
	{"name-grammar", "entity escape spelling is addressable", "reference-name predicate agrees with R&amp;D", edit("{\"name\":\"R&amp;D\",\"accepted\":false}", "{\"name\":\"R&amp;D\",\"accepted\":true}")},
	{"name-grammar", "intraword underscores are not addressable", "reference-name predicate agrees with a_b", edit("{\"name\":\"a_b\",\"key\":\"a_b\",\"accepted\":true}", "{\"name\":\"a_b\",\"key\":\"a_b\",\"accepted\":false}")},
	{"name-grammar", "NFD names retain a noncanonical key", "raw and NFC reference forms preserve key for Café", edit("{\"name\":\"Cafe\\u0301\",\"key\":\"café\",\"accepted\":true}", "{\"name\":\"Cafe\\u0301\",\"key\":\"cafe\\u0301\",\"accepted\":true}")},
	{"named", "person extensions ignore owner type", "a person extension requires a person owner", edit("FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),\n  CONSTRAINT people_death_day_order", "FOREIGN KEY (id) REFERENCES entities(id),\n  CONSTRAINT people_death_day_order")},
	{"names", "alias index is updated before insertion", "name insertion indexes an alias on the existing document", edit("CREATE TRIGGER entity_names_fts_insert AFTER INSERT ON entity_names BEGIN", "CREATE TRIGGER entity_names_fts_insert BEFORE INSERT ON entity_names BEGIN")},
	{"names", "journal preferred identity may change", "journal selection refuses a deliberately damaged owned alias", notrigger("entities_day_identity")},

	{"name-ownership", "preferred owner may be NULL", "preferred ownership is mandatory", edit("preferred_name_key TEXT NOT NULL,", "preferred_name_key TEXT,")},
	{"name-ownership", "preferred ownership is immediate", "entity insertion defers actual owned-name completion", edit("REFERENCES entity_names(entity_id, name_key) DEFERRABLE INITIALLY DEFERRED", "REFERENCES entity_names(entity_id, name_key)")},
	{"name-ownership", "names may reference an absent entity", "a name cannot refer to an absent owner", edit("entity_id   INTEGER NOT NULL REFERENCES entities(id),", "entity_id   INTEGER NOT NULL,")},
	{"names", "registry ids may change", "names own immutable registry ids", edit("NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key", "NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key")},
	{"names", "owned aliases may transfer", "names cannot transfer owners", edit("NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key", "NEW.id IS NOT OLD.id OR NEW.name_key IS NOT OLD.name_key")},
	{"names", "owned keys may change", "names cannot change normalized keys", edit("NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key", "NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id")},
	{"names", "aliases may be deleted", "owned names cannot be deleted", notrigger("entity_names_no_delete")},
	{"names", "preferred name may belong to another owner", "an unowned preferred selection refuses", edit("FOREIGN KEY (id, preferred_name_key) REFERENCES entity_names(entity_id, name_key) DEFERRABLE INITIALLY DEFERRED", "FOREIGN KEY (preferred_name_key) REFERENCES entity_names(name_key) DEFERRABLE INITIALLY DEFERRED")},
	{"names", "alias insert is not indexed", "name insertion indexes an alias on the existing document", notrigger("entity_names_fts_insert")},
	{"names", "alias insert deletes the wrong aggregate", "name insertion preserves the original indexed content", edit("WHERE entity_id = e.id AND id <> NEW.id", "WHERE entity_id = e.id")},
	{"names", "preferred selection keeps old index", "preferred selection reindexes without losing old aliases", notrigger("entities_fts_before_update")},
	{"names", "preferred selection omits new index", "preferred selection reindexes without losing old aliases", notrigger("entities_fts_update")},
	{"names", "spelling keeps old tokens", "case-equivalent spelling update keeps FTS consistent", notrigger("entity_names_fts_before_update")},
	{"names", "spelling omits new tokens", "case-equivalent spelling update keeps FTS consistent", notrigger("entity_names_fts_update")},
	{"names", "journal accepts aliases", "journal ownership permits no extra names", notrigger("entity_names_day_insert")},
	{"names", "journal spelling is editable", "journal spelling is fixed", notrigger("entity_names_day_update")},
	{"names", "name ownership is optional", "an unowned preferred selection refuses", edit("FOREIGN KEY (id, preferred_name_key) REFERENCES entity_names(entity_id, name_key) DEFERRABLE INITIALLY DEFERRED", "CHECK (1)")},
	{"dates", "exact days accept a negative year", "exact entities.day \"-0001-01-01\" accepted=false", edit("length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND ", "")},
	{"dates", "exact instant representation check absent", "exact entities.created_at \"-001-01-01T00:00:00.000Z\" accepted=false", nocheck("entities_created_at")},
	{"dates", "exact instants accept hour 24", "exact entities.created_at \"2026-01-01T24:59:59.000Z\" accepted=false", edit("substr(created_at,12,2) BETWEEN '00' AND '23' AND ", "")},
	{"integrity", "semantic integrity ignores invalid typed destinations", "typed-edge semantic query detects deliberate endpoint damage", edit("OR (k.to_types IS NOT NULL AND instr(',' || k.to_types || ',', ',' || t.entity_type || ',')=0)", "OR (0)")},

	{"links", "link ID can change", "links are immutable: id", edit("WHEN NEW.id IS NOT OLD.id OR NEW.from_id", "WHEN NEW.from_id")},
	{"links", "link creation time can change", "links are immutable: created_at", edit("OR NEW.created_at IS NOT OLD.created_at OR NEW.source", "OR NEW.source")},
	{"links", "symmetric notes do not mirror", "symmetric note edit mirrors forward", notrigger("links_mirror_note")},
	{"evolution", "type changes strand retained typed links", "type change refuses a retained incoming typed edge", notrigger("entities_endpoint_types")},
	{"habits", "Mood accepts habit insertion", "Mood identity cannot start a habit", edit("WHERE NEW.metric_id = 1;", "WHERE 0;")},
	{"habits", "habit can be reassigned to Mood", "habit update cannot reassign to Mood identity", editNth("WHERE NEW.metric_id = 1;", "WHERE 0;", 1)},
	{"identity", "revision can start at zero", "zero revision refused", nocheck("entities_revision")},
	{"identity", "revision can decrease", "revision decrease refused", notrigger("entities_revision_monotonic")},
	{"identity", "body edits keep the revision", "body changes advance revision", notrigger("entities_touch")},
	{"identity", "lifecycle edits keep the revision", "lifecycle changes advance revision", notrigger("entities_touch")},
	{"identity", "person edits keep the revision", "person details advance revision", notrigger("people_touch")},
	{"identity", "place edits keep the revision", "place details advance revision", notrigger("places_touch")},
	{"identity", "file edits keep the revision", "file details advance revision", notrigger("files_touch")},
	{"identity", "person insertion keeps revision", "person detail insertion advances revision", notrigger("people_touch_insert")},
	{"identity", "metric insertion keeps revision", "metric detail insertion advances revision", notrigger("metrics_touch_insert")},
	{"identity", "file insertion keeps revision", "file detail insertion advances revision", notrigger("files_touch_insert")},
	{"identity", "place insertion keeps revision", "place detail insertion advances revision", notrigger("places_touch_insert")},
	{"identity", "habit insertion keeps revision", "habit insertion advances revision", notrigger("habit_periods_touch_insert")},
	{"identity", "habit edits keep revision", "habit edits advance revision", notrigger("habit_periods_touch_update")},
	{"identity", "link insertion keeps revision", "link insertion advances revision", notrigger("links_touch_insert")},
	{"identity", "link deletion keeps revision", "link deletion advances revision", notrigger("links_touch_delete")},
	{"identity", "link notes keep revision", "link note edit advances revision", notrigger("links_touch_note")},

	{"dates", "a day CHECK uses = instead of IS", "entities.day rejects \"2026-13-01\"", edit("CONSTRAINT entities_day CHECK (day IS NULL OR length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day)", "CONSTRAINT entities_day CHECK (day IS NULL OR length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) = day)")},
	{"dates", "instants lose their milliseconds", "measurements.created_at accepts a UTC instant with milliseconds", edit("CONSTRAINT measurements_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at)", "CONSTRAINT measurements_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%SZ', created_at) IS created_at)")},
	{"identity", "cookbook/capture attaches the mood with last_insert_rowid()", "cookbook/capture run literally, with a link sync inside: the mood reading points at the day page the RETURNING gave", edit("SELECT e.id, '2026-09-29', 4, 'ui', :page_id, strftime", "SELECT e.id, '2026-09-29', 4, 'ui', last_insert_rowid(), strftime")},
	{"identity", "entities.source may be NULL", "entities.source is TEXT NOT NULL with no default", edit("source      TEXT NOT NULL CONSTRAINT entities_source", "source      TEXT CONSTRAINT entities_source")},
	{"identity", "entities.source can change", "entities.source cannot change", edit("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.import_key IS NOT OLD.import_key")},
	{"identity", "entities.import_key can change", "entities.import_key cannot change", edit("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.source IS NOT OLD.source")},
	{"identity", "an entity may be imported twice", "a second entity with the same (source, import_key) is refused", edit("CREATE UNIQUE INDEX entities_import", "CREATE INDEX entities_import")},
	{"identity", "cookbook/import-a-row-once revives a tombstoned import", "cookbook/import-a-row-once: a tombstoned import is neither updated nor inserted again", edit("WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);", "WHERE source = 'import:vault' AND import_key = :import_key);")},
	{"identity", "the mirror drops the source", "the mirror of a symmetric link copies its source", edit("VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NEW.source);", "VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, 'ui');")},
	{"identity", "people may be hard-deleted", "DELETE FROM people is refused", edit("CREATE TRIGGER people_no_delete BEFORE DELETE ON people\nBEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;\n", "")},
	{"identity", "editing a page does not bump updated_at", "updating a page bumps entities.updated_at", notrigger("entities_touch")},
	{"identity", "the tombstone does not bump updated_at", "a tombstone bumps updated_at to the tombstone instant, and the trigger does not re-fire itself", notrigger("entities_touch")},
	{"links", "located-in accepts any endpoint", "link located-in place->person: ERR", edit("  ('located-in', 0, 'place',   'place',", "  ('located-in', 0, NULL,      NULL,")},
	{"named", "a day page may be promoted", "a day page cannot become a person or a place: the cascade meets entities_day_page_plain", nocheck("entities_day_page_plain")},
	{"named", "ghost_pages lists the page of a person", "ghost_pages lists the real ghost and none of the person or place pages, and not a page a rename points at", edit("WHERE e.entity_type = 'page' AND e.body = ''", "WHERE e.body = ''")},
	{"named", "a wikilink cannot land on a person", "the day page links to the person's own id, and no new page was made", edit("('wikilink', 0, 'page,person,place,metric,file,period', 'page,person,place,metric,file,period',", "('wikilink', 0, 'page,person,place,metric,file,period', 'page',")},
	{"named", "a page may claim an unknown type", "entities.entity_type is checked without foreign keys", nocheck("entities_entity_type")},
	{"pages", "the title CHECK lets the zero-width space through", "U+200B in a title is rejected by the DB and by the writer", edit("|| char(8203) || char(8206)", "|| char(8206)")},
	{"pages", "a device name before an extension is allowed", "unsafe or device title \"con.txt\" rejected", edit("upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)", "upper(title)")},
	{"pages", "the title index is not unique", "DIET without a day collides with Diet with a day", edit("name_key    TEXT NOT NULL UNIQUE,", "name_key    TEXT NOT NULL,")},
	{"pages", "a page may have no title", "a page with a key but no title rejected", edit("title       TEXT NOT NULL,              -- preferred spelling", "title       TEXT,                       -- preferred spelling")},
	{"pages", "pages_fts_update re-indexes on every update", "a new day touches the entity revision only, not the derived index", edit("CREATE TRIGGER entities_fts_update AFTER UPDATE OF body, preferred_name_key ON entities\n WHEN NEW.body IS NOT OLD.body OR NEW.preferred_name_key IS NOT OLD.preferred_name_key BEGIN", "CREATE TRIGGER entities_fts_update AFTER UPDATE OF body, day, preferred_name_key ON entities BEGIN")},
	{"pages", "the cookbook/save-a-body resolve scans by lower(title)", "...and is an indexed SEARCH on the registry", edit("WHERE n.name_key = :key;", "WHERE lower(n.title) = :key;")},
	{"links", "friend is not symmetric", "a symmetric kind is stored in both directions", edit("  ('friend',   1, 'person',", "  ('friend',   0, 'person',")},
	{"links", "at accepts any endpoint", "link at person->place (at comes from a page): ERR", edit("  ('at',       0, 'page',      'place',", "  ('at',       0, NULL,        NULL,")},
	{"links", "link kinds may change structure", "link_kinds: symmetric is fixed (by the trigger: located-in could be symmetric by its CHECK)", edit("  WHEN NEW.symmetric IS NOT OLD.symmetric OR NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types\n", "  WHEN 0\n")},
	{"journal", "cookbook/where-was-i lists the places of a tombstoned day", "cookbook/where-was-i where was I on a tombstoned day: nowhere", editNth("WHERE d.preferred_name_key = :day AND d.day = :day AND d.deleted_at IS NULL", "WHERE d.preferred_name_key = :day AND d.day = :day", 1)},
	{"links", "cookbook/inside-a-place lists a tombstoned place", "cookbook/inside-a-place asked about a tombstoned place itself lists nothing", edit("JOIN entities epl ON epl.id = inside.id AND epl.deleted_at IS NULL", "JOIN entities epl ON epl.id = inside.id")},
	{"links", "cookbook/inside-a-place walks with UNION ALL", "cookbook/inside-a-place terminates on a cycle (UNION)", edit("  SELECT :place_id\n  UNION\n", "  SELECT :place_id\n  UNION ALL\n")},
	{"facts", "a correction may be of another metric", "a correction of another metric is refused", edit("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE 0;")},
	{"facts", "measurement values may be infinite", "+Infinity refused", edit("CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)", "CHECK (value IS NULL OR value = value)")},
	{"facts", "measurements may be updated", "UPDATE of a value refused", edit("CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements\nBEGIN\n", "CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements WHEN 0\nBEGIN\n")},
	{"journal", "cookbook/where-was-i says I was at a tombstoned place", "...and a tombstoned place is not where I was", edit("JOIN entities e ON e.id = l.to_id AND e.deleted_at IS NULL\n  JOIN entity_names pl", "JOIN entities e ON e.id = l.to_id\n  JOIN entity_names pl")},
	{"journal", "a day page may have no day", "journal day cannot be cleared", nocheck("entities_day_page")},
	{"journal", "pages_day_page compares the day with = instead of IS", "journal day cannot be cleared", edit("OR day IS preferred_name_key)", "OR day = preferred_name_key)")},
	{"journal", "cookbook/capture looks for the day page under another key", "cookbook/capture finds the day page by its key, the day itself", edit("WHERE n.name_key = '2026-09-29';", "WHERE n.name_key = 'today';")},
	{"journal", "cookbook/days-that-name lists a tombstoned day", "...and drops a tombstoned day", edit("JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day AND d.deleted_at IS NULL\n WHERE l.to_id = :entity_id", "JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day\n WHERE l.to_id = :entity_id")},
	{"journal", "cookbook/days-that-name forgets the about links", "...also a day that names her without brackets, by an about link; a day with both is listed once", edit("l.kind IN ('wikilink', 'about')", "l.kind = 'wikilink'")},
	{"journal", "cookbook/days-that-name lists pages that are not days", "cookbook/days-that-name lists the day pages that link Ana, newest first, and not an essay that names her", edit("JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day AND d.deleted_at IS NULL\n WHERE l.to_id = :entity_id", "JOIN entities d ON d.id = l.from_id AND d.deleted_at IS NULL\n WHERE l.to_id = :entity_id")},
	{"habits", "two periods of one habit may overlap", "an overlapping period refused", edit("   WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id\n", "   WHERE 0 AND EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id\n")},
	{"habits", "an update may make periods overlap", "moving a period onto another refused (update)", edit("WHERE EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id", "WHERE 0 AND EXISTS (SELECT 1 FROM habit_periods p WHERE p.metric_id = NEW.metric_id AND p.id <> NEW.id")},
	{"habits", "a metric with a unit may be a habit", "a period on a metric with a unit refused: a habit is 0/1", edit("   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';", "   WHERE 0;")},
	{"habits", "habit periods may be deleted", "a period is never deleted", edit("CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods\nBEGIN", "CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods WHEN 0\nBEGIN")},
	{"habits", "habit_periods.source can change", "a period's source never changes", edit("CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\n  WHEN NEW.source IS NOT OLD.source", "CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\n  WHEN 0")},
	{"habits", "a period may end before it starts", "an end before the start refused (habit_periods_order)", edit("CONSTRAINT habit_periods_order CHECK (end_day IS NULL OR end_day >= start_day)", "CONSTRAINT habit_periods_order CHECK (1)")},
	{"habits", "start_day is checked with = instead of IS", "start_day \"2026-13-03\" refused", edit("date(start_day) IS start_day", "date(start_day) = start_day")},
	{"habits", "cookbook/day-view ignores habit tombstones", "cookbook/day-view hides tombstoned habits", edit("FROM habit_periods h JOIN entities e ON e.id = h.metric_id AND e.deleted_at IS NULL", "FROM habit_periods h JOIN entities e ON e.id = h.metric_id")},
	{"habits", "cookbook/habits daily ignores tombstones", "cookbook/habits hides tombstoned daily rows", editNth("FROM habit_periods h JOIN entities e ON e.id = h.metric_id AND e.deleted_at IS NULL", "FROM habit_periods h JOIN entities e ON e.id = h.metric_id", 1)},
	{"habits", "cookbook/habits completion ignores tombstones", "cookbook/habits hides tombstoned completion rows", edit("JOIN entities e ON e.id = a.metric_id AND e.deleted_at IS NULL", "JOIN entities e ON e.id = a.metric_id")},
	{"habits", "cookbook/habits start ignores tombstones", "cookbook/habits start does not add a tombstoned metric", edit("SELECT e.id, :day, 'ui' FROM entity_names n JOIN entities e ON e.id = n.entity_id AND e.deleted_at IS NULL", "SELECT e.id, :day, 'ui' FROM entity_names n JOIN entities e ON e.id = n.entity_id")},
	{"habits", "cookbook/habits stop ignores tombstones", "cookbook/habits stop does not change a tombstoned metric", edit("SELECT e.id FROM entity_names n JOIN entities e ON e.id = n.entity_id AND e.deleted_at IS NULL", "SELECT e.id FROM entity_names n JOIN entities e ON e.id = n.entity_id")},
	{"journal", "cookbook/capture writes tombstoned Mood", "cookbook/capture does not write tombstoned Mood", edit("WHERE e.id = 1 AND e.deleted_at IS NULL;", "WHERE e.id = 1;")},
	{"habits", "cookbook/habits counts a day not recorded as not done", "cookbook/habits completion counts only active days: done, not done, not recorded", edit("sum(s.value IS 0 AND s.invalid IS 0) AS not_done", "sum(s.value IS NOT 1) AS not_done")},
	{"habits", "cookbook/habits loses the idempotent re-run", "cookbook/habits documents the idempotent re-run", edit("A re-sent period is idempotent: insert it with\n`ON CONFLICT(metric_id, start_day) DO NOTHING`", "A re-sent period is idempotent: insert it again")},
	{"habits", "cookbook/habits re-sends a period without its end_day", "cookbook/habits says a re-sent period carries its end_day", edit("carrying the `end_day` it was sent with", "as it is")},
	{"habits", "cookbook/day-view lists a habit outside its periods", "...outside every period a 0/1 reading is just a reading, and no habit is listed", edit("   WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day\n  UNION ALL", "   WHERE 1\n  UNION ALL")},
	{"journal", "cookbook/day-view shows superseded readings", "...not another day's page or place, a link target, a person's page, a page of another day or the superseded reading", edit("FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n    JOIN entities e ON e.id = m.id", "FROM measurements me JOIN metrics m ON m.id = me.metric_id\n    JOIN entities e ON e.id = m.id")},
	{"writers", "contract/connections no longer sets trusted_schema = OFF", "the contract/connections block sets every pragma the contract names", edit("PRAGMA trusted_schema = OFF;   -- the schema may call only side-effect-free functions (all of this one's are)\n", "")},
	{"integrity", "the orphan query forgets people", "a person with an owned name but no extension: only the orphan query sees it", edit(" OR (e.entity_type='person' AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id=e.id))", "")},
	{"integrity", "contract/integrity-checks loses the FTS5 integrity-check", "contract/integrity-checks has four groups: structure, foreign keys, domain/typed-edge semantics, FTS5", edit("INSERT INTO entities_fts(entities_fts, rank) VALUES ('integrity-check', 1);   -- no error\n", "")},
	{"imports", "the import block loses WHERE true", "the document's block loads 1 000 rows", edit("  FROM s.staging WHERE true\n", "  FROM s.staging\n")},
	{"imports", "contract/imports copies life.db through a read-write shell", "contract/imports step 1: the documented command opens the file read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO '/tmp/trial.db'", "sqlite3 life.db \"VACUUM INTO '/tmp/trial.db'")},
	{"imports", "contract/imports copies life.db through a reader that trusts the schema", "contract/imports step 1: the documented command opens the file read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO '/tmp/trial.db'", "sqlite3 -readonly life.db \"VACUUM INTO '/tmp/trial.db'")},
	{"snapshots", "the shell form of the snapshot trusts the schema", "the shell form of the snapshot opens life.db read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO 'life-", "sqlite3 -readonly life.db \"VACUUM INTO 'life-")},
	{"snapshots", "cookbook/take-a-snapshot takes it on the writer's connection", "the snapshot block runs on a connection that opened life.db read-only (mode=ro)", edit("-- on a connection to life.db opened read-only (mode=ro), while", "-- on the writer's connection to life.db, while")},
	{"snapshots", "the restore check opens the snapshot read-only, where the FTS5 check is refused", "the restore check opens the snapshot with the writer's connection settings", edit("open it with the writer's connection settings", "open it read-only (mode=ro)")},
	{"snapshots", "the restore leaves life.db in rollback-journal mode", "the restored life.db is in WAL mode again", edit("PRAGMA journal_mode = WAL;   -- once, on the restored life.db", "PRAGMA journal_mode = DELETE;   -- once, on the restored life.db")},
	{"evolution", "one CHECK is unnamed", "every CHECK in schema is named", edit("CONSTRAINT people_death_day_order CHECK", "CHECK")},
	{"cookbook", "cookbook/inside-a-place names a column that does not exist", "plain: all statements of the cookbook prepare", edit("SELECT d.day, pl.title AS place", "SELECT d.day, pl.name AS place")},
	{"document", "a 2075 answer is gone from the file", "Q18: the answer says [3.51.3 3.53]", edit("  ('sqlite',    'writers need SQLite >= 3.51.3", "  ('sqlite',    'writers need SQLite >= 3.51")},
	{"document", "a lifelog_meta key answers no question", "every lifelog_meta key answers some question (no rule without a question)", edit("  ('evolution', 'after the freeze", "  ('orphan',    'x'),\n  ('evolution', 'after the freeze")},
	{"document", "cookbook/import-a-row-once bypasses the save contract", "import-a-row-once sends an imported body through the save contract (D19)", edit("run the link sync of [save a body](save-a-body.md)", "skip")},
	{"document", "the deletes row hides the registries", "Q6: the answer says [tombstone links registries revival]", edit("the registries (link_kinds, lifelog_meta) are the owner''s administrative rows", "link_kinds and lifelog_meta are the owner''s administrative rows")},
	{"document", "the schema totals drift from the DDL", "the totals in schema/README.md match the DDL (tables, views, triggers)", edit("**+ 79 triggers.**", "**+ 30 triggers.**")},
	{"diagrams", "a foreign key is not drawn", "every foreign key is drawn as `parent --- child : first FK column`", edit("    entities ||--o| people : \"id\"\n", "")},
	{"diagrams", "the link map invents an edge", "the link map has the same edges as link_kinds", edit("    place -->|\"located-in\"| place\n", "    place -->|\"located-in\"| place\n    person -->|\"mentioned\"| page\n")},
	{"diagrams", "a new link kind, the map unchanged", "the link map has the same edges as link_kinds", edit("  ('friend',   1, 'person',    'person',       NULL),", "  ('mentor',   0, 'person',    'person',       NULL),\n  ('friend',   1, 'person',    'person',       NULL),")},
	{"diagrams", "the correction story shows a wrong value", "executed, the view shows what each state of the diagram says", edit("state \"view shows 70.8\" as V2", "state \"view shows 70.9\" as V2")},
	{"diagrams", "a non-key column is drawn", "er-core.link_kinds.symmetric is a key column (the diagrams draw keys only)", edit("    link_kinds {\n        TEXT kind PK\n", "    link_kinds {\n        TEXT kind PK\n        INTEGER symmetric\n")},
	{"named", "the ark query lists a symmetric relation in both legs", "...and a friend once, not twice", edit(" AND l.kind NOT IN (SELECT kind FROM link_kinds WHERE symmetric = 1)", "")},
	{"facts", "metric-series has no upper bound", "cookbook/metric-series: the 90 days ending on :day, none after it", edit("me.day <= :day", "1")},
	{"facts", "metric-series includes the 91st day", "cookbook/metric-series: the 90 days ending on :day, none after it", edit("me.day > date(", "me.day >= date(")},
	{"named", "a promotion does not revive a tombstoned page", "a tombstoned ghost is promoted and revived", edit(", deleted_at = NULL   -- revives", "   -- revives")},
	{"dates", "entities.updated_at may lack milliseconds", "entities.updated_at without milliseconds refused", nocheck("entities_updated_at")},
	{"dates", "a tombstone need not be an instant", "a tombstone that is not an instant refused", nocheck("entities_deleted_at")},
	{"dates", "links.created_at need not be an instant", "links.created_at must be an instant", nocheck("links_created_at")},
	{"dates", "measurements.created_at need not be an instant", "measurements.created_at must be an instant", nocheck("measurements_created_at")},
	{"dates", "people.death_day need not round-trip", "people.death_day must round-trip", nocheck("people_death_day")},
	{"dates", "habit_periods.end_day need not round-trip", "habit_periods.end_day must round-trip", nocheck("habit_periods_end_day")},
	{"named", "a death may precede the birth", "a death before the birth is refused", nocheck("people_death_day_order")},
	{"identity", "links.source may be anything", "links.source takes the same GLOB as entities.source", nocheck("links_source")},
	{"links", "link_kinds.symmetric may be 2", "link_kinds.symmetric is 0 or 1", nocheck("link_kinds_symmetric")},
	{"identity", "habit_periods.source may be anything", "habit_periods.source takes the same GLOB as entities.source", nocheck("habit_periods_source")},
	{"pages", "a title key may hold an ASCII capital", "a non-ASCII title's key may not hold an ASCII capital", nocheck("entity_names_key_folded")},
	{"pages", "a title may have leading or trailing space", "a title with leading space refused (a key the writer folded to match)", edit("CHECK (title = trim(title) AND length(title) >= 1", "CHECK (length(title) >= 1")},
	{"pages", "a title may hold a NUL byte", "a title with a NUL byte refused", edit("         AND instr(title, char(0)) = 0\n", "")},
	{"facts", "a correction of a row that does not exist passes with foreign_keys=OFF", "with foreign_keys=OFF, a correction of a row that does not exist is refused by the trigger", edit("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) <> NEW.metric_id;")},
	{"links", "an unknown endpoint id passes as a place with foreign_keys=OFF", "with foreign_keys=OFF, a typed link to an id that does not exist is refused", edit("coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), '?')", "coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), 'place')")},
	{"named", "ghost_pages lists a page younger than 30 days", "ghost_pages leaves a page younger than 30 days alone", edit("     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')\n", "")},
	{"named", "ghost_pages lists a tombstoned page", "ghost_pages leaves a tombstoned page alone", edit("   WHERE e.entity_type = 'page' AND e.body = '' AND e.deleted_at IS NULL", "   WHERE e.entity_type = 'page' AND e.body = ''")},
	{"facts", "cookbook/mood-over-time shows superseded readings", "cookbook/mood-over-time skips superseded and retracted readings", edit("FROM measurement_values me\n  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'", "FROM measurements me\n  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'")},
	{"facts", "cookbook/metric-series shows superseded readings", "cookbook/metric-series skips superseded and retracted readings", edit("FROM measurement_values me\n  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'", "FROM measurements me\n  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'")},
	{"facts", "measurements may be deleted", "DELETE refused", notrigger("measurements_no_delete")},
	{"writers", "the DDL does not mark the file as Lifelog", "the DDL marks the file as Lifelog: application_id 0x4C494645 and user_version 1", edit("PRAGMA application_id = 0x4C494645;", "PRAGMA application_id = 0;")},
	{"pages", "a title can change", "an owned name key cannot change", notrigger("entity_names_fixed")},
	{"identity", "entities may be hard-deleted", "with foreign_keys=OFF: DELETE FROM entities still refused (the trigger, not the FK)", notrigger("entities_no_delete")},
	{"identity", "a link's endpoints can change", "a link's endpoints cannot change", notrigger("links_fixed")},
	{"identity", "editing a person does not bump updated_at", "updating a person bumps entities.updated_at", notrigger("people_touch")},
	{"identity", "measurements.source may be anything", "source  rejected on entities and measurements", nocheck("measurements_source")},
	{"identity", "entities.entity_type may be anything", "entities.entity_type rejects an unknown type", nocheck("entities_entity_type")},
	{"facts", "a metrics row may name a page that is not a metric", "...and cannot claim another type to hang off a person's page", nocheck("metrics_entity_type")},
	{"facts", "a metric's unit can change", "metrics.unit cannot change", notrigger("metrics_unit_fixed")},
	{"facts", "a reading may be corrected twice", "a second correction of the same row is refused", edit("CREATE UNIQUE INDEX measurements_one_correction", "CREATE INDEX measurements_one_correction")},
	{"facts", "a measurement may be imported twice", "a duplicate measurement import is refused by its unique index", edit("CREATE UNIQUE INDEX measurements_import", "CREATE INDEX measurements_import")},
	{"facts", "a reading may supersede itself", "a row cannot supersede itself", nocheck("measurements_not_self")},
	{"facts", "a first reading may have no value", "a first reading with NULL value is refused", nocheck("measurements_first_has_value")},
	{"facts", "a metrics row may hang off a page that is not a metric", "a metrics row needs a page of type metric", edit("  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type)\n) STRICT;\nCREATE TRIGGER metrics_unit_fixed", "  FOREIGN KEY (id) REFERENCES entities(id)\n) STRICT;\nCREATE TRIGGER metrics_unit_fixed")},
	{"identity", "metrics may be hard-deleted", "DELETE FROM metrics is refused", notrigger("metrics_no_delete")},
	{"integrity", "the orphan query forgets metrics", "a metric with an owned name but no extension: only the orphan query sees it", edit(" OR (e.entity_type='metric' AND NOT EXISTS (SELECT 1 FROM metrics m WHERE m.id=e.id))", "")},
	{"facts", "a category may be a person", "...but a category is a plain page: never a person or a metric", edit("  ('part-of',  0, NULL,        'page',", "  ('part-of',  0, NULL,        'page,person',")},
	{"facts", "cookbook/metrics-by-category walks no deeper than the category itself", "cookbook/metrics-by-category: a category holds the metrics of every category under it", edit("  SELECT l.from_id FROM links l JOIN tree t ON l.to_id = t.id AND l.kind = 'part-of'\n", "  SELECT l.from_id FROM links l JOIN tree t ON l.to_id = t.id AND l.kind = 'part-of' AND 0\n")},
	{"facts", "cookbook/metrics-by-category files metrics in a deleted category", "cookbook/metrics-by-category: a deleted category's metrics read as filed nowhere", edit("FROM links l JOIN entities e ON e.id = l.to_id AND e.deleted_at IS NULL", "FROM links l JOIN entities e ON e.id = l.to_id")},
	{"facts", "measurement_values shows retractions", "the tap and its retraction are hidden", edit("   WHERE me.value IS NOT NULL\n     AND NOT EXISTS", "   WHERE NOT EXISTS")},
	{"dates", "measurements.tz may be anything", "measurements.tz \"\" rejected", nocheck("measurements_tz")},
	{"dates", "measurements.day is checked with = instead of IS", "exact measurements.day \"2026-13-01\" accepted=false", edit("CONSTRAINT measurements_day CHECK (length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day)", "CONSTRAINT measurements_day CHECK (length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) = day)")},
	{"dates", "people.birth_day is checked with = instead of IS", "exact people.birth_day \"2026-13-01\" accepted=false", edit("date(birth_day) IS birth_day", "date(birth_day) = birth_day")},
	{"imports", "measurements.taken_at may be anything", "without NULLIF an empty taken_at ('' is not NULL) fails its CHECK", nocheck("measurements_taken_at")},
	{"pages", "a pure-ASCII title may have any key", "ASCII title with a wrong key rejected", nocheck("entity_names_key_ascii")},
	{"pages", "a title may be 400 bytes", "241 bytes rejected (é × 121 = 242 bytes)", edit("length(CAST(title AS BLOB)) <= 240", "length(CAST(title AS BLOB)) <= 400")},
	{"links", "deleting a symmetric link leaves its mirror", "deleting one side of a symmetric edge deletes its mirror", notrigger("links_mirror_delete")},
	{"links", "an unregistered kind passes the trigger", "with foreign_keys=OFF an unregistered kind is still refused (the trigger, in autocommit)", edit("   WHERE NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);", "   WHERE 0 AND NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);")},
	{"links", "a link kind may be named in upper case", "a kind name in upper case is refused", nocheck("link_kinds_kind")},
	{"links", "from_types may be malformed", "a malformed type list is refused", nocheck("link_kinds_from_types")},
	{"links", "a symmetric kind may have different endpoint types", "a symmetric kind with different endpoint types is refused", nocheck("link_kinds_mirror_valid")},
	{"habits", "one habit may start twice on a day", "the same start twice is refused by UNIQUE", edit(",\n  UNIQUE (metric_id, start_day)", "")},
	{"habits", "an update may give a habit a unit", "moving a period to a metric with a unit refused (update)", editNth("   WHERE (SELECT unit FROM metrics WHERE id = NEW.metric_id) IS NOT '';", "   WHERE 0;", 1)},
	{"habits", "a period that starts where another starts is refused as an overlap", "the same start twice is refused by UNIQUE", edit("                    AND p.start_day IS NOT NEW.start_day\n", "")},
	{"writers", "the DDL does not set WAL", "journal_mode=WAL is stored in the file by the DDL", edit("PRAGMA journal_mode  = WAL;", "PRAGMA journal_mode  = DELETE;")},
	{"writers", "contract/connections lets readers keep trusted_schema on", "contract/connections: readers are read-only and set trusted_schema = OFF", edit("Readers must be **read-only** and set **`PRAGMA trusted_schema = OFF`** per connection, as writers do.", "Readers must be **read-only**.")},
	{"pages", "the Cn rule names a Unicode version the writer does not pin", "the Cn rule names one Unicode version, the one the writer pins", edit("**Unicode 15.0** has not", "**Unicode 16.0** has not")},
	{"pages", "the Cn rule accepts a code point Unicode 15.0 has not assigned", "the Cn rule lists code points a writer refuses and accepts", edit("and `U+1FAE9` (16.0) in a title, and accepts `U+1FAE8` (15.0)", "in a title, and accepts `U+1FAE8` (15.0) and `U+1FAE9` (16.0)")},
	{"files", "files.sha256 may be anything", "sha256 AAAAAAAAAAAA… refused", nocheck("files_sha256")},
	{"files", "files.mime may be anything", "mime Image/JPEG refused", nocheck("files_mime")},
	{"files", "files_preview compares with = and lets an empty preview through", "preview: an empty blob refused", edit("substr(preview, 1, 3) IS x'FFD8FF'", "substr(preview, 1, 3) = x'FFD8FF'")},
	{"files", "a preview need not be a JPEG", "preview: a PNG refused", edit("(substr(preview, 1, 3) IS x'FFD8FF'\n", "(1\n")},
	{"files", "a preview may be 2 MB", "preview: 1 MB accepted, 1 MB and a byte refused", edit("AND length(preview) <= 1048576", "AND length(preview) <= 2097152")},
	{"files", "one original may be kept twice", "one page per original: a second files row with the same sha256 is refused (files_sha256)", edit("CREATE UNIQUE INDEX files_sha256", "CREATE INDEX files_sha256")},
	{"files", "the hash and type of an original can change", "files.sha256 cannot change", notrigger("files_original_fixed")},
	{"files", "adding a preview does not bump updated_at", "adding it bumps entities.updated_at (files_touch)", notrigger("files_touch")},
	{"files", "a files row may hang off a page that is not a file", "a files row needs a page of type file", edit("  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type)\n) STRICT;\nCREATE UNIQUE INDEX files_sha256", "  FOREIGN KEY (id) REFERENCES entities(id)\n) STRICT;\nCREATE UNIQUE INDEX files_sha256")},
	{"files", "a files row may claim another type", "...and cannot claim another type to hang off a person's page", nocheck("files_entity_type")},
	{"files", "an embed cannot land on a file page", "an embed in a day page lands on the file page (a wikilink)", edit("('wikilink', 0, 'page,person,place,metric,file,period', 'page,person,place,metric,file,period',", "('wikilink', 0, 'page,person,place,metric,file,period', 'page,person,place,metric,period',")},
	{"files", "cookbook/keep-a-file finds an original only under its own source", "the same original again, from another source: step 0 finds the page, and nothing is written", edit(" WHERE f.sha256 = :sha256;", " WHERE f.sha256 = :sha256 AND e.source = :source;")},
	{"files", "cookbook/keep-a-file replaces a preview a file already has", "...and an existing preview is never replaced", edit("WHERE sha256 = :sha256 AND preview IS NULL AND :preview IS NOT NULL", "WHERE sha256 = :sha256 AND :preview IS NOT NULL")},
	{"files", "cookbook/keep-a-file fills the preview of a tombstoned file", "a tombstoned file is left alone: no preview is filled", edit("\n   AND id IN (SELECT id FROM entities WHERE deleted_at IS NULL);", ";")},
	{"files", "cookbook/keep-a-file leaves a promoted page without its missing day", "promotion missing day: recipe and writer agree, filling only a missing day", edit("UPDATE entities SET day = :day WHERE id = :file_id AND entity_type = 'file' AND day IS NULL;", "UPDATE entities SET day = day WHERE id = :file_id AND entity_type = 'file' AND day IS NULL;")},
	{"files", "cookbook/keep-a-file overwrites the text of the page it promotes", "a page with text becomes the file with its text kept; :body fills only an empty body", edit("WHERE id = :file_id AND entity_type = 'file' AND body = '';", "WHERE id = :file_id AND entity_type = 'file';")},
	{"identity", "files may be hard-deleted", "DELETE FROM files is refused", notrigger("files_no_delete")},
	{"integrity", "the orphan query forgets files", "a file with an owned name but no extension: only the orphan query sees it", edit(" OR (e.entity_type='file' AND NOT EXISTS (SELECT 1 FROM files f WHERE f.id=e.id))", "")},
	{"places", "a place's latitude may be anything", "a point at (91, 0.5) is refused", nocheck("places_lat")},
	{"places", "a place's longitude may be anything", "a point at (10, 180.5) is refused", nocheck("places_lon")},
	{"places", "a radius may be any size", "a radius of 9 m is refused", nocheck("places_radius")},
	{"places", "link_days may be anything", "link_days is 0 or 1", nocheck("places_link_days")},
	{"places", "a place may sit at 0°, 0°", "a point at (0, 0) is refused", nocheck("places_not_null_island")},
	{"places", "a places row may claim another type", "...and cannot claim another type", nocheck("places_entity_type")},
	{"places", "a places row may hang off a page that is not a place", "a places row needs a page of type place", edit("  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),\n  CONSTRAINT places_not_null_island", "  FOREIGN KEY (id) REFERENCES entities(id),\n  CONSTRAINT places_not_null_island")},
	{"places", "a point may be deleted", "a point is never deleted", notrigger("places_no_delete")},
	{"places", "fixing a point does not bump updated_at", "...which bumps entities.updated_at (places_touch)", notrigger("places_touch")},
	{"places", "cookbook/place-of-a-photo moves a point a second answer gives", "cookbook/place-of-a-photo: a place's point is given once; a second answer never moves it", edit("ON CONFLICT(id) DO NOTHING;\n```\n\n**Match", "ON CONFLICT(id) DO UPDATE SET lat = excluded.lat, lon = excluded.lon, radius_m = excluded.radius_m, link_days = excluded.link_days;\n```\n\n**Match")},
	{"places", "cookbook/place-of-a-photo matches the nearest place, not the smallest circle", "the match is the haversine answer for all 400 positions among 60 live places of random radii (within 1 % of a radius, either answer)", edit(" ORDER BY radius_m, d2, id\n", " ORDER BY d2, id\n")},
	{"places", "cookbook/place-of-a-photo matches a place whatever the distance", "a position in Lisbon, outside the café: Lisbon", edit(" WHERE d2 <= radius_m * radius_m\n", " WHERE 1\n")},
	{"places", "cookbook/place-of-a-photo ignores the latitude's metres per degree", "and its distance is within 0.5 % of the great-circle distance", edit("((pl.lon - :lon) * :m_per_deg_lon) * ((pl.lon - :lon) * :m_per_deg_lon) AS d2", "((pl.lon - :lon) * 111320.0) * ((pl.lon - :lon) * 111320.0) AS d2")},
	{"places", "cookbook/place-of-a-photo matches a tombstoned place", "a tombstoned place is never the answer", edit("JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL", "JOIN entities e ON e.id = pl.id")},
	{"places", "cookbook/place-of-a-photo ignores the parsed append decision", "cookbook parsed embed binding preserves aliases but appends after literal code/plain links", edit(" WHERE id = :photo_day_id AND :append_embed = 1;", " WHERE id = :photo_day_id AND instr(body, '![[' || :file_title || ']]') = 0;")},
	{"places", "cookbook/place-of-a-photo shows a photo twice in its day", "...and a second photo of that day at that place, or the same photo again, adds no link and no second embed", edit(" WHERE id = :photo_day_id AND :append_embed = 1;", " WHERE id = :photo_day_id;")},
	{"doc-save-contract", "an embed makes no link", "A every printed vector reproduces with the writer's extraction", edit("| `![[Lake.jpg]] and ![[Lake.jpg\\|a lake]]` | `Lake.jpg` |", "| `![[Lake.jpg]] and ![[Lake.jpg\\|a lake]]` | — |")},
}

var (
	baselineMu sync.Mutex
	baseline   = map[string]string{}
)

// clean runs a suite on the unmutated document once: "" when every expectation is met.
func clean(t *testing.T, suite string) string {
	baselineMu.Lock()
	defer baselineMu.Unlock()
	if r, ok := baseline[suite]; ok {
		return r
	}
	s, stopped := runSuite(suite, realDocs(), t.TempDir())
	r := ""
	if stopped != "" || s.ok != s.n || s.n == 0 {
		r = fmt.Sprintf("%d/%d: %v", s.ok, s.n, s.fails)
	}
	baseline[suite] = r
	return r
}

// TestMutants runs every mutant against the suite that owns its rule (skipped with -short).
func TestMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("mutants: skipped with -short")
	}
	for _, m := range mutants {
		if m.witness == "" {
			t.Fatalf("%s: missing intended assertion witness", m.name)
		}
		if r := clean(t, m.suite); r != "" {
			t.Fatalf("%s must pass on the unmutated document first: %s", m.suite, r)
		}
	}
	for i, m := range mutants {
		t.Run(fmt.Sprintf("%03d %s: %s", i+1, m.suite, m.name), func(t *testing.T) {
			t.Parallel()
			over, err := m.change.apply(realDocs())
			if err != nil {
				t.Fatal(err)
			}
			s, stopped := runSuite(m.suite, realDocs().with(over), t.TempDir())
			if !mutantKilled(s, stopped, m.witness) {
				how := fmt.Sprintf("intended assertion %q did not fail; failed labels: %q", m.witness, s.failedLabels)
				if stopped != "" {
					how = "stopped only: " + clip(stopped, 120)
				}
				t.Errorf("MISSED: %s did not notice %q (%d/%d; %s)", m.suite, m.name, s.ok, s.n, how)
			}
		})
	}
	t.Logf("mutants: %d", len(mutants))
}

// mutantKilled matches the exact expectation label, never formatted diagnostics.
// A setup stop invalidates even an earlier intended failure.
func mutantKilled(s *S, stopped, witness string) bool {
	return witness != "" && stopped == "" && contains(s.failedLabels, witness)
}

func TestMutantWitness(t *testing.T) {
	t.Run("diagnostics are not identities", func(t *testing.T) {
		s := &S{}
		s.K("unrelated", false, "intended")
		if mutantKilled(s, "", "intended") || !contains(s.failedLabels, "unrelated") {
			t.Fatal("formatted details credited as an assertion identity")
		}
	})
	for _, tc := range []struct {
		name             string
		failures         []string
		stopped, witness string
		want             bool
	}{
		{"unrelated failure", []string{"unrelated"}, "", "intended", false},
		{"intended failure", []string{"intended"}, "", "intended", true},
		{"stopped suite", []string{"intended", "other"}, "setup failed", "intended", false},
		{"missing witness", []string{"intended"}, "", "", false},
		{"no failure", nil, "", "intended", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &S{n: len(tc.failures), failedLabels: tc.failures}
			if got := mutantKilled(s, tc.stopped, tc.witness); got != tc.want {
				t.Errorf("kill=%v want %v", got, tc.want)
			}
		})
	}
	t.Run("unchanged mutation", func(t *testing.T) {
		over, err := edit("CONSTRAINT entities_day CHECK", "CONSTRAINT entities_day CHECK").apply(realDocs())
		if err != nil {
			t.Fatal(err)
		}
		s, stopped := runSuite("dates", realDocs().with(over), t.TempDir())
		if stopped != "" || s.ok != s.n || s.n == 0 {
			t.Fatalf("no-op baseline: %d/%d stopped=%s failures=%v", s.ok, s.n, stopped, s.fails)
		}
		if mutantKilled(s, stopped, `entities.day rejects "banana"`) {
			t.Fatal("unchanged mutation credited")
		}
	})
}
