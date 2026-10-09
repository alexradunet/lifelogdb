package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// An import key is 1 to 512 bytes without NUL (entities_import_key and its siblings); a source identifier that can
// be longer is stored as "sha256:" and the 64 hex digits of the SHA-256 of its bytes, so the same source gives the
// same key on every run (docs/contract/imports.md).

func TestImportKeyShape(t *testing.T) {
	const hashed = "sha256:"
	for _, c := range []struct {
		name string
		raw  string
		want string // "" = unchanged
	}{
		{"one byte", "a", ""},
		{"a short path key", "Journal/2031-04-11.md|person|bob sample", ""},
		{"exactly 512 bytes", strings.Repeat("a", 512), ""},
		{"512 bytes of two-byte runes", strings.Repeat("é", 256), ""},
		{"513 bytes", strings.Repeat("a", 513), hashed + "02425c0f5b0dabf3d2b9115f3f7723a02ad8bcfb1534a0d231614fd42b8188f6"},
		// the limit is in bytes, not characters: 257 runes of two bytes
		{"514 bytes in 257 runes", strings.Repeat("é", 257), hashed + "db33c4ce9e8a72562c8f24aa67b7abbabdb848426e8ddfff4ee08f6ae04d1b17"},
		// a NUL byte is hashed, never stored and never refused: the source's identifier stays usable
		{"a NUL byte", "a\x00b", hashed + "59b271ae1bbcb1d31d41929817f4b16fb439eb4f31520b5ad1d5ce98920a7138"},
		// the empty string would enter the unique index and shadow every later row of the source
		{"empty", "", hashed + "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := importKey(c.raw)
			want := c.want
			if want == "" {
				want = c.raw
			}
			if got != want {
				t.Errorf("importKey = %.90q (%d bytes), want %.90q (%d bytes)", got, len(got), want, len(want))
			}
			if n := len(got); n < 1 || n > 512 || strings.ContainsRune(got, 0) {
				t.Errorf("the key %.90q is not a legal import key (%d bytes)", got, n)
			}
			if again := importKey(c.raw); again != got {
				t.Errorf("a second call gave %.90q, then %.90q", got, again)
			}
			// a stored key put through the function again is itself, so a key read back from the database can be passed on
			if again := importKey(got); again != got {
				t.Errorf("importKey of %.90q is %.90q", got, again)
			}
		})
	}
}

func TestPreparedKeysAreImportKeys(t *testing.T) {
	short := preparedKey("legacy-sleep-array-v1", "sleep:9007199254740993", "asleep-minutes")
	if want := `["prepared-v1","legacy-sleep-array-v1","sleep:9007199254740993","asleep-minutes"]`; short != want {
		t.Errorf("a short prepared key changed: %s", short)
	}
	long := preparedKey("legacy-sleep-array-v1", "sleep:"+strings.Repeat("9", 600), "asleep-minutes")
	sum := sha256.Sum256([]byte(`["prepared-v1","legacy-sleep-array-v1","sleep:` + strings.Repeat("9", 600) + `","asleep-minutes"]`))
	if want := "sha256:" + hex.EncodeToString(sum[:]); long != want {
		t.Errorf("a long prepared key is %q, want %q", long, want)
	}
}

// storedKeys are the import keys of one source, from every table of the file.
func storedKeys(t *testing.T, f *fixture, source string) []string {
	t.Helper()
	rows, err := f.s.DB.R.QueryContext(ctx, `SELECT import_key FROM entities WHERE source = ? AND import_key IS NOT NULL
	    UNION ALL SELECT import_key FROM measurements WHERE source = ? AND import_key IS NOT NULL
	    UNION ALL SELECT import_key FROM sessions WHERE source = ? AND import_key IS NOT NULL`, source, source, source)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(keys)
	return keys
}

