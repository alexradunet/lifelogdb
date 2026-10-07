package importer

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// The source profiles must retain exact string identities and reject duplicate
// decoded keys, including alternative JSON escape spellings (README source profiles).
func FuzzSourceJSONIdentity(f *testing.F) {
	for _, seed := range [][2]string{
		{"logId", "00018446744073709551615"},
		{"", ""},
		{"café", "cafe\u0301"},
		{"\x00key", "\"\\\n\t"},
		{"🌙", "unresolved local clock"},
		{"replacement", "\ufffd"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, key, value string) {
		if len(key)+len(value) > 8192 || !utf8.ValidString(key) || !utf8.ValidString(value) {
			t.Skip("bounded valid Unicode string identities")
		}
		marshal := func(s string) string {
			t.Helper()
			b, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
		quotedKey, quotedValue := marshal(key), marshal(value)
		valid := "{" + quotedKey + ":" + quotedValue + "}"
		if err := validateSourceJSON([]byte(valid)); err != nil {
			t.Fatalf("rejected valid string identity %q: %v", valid, err)
		}
		var decoded map[string]string
		if err := json.Unmarshal([]byte(valid), &decoded); err != nil || len(decoded) != 1 || decoded[key] != value {
			t.Fatalf("identity did not round trip: %q, %+v, %v", valid, decoded, err)
		}
		// Encode the same key independently as UTF-16 escapes. Duplicate detection
		// must compare decoded identities rather than raw bytes or NFC spellings.
		var escaped strings.Builder
		escaped.WriteByte('"')
		for _, unit := range utf16.Encode([]rune(key)) {
			fmt.Fprintf(&escaped, "\\u%04x", unit)
		}
		escaped.WriteByte('"')
		duplicate := "{" + quotedKey + ":" + quotedValue + "," + escaped.String() + ":null}"
		for _, rejected := range []string{
			duplicate,
			"[{\"nested\":" + duplicate + "}]",
			valid + " null",
			"[" + valid + ",\"\\ud800\"]",
			"[" + valid + ",\"\\udfff\"]",
			"[" + valid + ",\"\xff\"]",
		} {
			if err := validateSourceJSON([]byte(rejected)); err == nil {
				t.Fatalf("accepted ambiguous or lossy source JSON: %q", rejected)
			}
		}
		// Embedded NUL is valid JSON string data, not an identity terminator.
		distinct := "{" + quotedKey + ":" + quotedValue + "," + marshal(key+"\x00") + ":null}"
		if err := validateSourceJSON([]byte(distinct)); err != nil {
			t.Fatalf("collapsed distinct string identities: %q: %v", distinct, err)
		}
		canonicalVariants := "{" + marshal(key+"é") + ":null," + marshal(key+"e\u0301") + ":null}"
		if err := validateSourceJSON([]byte(canonicalVariants)); err != nil {
			t.Fatalf("normalized distinct source keys: %q: %v", canonicalVariants, err)
		}
	})
}
