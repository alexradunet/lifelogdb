package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestSnapshotNeverFollowsDestinationFileSymlinks(t *testing.T) {
	for _, occupied := range []string{"daily", "timestamp"} {
		t.Run(occupied, func(t *testing.T) {
			root := t.TempDir()
			live := filepath.Join(root, "life.db")
			if err := db.Init(live); err != nil {
				t.Fatal(err)
			}
			dir, repo := filepath.Join(root, "snapshots"), filepath.Join(root, "repo")
			for _, path := range []string{dir, filepath.Join(repo, ".git")} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			daily := filepath.Join(dir, "life-2031-02-03.db")
			timestamp := filepath.Join(dir, "life-2031-02-03T040506.db")
			leaf := daily
			if occupied == "timestamp" {
				if err := os.WriteFile(daily, []byte("existing snapshot sentinel"), 0o600); err != nil {
					t.Fatal(err)
				}
				leaf = timestamp
			}
			target := filepath.Join(repo, "private.db")
			if err := os.Symlink(target, leaf); err != nil {
				t.Skipf("cannot create test-owned file symlink: %v", err)
			}
			now := time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC)
			path, result, err := core.Snapshot(context.Background(), live, dir, now)
			if occupied == "daily" {
				if err != nil || result == nil || !result.OK {
					t.Errorf("occupied daily entry: path=%q result=%+v error=%v; want a valid timestamp snapshot", path, result, err)
				} else {
					assertSnapshotDestination(t, path, timestamp)
				}
			} else {
				if err == nil {
					t.Error("snapshot accepted an occupied timestamp entry")
				}
				if got := string(mustRead(t, daily)); got != "existing snapshot sentinel" {
					t.Errorf("existing snapshot changed: %q", got)
				}
			}
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("snapshot escaped into a git repository through the filename: %v", err)
			}
			if got, err := os.Readlink(leaf); err != nil || got != target {
				t.Errorf("snapshot changed the existing symlink: %q, %v", got, err)
			}
		})
	}
}
