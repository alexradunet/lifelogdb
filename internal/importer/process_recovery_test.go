package importer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// Each interrupted child holds its real database open at the boundary, announces
// readiness over a pipe, and is killed by its parent. No deferred rollback/Close
// runs. Separate children then recover and retry from disk. This is a process
// interruption test, not a power-loss test.
func TestImportProcessRecovery(t *testing.T) {
	for _, kind := range []string{"correction", "prepared", "selected", "vault"} {
		for _, boundary := range []string{"ready", "committed"} {
			t.Run(kind+"/"+boundary, func(t *testing.T) {
				var f *fixture
				var root int64
				switch kind {
				case "correction":
					f, root = importedMoodCorrectionFixture(t)
				case "vault":
					f = draftReceiptFixture(t)
					if _, _, err := f.s.Capture(ctx, "cli", "2031-04-12", "Existing journal", nil); err != nil {
						t.Fatal(err)
					}
				default:
					f = setup(t)
					f.approveRules(t, rulesBody)
					if kind == "prepared" {
						writeSource(t, f, "daily.csv", "Date,Distance (m)\n2020-01-02,10\n2020-01-03,20\n")
						mustLedger(t, f)
						registerMetrics(t, f, Metric{Name: "Distance", Unit: "m", Note: "Synthetic distance"})
						if _, err := f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); err != nil {
							t.Fatal(err)
						}
						if err := ownerApproves(f.w, preparedFile); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.WriteFile(filepath.Join(f.w.Source, "one.jpg"), syntheticSelectedJPEG(t), 0600); err != nil {
							t.Fatal(err)
						}
						mustLedger(t, f)
						if _, err := f.w.DraftSelectedPhoto(ctx, "one.jpg", "", "One.jpg", "Synthetic picture", PhotoChoices{Capture: "none", GPS: "none"}); err != nil {
							t.Fatal(err)
						}
						if err := ownerApproves(f.w, selectedPhotoFile); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := f.s.DB.Close(); err != nil {
					t.Fatal(err)
				}
				runRecoveryProcess(t, f.w.Dir, kind, boundary, "interrupt", root)
				if _, err := os.Stat(filepath.Join(f.w.Dir, "process-cleanup-ran")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("interrupted child ran cleanup: %v", err)
				}
				runRecoveryProcess(t, f.w.Dir, kind, boundary, "recover", root)
				runRecoveryProcess(t, f.w.Dir, kind, boundary, "retry", root)
			})
		}
	}
}

func runRecoveryProcess(t *testing.T, dir, kind, boundary, phase string, root int64) {
	t.Helper()
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestImportProcessRecoveryChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "LIFELOG_RECOVERY_DIR="+dir, "LIFELOG_RECOVERY_KIND="+kind,
		"LIFELOG_RECOVERY_BOUNDARY="+boundary, "LIFELOG_RECOVERY_PHASE="+phase, "LIFELOG_RECOVERY_ROOT="+strconv.FormatInt(root, 10))
	if phase != "interrupt" {
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s child: %v\n%s", phase, err, out)
		}
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, readErr := bufio.NewReader(stdout).ReadString('\n')
	if readErr != nil || line != "boundary ready\n" {
		cancel()
		waitErr := cmd.Wait()
		t.Fatalf("child never reached boundary: %q, read %v, wait %v\n%s", line, readErr, waitErr, diagnostic.String())
	}
	if err := cmd.Process.Kill(); err != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || childCtx.Err() != nil {
		t.Fatalf("child was not killed at boundary: %v (context %v)", err, childCtx.Err())
	}
}

