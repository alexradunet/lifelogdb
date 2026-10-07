package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// This is workspace evidence of exact applied plan identity, not name history or a
// cryptographic seal against arbitrary filesystem writers. No database IDs or prose.
type vaultIdentityReceipt struct {
	Version int                             `json:"version"`
	Source  string                          `json:"source"`
	Notes   map[string]vaultPlannedIdentity `json:"notes"`
}
type vaultPlannedIdentity struct {
	Title   string `json:"title"`
	Day     string `json:"day"`
	Applied bool   `json:"applied"`
}

func (w *Workspace) vaultReceiptPath() string { return w.file("applied-plan.json") }

func (w *Workspace) loadVaultReceipt() (*vaultIdentityReceipt, error) {
	f, err := os.Open(w.vaultReceiptPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read applied plan identity: %w", err)
	}
	b, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, fmt.Errorf("read applied plan identity: %w", err)
	}
	if len(b) > 8<<20 {
		return nil, refuse("applied plan identity receipt is too large")
	}
	var r vaultIdentityReceipt
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return nil, refuse("applied plan identity receipt is malformed")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, refuse("applied plan identity receipt has trailing data")
	}
	source, err := w.Name()
	if err != nil {
		return nil, err
	}
	if r.Version != 1 || r.Source != source || r.Notes == nil {
		return nil, refuse("applied plan identity receipt has wrong source or format")
	}
	for path, n := range r.Notes {
		if path == "" || !text.ValidTitle(n.Title) || n.Day != "" && !core.IsDay(n.Day) {
			return nil, refuse("applied plan identity receipt contains an invalid identity")
		}
	}
	return &r, nil
}

func checkVaultReceiptIdentity(t *core.Tx, n *Note, r *vaultIdentityReceipt, requireApplied bool) error {
	_, day, found, err := t.ImportedPageIdentity(n.Path)
	if err != nil {
		return err
	}
	var original vaultPlannedIdentity
	recorded := false
	if r != nil {
		original, recorded = r.Notes[n.Path]
	}
	// Appends have no per-source entity row. A prepared append with a lost completion
	// marker is indeterminate, not permission to change its prepared identity.
	bound := found || n.Appended || recorded && (n.Action == "append" || original.Applied)
	if requireApplied && (!recorded || !original.Applied || !found && !(n.Action == "append" && n.Appended)) {
		return refuse("%s: apply the vault plan to the trial first, then review and replay", n.Path)
	}
	if !bound {
		return nil
	}
	if !recorded {
		return refuse("%s: applied note has no original identity receipt; explicit workspace disposition is required", n.Path)
	}
	if original.Title != n.Title || original.Day != n.Day {
		return refuse("%s: applied plan title/day conflicts with its original identity receipt", n.Path)
	}
	if found {
		sameDay := day == nil && n.Day == "" || day != nil && *day == n.Day
		id, err := t.ByImportKey(n.Path)
		if err != nil {
			return err
		}
		p, err := t.Lookup(original.Title)
		if err != nil {
			return err
		}
		if !sameDay || p == nil || p.ID != id {
			return refuse("%s: applied page identity conflicts with the plan's original name/day", n.Path)
		}
	}
	return nil
}

// Called under the workspace lock and BEGIN IMMEDIATE after source reads/validation.
// The small atomic receipt write is deliberately inside the transaction: re-read
// evidence and row bindings together, so a receipt error prevents entity writes.
// Receipt preparation and the SQLite commit are NOT a jointly atomic durability unit.
func publishVaultReceipt(path string, r *vaultIdentityReceipt) error { return writeJSON(path, r) }

func (w *Workspace) prepareVaultReceipt(t *core.Tx, p *Plan) error {
	return w.prepareVaultReceiptPublishing(t, p, publishVaultReceipt)
}

func (w *Workspace) prepareVaultReceiptPublishing(t *core.Tx, p *Plan, publish func(string, *vaultIdentityReceipt) error) error {
	r, err := w.loadVaultReceipt()
	if err != nil {
		return err
	}
	if r == nil {
		source, err := w.Name()
		if err != nil {
			return err
		}
		r = &vaultIdentityReceipt{Version: 1, Source: source, Notes: map[string]vaultPlannedIdentity{}}
	}
	changed := false
	for i := range p.Notes {
		n := &p.Notes[i]
		if err := checkVaultReceiptIdentity(t, n, r, false); err != nil {
			return err
		}
		_, _, found, err := t.ImportedPageIdentity(n.Path)
		if err != nil {
			return err
		}
		existing, recorded := r.Notes[n.Path]
		if recorded && existing.Applied && !found && n.Action != "append" {
			return refuse("%s: completed receipt has no corresponding trial identity", n.Path)
		}
		if found || n.Appended || n.Action == "append" && recorded {
			continue
		}
		want := vaultPlannedIdentity{Title: n.Title, Day: n.Day}
		if !recorded || existing != want {
			r.Notes[n.Path] = want
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return publish(w.vaultReceiptPath(), r)
}

// Applied is historical completion of this exact binding's whole ApplyVault pass,
// not current-source/body equality, owner approval, or filesystem/SQLite atomicity.
func (w *Workspace) completeVaultReceipt(t *core.Tx, p *Plan, publish func(string, *vaultIdentityReceipt) error) error {
	r, err := w.loadVaultReceipt()
	if err != nil {
		return err
	}
	if r == nil {
		return refuse("completed vault pass has no original identity receipt")
	}
	changed := false
	for i := range p.Notes {
		n := &p.Notes[i]
		if err := checkVaultReceiptIdentity(t, n, r, false); err != nil {
			return err
		}
		_, _, found, err := t.ImportedPageIdentity(n.Path)
		if err != nil {
			return err
		}
		original, recorded := r.Notes[n.Path]
		if !recorded || !found && !(n.Action == "append" && n.Appended) {
			return refuse("%s: vault pass has incomplete trial identity evidence", n.Path)
		}
		if !original.Applied {
			original.Applied = true
			r.Notes[n.Path] = original
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return publish(w.vaultReceiptPath(), r)
}

// Replay/rehearsal validate evidence against the authoritative trial, never the
// disposable/fresh target. The returned plan snapshot is the only plan replay uses.
func (w *Workspace) vaultReplayPlan(ctx context.Context, trial *core.Store, required bool) (*Plan, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, ok, err := w.LoadPlan()
	if err != nil || !ok {
		return nil, err
	}
	if err := w.validatePlan(ctx, trial, p); err != nil {
		return nil, err
	}
	r, err := w.loadVaultReceipt()
	if err != nil {
		return nil, err
	}
	source, err := w.Name()
	if err != nil {
		return nil, err
	}
	err = trial.DryRun(ctx, source, func(t *core.Tx) error {
		for i := range p.Notes {
			if err := checkVaultReceiptIdentity(t, &p.Notes[i], r, required); err != nil {
				return err
			}
		}
		return nil
	})
	return p, err
}