func sha256Key(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// A long path and long titles make every key of a facts file longer than 512 bytes. The file is still imported,
// under keys that are the same on every run, and a second apply writes nothing.
func TestLongFactsKeysAreHashedAndReplayWritesNothing(t *testing.T) {
	file := "Journal/" + strings.Repeat("d", 140) + "/" + strings.Repeat("e", 140) + "/2031-09-01.md"
	person, place, page, metric := strings.Repeat("p", 240), strings.Repeat("q", 240), strings.Repeat("r", 240), strings.Repeat("m", 200)
	body := "Met " + person + ". Went to " + place + ". Read " + page + ". " + metric + ": 4\n"
	f := setupEvidenceMetric(t, file, body, Metric{Name: metric, Note: "a long name"})
	facts := map[string]any{"file": file, "writes": []any{
		map[string]any{"person": map[string]any{"title": person}, "quote": "Met " + person},
		map[string]any{"place": map[string]any{"title": place}, "quote": "Went to " + place},
		map[string]any{"page": map[string]any{"title": page}, "quote": "Read " + page},
		map[string]any{"reading": map[string]any{"metric": metric, "day": "2031-09-01", "value": "4"}, "quote": metric + ": 4"},
	}}
	f.decideNames(t, Entity{Kind: "person", Name: person}, Entity{Kind: "place", Name: place})
	if err := f.facts(t, file, facts); err != nil {
		t.Fatal(err)
	}
	r, err := f.w.Apply(ctx, f.s, file)
	if err != nil {
		t.Fatalf("a file whose keys are longer than 512 bytes: %v", err)
	}
	if r.Summary != "1 page, 1 place, 1 person, 1 reading (4 new)" {
		t.Errorf("first apply: %s", r.Summary)
	}
	want := []string{
		sha256Key(file + "|person|" + person), sha256Key(file + "|place|" + place), sha256Key(file + "|page|" + page),
		sha256Key(file + "|reading|" + metric + "|2031-09-01|1"),
	}
	sort.Strings(want)
	got := storedKeys(t, f, "import:notebook")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("stored keys\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	before, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	measurements := totalMeasurements(t, f)

	if r, err = f.w.Apply(ctx, f.s, file); err != nil || r.Summary != "1 page, 1 place, 1 person, 1 reading (4 existing)" {
		t.Fatalf("a second apply: %v, %v", r, err)
	}
	after, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.Pages != after.Pages || before.Readings != after.Readings || totalMeasurements(t, f) != measurements {
		t.Errorf("a second apply wrote rows: %+v -> %+v", before, after)
	}
	if again := storedKeys(t, f, "import:notebook"); strings.Join(again, "\n") != strings.Join(got, "\n") {
		t.Errorf("the keys changed on the second run: %v", again)
	}
	// status finds every stored key in a done file: it derives the same keys
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil || len(st.Mismatches) != 0 {
		t.Errorf("status: %+v, %v", st, err)
	}
}

// A vault note is keyed by its path: a path longer than 512 bytes is hashed, and a second apply of the vault finds
// the page by that key and writes nothing.
func TestLongVaultNotePathIsHashedAndReplayWritesNothing(t *testing.T) {
	path := "Notes/" + strings.Repeat("a", 200) + "/" + strings.Repeat("b", 200) + "/" + strings.Repeat("c", 200) + "/Deep note.md"
	f := setup(t)
	addSourceFile(t, f, path, "A note in a deep folder.\n")
	f.approveRules(t, rulesBody)
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatalf("a vault with a path longer than 512 bytes: %v", err)
	}
	var key string
	if err := f.s.DB.R.QueryRowContext(ctx, `SELECT e.import_key FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key WHERE n.title = 'Deep note'`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != sha256Key(path) {
		t.Errorf("the page of a %d-byte path is keyed %q, want %q", len(path), key, sha256Key(path))
	}
	before, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.w.ApplyVault(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 0 || res.Saved != 0 || res.Appended != 0 {
		t.Errorf("a second apply wrote %+v", res)
	}
	after, err := f.s.Counts(ctx)
	if err != nil || before.Pages != after.Pages {
		t.Errorf("a second apply changed the pages: %+v -> %+v, %v", before, after, err)
	}
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range st.Mismatches {
		if strings.Contains(m, "are in no done facts file or vault note") {
			t.Errorf("status does not know the hashed key of the note: %s", m)
		}
	}
}

// The ordinals of one metric's readings of a day lengthen their keys by a byte at the tenth: the first nine keys are
// exactly 512 bytes and stay as they are, the tenth is 513 and is hashed. A replay finds all ten.
func TestReadingKeysAcrossTheLimitReplayWritesNothing(t *testing.T) {
	metric := strings.Repeat("s", 100)
	// the key is file|reading|metric|2031-09-01|<ordinal>: size the folders so that a one-digit ordinal makes 512
	folders := 490 - len(metric) - len("Journal/") - len("/2031-09-01.md") - 1
	file := "Journal/" + strings.Repeat("d", folders/2) + "/" + strings.Repeat("e", folders-folders/2) + "/2031-09-01.md"
	var lines []string
	var writes []any
	var raw []string
	for n := 1; n <= 10; n++ {
		line := metric + " " + strings.Repeat("1", 1) + string(rune('0'+n%10))
		lines = append(lines, line)
		writes = append(writes, map[string]any{"reading": map[string]any{"metric": metric, "day": "2031-09-01", "value": "1" + string(rune('0'+n%10))}, "quote": line})
		raw = append(raw, file+"|reading|"+metric+"|2031-09-01|"+string(rune('0'+n%10)))
	}
	raw[9] = file + "|reading|" + metric + "|2031-09-01|10"
	if len(raw[8]) != 512 || len(raw[9]) != 513 {
		t.Fatalf("test setup: the keys are %d and %d bytes, want 512 and 513", len(raw[8]), len(raw[9]))
	}
	f := setupEvidenceMetric(t, file, strings.Join(lines, "\n")+"\n", Metric{Name: metric, Note: "a long name"})
	if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
		t.Fatal(err)
	}
	if r, err := f.w.Apply(ctx, f.s, file); err != nil || r.Summary != "10 reading (10 new)" {
		t.Fatalf("first apply: %v, %v", r, err)
	}
	var want []string
	for i, k := range raw {
		if i == 9 {
			k = sha256Key(k)
		}
		want = append(want, k)
	}
	sort.Strings(want)
	got := storedKeys(t, f, "import:notebook")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("stored keys\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r, err := f.w.Apply(ctx, f.s, file); err != nil || r.Summary != "10 reading (10 existing)" {
		t.Fatalf("second apply: %v, %v", r, err)
	}
	if n := totalMeasurements(t, f); n != 10 {
		t.Errorf("%d measurements after a replay", n)
	}
	if st, err := f.w.Status(ctx, f.s, f.trial); err != nil || len(st.Mismatches) != 0 {
		t.Errorf("status: %+v, %v", st, err)
	}
}

// A correction names its reading by the key it is stored under. For a reading whose key was hashed that is the hashed
// key: the correction is recorded and replayed into a fresh file like any other.
func TestACorrectionOfAHashedKeyReadingIsReplayed(t *testing.T) {
	file := "Journal/" + strings.Repeat("d", 140) + "/" + strings.Repeat("e", 140) + "/2031-09-02.md"
	metric := strings.Repeat("m", 200)
	f := setupEvidenceMetric(t, file, metric+": 4\n", Metric{Name: metric, Note: "a long name"})
	if err := f.facts(t, file, readingFacts(file, metric, "2031-09-02", "4", "", metric+": 4")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, file); err != nil {
		t.Fatal(err)
	}
	key := sha256Key(file + "|reading|" + metric + "|2031-09-02|1")
	var id int64
	if err := f.s.Do(ctx, "import:notebook", func(tx *core.Tx) (err error) {
		id, err = tx.MeasurementByKey("import:notebook", metric, key)
		return
	}); err != nil || id == 0 {
		t.Fatalf("the reading is not stored under %s: %d, %v", key, id, err)
	}
	five := 5.0
	_, corrected, err := f.s.Correct(ctx, "cli", id, &five)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Key != key {
		t.Fatalf("the correction names %q, want the stored key %q", corrected.Key, key)
	}
	if err := f.w.RecordCorrection(corrected); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "life.db")
	res, err := f.w.Replay(ctx, f.s, target)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res.Corrections != 1 || len(res.Differences) != 0 || !res.Integrity.OK {
		t.Errorf("replay: %+v", res)
	}
	d, err := db.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ts := &core.Store{DB: d}
	var current float64
	if err := ts.Do(ctx, "import:notebook", func(tx *core.Tx) error {
		root, err := tx.MeasurementByKey("import:notebook", metric, key)
		if err != nil || root == 0 {
			return fmt.Errorf("the replayed reading: %d, %v", root, err)
		}
		_, v, ok, err := tx.CurrentOf(root)
		if err == nil && !ok {
			err = fmt.Errorf("the replayed reading is retracted")
		}
		current = v
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if current != 5 {
		t.Errorf("the replayed reading is %g, want the corrected 5", current)
	}
}
