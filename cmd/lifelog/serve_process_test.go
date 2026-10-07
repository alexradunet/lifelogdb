package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestExecutableServeDrainsActiveRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to another process is unsupported on Windows")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "lifelog")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	live := filepath.Join(root, "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "--db", live, "serve", "--addr", "127.0.0.1:0")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	startup, err := bufio.NewReader(stderr).ReadString('\n')
	if err != nil {
		t.Fatalf("server startup: %q, %v", startup, err)
	}
	_, address, ok := strings.Cut(strings.TrimSpace(startup), " on http://")
	if !ok {
		t.Fatalf("server startup: %q", startup)
	}
	connection, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		t.Fatal(err)
	}
	const body = "text=Completed+after+interrupt"
	if _, err := fmt.Fprintf(connection, "POST /days/2031-02-03/capture HTTP/1.1\r\nHost: %s\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\nExpect: 100-continue\r\nConnection: close\r\n\r\n", address, len(body)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusContinue {
		t.Fatalf("handler did not begin reading the request: %s", response.Status)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	// The listener closing is the observable shutdown barrier. Keep the admitted
	// request's body pending until then; no sleep assumes how fast shutdown runs.
	closedCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	for {
		probe, err := (&net.Dialer{}).DialContext(closedCtx, "tcp", address)
		if closedCtx.Err() != nil {
			t.Fatalf("server did not close its listener: %v", closedCtx.Err())
		}
		if err != nil {
			break
		}
		if err := probe.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := io.WriteString(connection, body); err != nil {
		t.Fatalf("shutdown discarded the admitted request: %v", err)
	}
	response, err = http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("shutdown closed the active request before its response: %v", err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("request completion: status=%s read=%v close=%v", response.Status, readErr, closeErr)
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatalf("server exit after successful drain: %v", err)
	}
	database, err := db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := &core.Store{DB: database}
	id, err := store.PageID(ctx, "2031-02-03")
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.PageByID(ctx, id)
	if err != nil || !strings.Contains(page.Body, "Completed after interrupt") {
		t.Fatalf("drained request was not saved: page=%+v error=%v", page, err)
	}
}
