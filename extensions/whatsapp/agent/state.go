package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// convo is today's conversation page as the extension last saw it. The file state.json keeps it after the
// extension's own last save, so the watcher can tell the extension's saves from the changes of the owner.
type convo struct {
	Title   string    `json:"title"`
	ID      string    `json:"id"`
	Version string    `json:"version"`
	Body    string    `json:"body"`
	SavedAt time.Time `json:"saved_at"`
}

// loadState reads the state file. A file that does not exist is no state.
func loadState(path string) (*convo, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("state file: %w", err)
	}
	var c convo
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("state file %s is not JSON: %w (delete it to start again)", path, err)
	}
	return &c, nil
}

// LastSave is the time of the extension's last save of a conversation page, and false when there is no state file.
func LastSave(path string) (time.Time, bool, error) {
	c, err := loadState(path)
	if err != nil || c == nil {
		return time.Time{}, false, err
	}
	return c.SavedAt, true, nil
}

// saveState writes the state file through a temporary file, so a crash leaves the old file or the new one.
func saveState(path string, c *convo) error {
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	tmp := filepath.Join(filepath.Dir(path), ".state.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	return nil
}
