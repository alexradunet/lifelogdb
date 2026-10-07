package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// PreparedSource is a source-derived review result, not permission to apply writes.
// It contains no trial IDs. The source namespace and target mappings belong to owner-reviewed application.
type PreparedSource struct {
	Version      int                `json:"version"`
	Profile      string             `json:"profile"`
	File         string             `json:"file"`
	SourceSHA256 string             `json:"source_sha256"`
	FitBinding   *FitSessionBinding `json:"fit_binding,omitempty"`
	SourceProfile
}

// PrepareSupportedSource reads the selected confined snapshot and derives the fixed-profile review data.
// No database, approval stamp, ledger or side-effecting operation is performed here.
func (w *Workspace) PrepareSupportedSource(ctx context.Context, file, profile string, binding *FitSessionBinding) (*PreparedSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return nil, refuse("cannot open selected source")
	}
	defer root.Close()
	path, err := w.sourcePath(root, file)
	if err != nil {
		return nil, refuse("selected source is not confined")
	}
	fh, err := root.Open(path)
	if err != nil {
		return nil, refuse("cannot read selected source")
	}
	data, readErr := io.ReadAll(io.LimitReader(fh, maxProfileBytes+1))
	closeErr := fh.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil || closeErr != nil {
		return nil, refuse("cannot read selected source")
	}
	result, err := decodeSourceProfile(profile, data, binding)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	var copied *FitSessionBinding
	if binding != nil {
		b := *binding
		copied = &b
	}
	return &PreparedSource{Version: 1, Profile: profile, File: file, SourceSHA256: hex.EncodeToString(digest[:]), FitBinding: copied, SourceProfile: *result}, nil
}