func TestImportProcessRecoveryChild(t *testing.T) {
	dir := os.Getenv("LIFELOG_RECOVERY_DIR")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	kind, boundary, phase := os.Getenv("LIFELOG_RECOVERY_KIND"), os.Getenv("LIFELOG_RECOVERY_BOUNDARY"), os.Getenv("LIFELOG_RECOVERY_PHASE")
	root, err := strconv.ParseInt(os.Getenv("LIFELOG_RECOVERY_ROOT"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(w.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	f := &fixture{w: w, s: &core.Store{DB: d}, trial: w.TrialDB()}
	if phase == "interrupt" {
		defer func() {
			if err := os.WriteFile(filepath.Join(dir, "process-cleanup-ran"), []byte("cleanup"), 0600); err != nil {
				t.Error(err)
			}
		}()
		stop := func() error {
			if _, err := fmt.Fprintln(os.Stdout, "boundary ready"); err != nil {
				return err
			}
			var b [1]byte
			_, err := io.ReadFull(os.Stdin, b[:])
			return fmt.Errorf("parent failed to kill child: %w", err)
		}
		interruptImport(t, f, kind, boundary, root, stop)
		t.Fatal("interruption boundary returned")
	}
	if phase != "recover" && phase != "retry" {
		t.Fatalf("unknown child phase %q", phase)
	}
	if phase == "recover" {
		checkInterruptedImport(t, f, kind, boundary, root)
	}
	var before importRecoveryState
	if phase == "retry" {
		before = checkRecoveredImport(t, f, kind, root)
	}
	switch kind {
	case "correction":
		err = w.RecoverCorrections(ctx, f.s)
	case "prepared":
		_, err = w.Prepared(ctx, f.s, false)
	case "selected":
		_, err = w.SelectedPhoto(ctx, f.s, false)
	case "vault":
		_, err = w.ApplyVault(ctx, f.s)
	default:
		t.Fatalf("unknown import kind %q", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	after := checkRecoveredImport(t, f, kind, root)
	if phase == "retry" && !reflect.DeepEqual(before, after) {
		t.Fatalf("fresh-process retry changed identities or contents: before %+v after %+v", before, after)
	}
}

func interruptImport(t *testing.T, f *fixture, kind, boundary string, root int64, stop func() error) {
	t.Helper()
	var err error
	switch kind {
	case "correction":
		hook := func(correctionIntent) error { return stop() }
		if boundary == "ready" {
			correctionAfterReadyHook = hook
		} else {
			correctionAfterCommitHook = hook
		}
		value := 4.0
		_, _, err = f.w.CorrectImported(ctx, f.s, "cli", root, &value)
	case "prepared":
		if boundary == "ready" {
			var b *PreparedBatch
			b, err = f.w.readPrepared(ctx, true)
			if err == nil {
				err = f.w.bindPrepared(b, true)
			}
			if err == nil {
				err = stop()
			}
		} else {
			_, err = f.w.prepared(ctx, f.s, false, stop)
		}
	case "selected":
		if boundary == "ready" {
			var p *SelectedPhoto
			p, _, err = f.w.readSelectedPhoto(ctx, true)
			if err == nil {
				err = f.w.bindSelectedPhoto(p, true)
			}
			if err == nil {
				err = stop()
			}
		} else {
			_, err = f.w.selectedPhoto(ctx, f.s, false, stop)
		}
	case "vault":
		var p *Plan
		var ok bool
		p, ok, err = f.w.LoadPlan()
		if err == nil && !ok {
			t.Fatal("missing vault plan")
		}
		if err == nil {
			err = f.w.validatePlan(ctx, f.s, p)
		}
		if err == nil {
			_, err = f.w.applyPlanPublishing(ctx, f.s, p, true, func(path string, receipt *vaultIdentityReceipt) error {
				completed := false
				for _, n := range receipt.Notes {
					completed = completed || n.Applied
				}
				if boundary == "committed" && completed {
					return stop()
				}
				if err := publishVaultReceipt(path, receipt); err != nil {
					return err
				}
				if boundary == "ready" && !completed {
					return stop()
				}
				return nil
			})
		}
	default:
		t.Fatalf("unknown interruption kind %q", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func checkInterruptedImport(t *testing.T, f *fixture, kind, boundary string, root int64) {
	t.Helper()
	committed := boundary == "committed"
	switch kind {
	case "correction":
		rows, value, current := measurementRowsAndValue(t, f)
		wantRows, wantValue := 1, 3.0
		if committed {
			wantRows, wantValue = 2, 4
		}
		if rows != wantRows || value != wantValue || !current || readyIntentCount(t, f) != 1 {
			t.Fatalf("correction boundary: rows %d value %g current %v, want %d/%g with durable intent", rows, value, current, wantRows, wantValue)
		}
	case "prepared":
		want := 0
		if committed {
			want = 2
		}
		if n := importerMeasurementRows(t, f); n != want {
			t.Fatalf("prepared boundary: %d readings, want %d", n, want)
		}
		b, err := f.w.readPrepared(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(f.w.file(preparedBindingName(b))); err != nil {
			t.Fatal(err)
		}
		if done, err := f.w.preparedCompletion(b); err != nil || done {
			t.Fatalf("incomplete prepared reservation claimed completion: %v %v", done, err)
		}
	case "selected":
		var n int
		if err := f.s.DB.R.QueryRowContext(ctx, "SELECT count(*) FROM files").Scan(&n); err != nil {
			t.Fatal(err)
		}
		want := 0
		if committed {
			want = 1
		}
		if n != want {
			t.Fatalf("selected boundary: %d files, want %d", n, want)
		}
		p, _, err := f.w.readSelectedPhoto(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(f.w.file(selectedBindingName(p))); err != nil {
			t.Fatal(err)
		}
		if ps, err := f.w.appliedSelectedPhotos(ctx); err != nil || len(ps) != 0 {
			t.Fatalf("incomplete selected reservation claimed completion: %v %v", ps, err)
		}
	case "vault":
		id, err := f.s.PageID(ctx, "Recipes")
		if err != nil || (id != 0) != committed {
			t.Fatalf("vault boundary: Recipes id %d, committed %v, error %v", id, committed, err)
		}
		r, err := f.w.loadVaultReceipt()
		if err != nil || r == nil {
			t.Fatalf("missing prepared vault receipt: %+v %v", r, err)
		}
		for _, n := range r.Notes {
			if n.Applied {
				t.Fatal("incomplete vault receipt claimed completion")
			}
		}
	}
}

type importRecoveryState struct {
	Counts *core.Counts
	IDs    []int64
	Bodies []string
}

func checkRecoveredImport(t *testing.T, f *fixture, kind string, root int64) importRecoveryState {
	t.Helper()
	state := importRecoveryState{}
	var err error
	state.Counts, err = f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	switch kind {
	case "correction":
		rows, value, current := measurementRowsAndValue(t, f)
		if rows != 2 || value != 4 || !current {
			t.Fatalf("correction recovery rows/value/current: %d/%g/%v", rows, value, current)
		}
		var id, predecessor int64
		var source, key string
		if err := f.s.DB.R.QueryRowContext(ctx, "SELECT id,supersedes_id,source,import_key FROM measurements WHERE supersedes_id=?", root).Scan(&id, &predecessor, &source, &key); err != nil {
			t.Fatal(err)
		}
		intents, err := f.w.correctionIntents()
		if err != nil || len(intents) != 1 || key != intents[0].EventKey || source != "cli" || predecessor != root {
			t.Fatalf("correction provenance: source %q key %q parent %d intents %+v error %v", source, key, predecessor, intents, err)
		}
		state.IDs = []int64{root, id}
	case "prepared":
		readings, err := f.s.SeriesScope(ctx, "Distance", "2020-01-01", "2020-01-03", "unassociated", 0, false)
		if err != nil || len(readings) != 2 || readings[0].Day != "2020-01-02" || readings[0].Value != 10 || readings[1].Day != "2020-01-03" || readings[1].Value != 20 || importerMeasurementRows(t, f) != 2 {
			t.Fatalf("prepared readings %+v error %v", readings, err)
		}
		state.IDs = []int64{readings[0].ID, readings[1].ID}
		bs, err := f.w.appliedPrepared(ctx)
		if err != nil || len(bs) != 1 {
			t.Fatalf("prepared completion %+v %v", bs, err)
		}
		checkRecoveryLedger(t, f.w, "daily.csv")
	case "selected":
		var id int64
		var hash, mime, body string
		var preview int
		if err := f.s.DB.R.QueryRowContext(ctx, "SELECT f.id,f.sha256,f.mime,e.body,length(f.preview) FROM files f JOIN entities e ON e.id=f.id").Scan(&id, &hash, &mime, &body, &preview); err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(filepath.Join(f.w.Source, "one.jpg"))
		if err != nil {
			t.Fatal(err)
		}
		if hash != fmt.Sprintf("%x", sha256.Sum256(source)) || mime != "image/jpeg" || body != "Synthetic picture" || preview == 0 {
			t.Fatalf("selected identity/content: %q %q %q preview %d", hash, mime, body, preview)
		}
		var n int
		if err := f.s.DB.R.QueryRowContext(ctx, "SELECT count(*) FROM files").Scan(&n); err != nil || n != 1 {
			t.Fatalf("selected files %d %v", n, err)
		}
		state.IDs = []int64{id}
		ps, err := f.w.appliedSelectedPhotos(ctx)
		if err != nil || len(ps) != 1 {
			t.Fatalf("selected completion %+v %v", ps, err)
		}
		checkRecoveryLedger(t, f.w, "one.jpg")
	case "vault":
		for _, title := range []string{"Recipes", "2031-04-12"} {
			id, err := f.s.PageID(ctx, title)
			if err != nil || id == 0 {
				t.Fatalf("missing recovered %s: %d %v", title, id, err)
			}
			page, err := f.s.PageByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if title == "Recipes" && page.Body != vault["Recipes.md"] || title == "2031-04-12" && (!strings.HasPrefix(page.Body, "Existing journal") || strings.Count(page.Body, "Coffee with Cara.") != 1) {
				t.Fatalf("recovered %s body %q", title, page.Body)
			}
			state.IDs = append(state.IDs, id)
			state.Bodies = append(state.Bodies, page.Body)
		}
		r, err := f.w.loadVaultReceipt()
		if err != nil || r == nil || len(r.Notes) == 0 {
			t.Fatalf("recovered vault receipt %+v %v", r, err)
		}
		for _, n := range r.Notes {
			if !n.Applied {
				t.Fatal("vault receipt remains incomplete")
			}
		}
	}
	if integrity, err := f.s.Integrity(ctx); err != nil || !integrity.OK {
		t.Fatalf("recovered integrity %+v %v", integrity, err)
	}
	return state
}

func checkRecoveryLedger(t *testing.T, w *Workspace, file string) {
	t.Helper()
	lines, _, err := w.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if l.File == file && l.State == "x" {
			return
		}
	}
	t.Fatalf("%s not completed in ledger", file)
}
