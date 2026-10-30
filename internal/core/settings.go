package core

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Settings are the settings of the application: a file beside life.db, not a part of the database. The key is a
// secret and a snapshot copies the database, so the settings stay out of it. Each setting is added when a feature
// needs it (README: the settings file).
type Settings struct {
	ModelURL string `json:"model_url,omitempty"` // the base URL of a server that speaks the chat-completions protocol, ending in /v1
	Model    string `json:"model,omitempty"`     // the model id
	ModelKey string `json:"model_key,omitempty"` // a bearer credential, when the server needs one
	// OwnerNumber is the digits of the owner's phone number with the country code: the one number that the agent on
	// the phone answers. The action settings refuses a value that is not digits.
	OwnerNumber string `json:"owner_number,omitempty"`
	// Ground is the floor of the browser: boards, oak, firelight or embers. Empty is boards. The catalog lists the
	// four values, and the action settings refuses any other.
	Ground string `json:"ground,omitempty"`
	// LockAfter is the idle minutes after which the public listener locks a browser (docs/plans/160-the-lock.md): a
	// number of minutes as text. Empty is off. The catalog lists the values, and the action settings refuses any other.
	LockAfter string `json:"lock_after,omitempty"`
	// LockPINHash is the stored form of the PIN that opens a locked browser (HashPIN). The file never holds the PIN.
	LockPINHash string `json:"lock_pin_hash,omitempty"`
}

// LockMinutes is the idle time of the lock in minutes; 0 means that the lock is off. The lock is on only when a PIN
// is stored: without one, nothing could open a locked browser.
func (v Settings) LockMinutes() int {
	n, err := strconv.Atoi(v.LockAfter)
	if err != nil || n <= 0 || v.LockPINHash == "" {
		return 0
	}
	return n
}

// pinIterations is the cost of one PBKDF2 try. A PIN has few values, so the cost protects little if the file is
// read; the limit of tries at the public listener protects the PIN.
const pinIterations = 200_000

// HashPIN is the stored form of pin: "pbkdf2-sha256$ITERATIONS$SALT$KEY", with the salt and the key in base64. The
// salt and the count are in the text, so a later version can change the count and still read an old hash.
func HashPIN(pin string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("hash the PIN: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, pin, salt, pinIterations, 32)
	if err != nil {
		return "", fmt.Errorf("hash the PIN: %w", err)
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pinIterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// PINMatches reports whether pin is the PIN that stored (from HashPIN) was made from. A stored text that is not
// of that form matches nothing.
func PINMatches(stored, pin string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 1 || n > 10_000_000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pin, salt, n, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// SettingsPathFor is the settings file of the database at dbPath: the path with .db cut and .settings.json added
// (life.db gives life.settings.json). A path that does not end in .db gets the suffix added whole.
func SettingsPathFor(dbPath string) string {
	return strings.TrimSuffix(dbPath, ".db") + ".settings.json"
}

var errNoSettingsPath = &Error{Status: http.StatusInternalServerError, Msg: "this store has no settings file: SettingsPath is empty"}

// ReadSettings reads the settings file. A file that does not exist gives empty settings.
func (s *Store) ReadSettings() (Settings, error) {
	var v Settings
	if s.SettingsPath == "" {
		return v, errNoSettingsPath
	}
	b, err := os.ReadFile(s.SettingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, fmt.Errorf("read the settings: %w", err)
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Settings{}, fmt.Errorf("read the settings %s: %w", s.SettingsPath, err)
	}
	return v, nil
}

// WriteSettings writes the settings file with mode 0600: a temporary file in the same folder, then a rename, so a
// reader never sees half a file.
func (s *Store) WriteSettings(v Settings) (err error) {
	if s.SettingsPath == "" {
		return errNoSettingsPath
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.SettingsPath), ".settings-*.tmp") // CreateTemp makes the file with mode 0600
	if err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	if err = os.Rename(f.Name(), s.SettingsPath); err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	return nil
}
