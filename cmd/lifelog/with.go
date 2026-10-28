package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// restartDelay is the wait before serve starts a failed extension again (extensions/README.md, run by serve). A test
// shortens it.
var restartDelay = 10 * time.Second

// startWith starts a child for each --with of the line, after the listener is up, and gives the context that
// serveHTTP follows. That context ends only after the children ended: on ctx.Done the children get an interrupt and
// are awaited, then the server drains. wait ends the children (if serveHTTP ended by itself) and joins every
// goroutine. With no --with, the context is ctx and wait does nothing.
func startWith(ctx context.Context, e env, listen net.Addr) (serveCtx context.Context, wait func()) {
	paths := e.o.serve.withPaths
	if len(paths) == 0 {
		return ctx, func() {}
	}
	url := "http://127.0.0.1:" + portOf(listen)
	childCtx, endChildren := context.WithCancel(ctx)
	serveCtx, endServe := context.WithCancel(context.WithoutCancel(ctx))
	var children sync.WaitGroup
	for i, name := range e.o.serve.with {
		children.Add(1)
		go func() {
			defer children.Done()
			superviseWith(childCtx, e, name, paths[i], url)
		}()
	}
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		<-childCtx.Done()
		children.Wait()
		endServe()
	}()
	return serveCtx, func() {
		endChildren()
		<-joined
		endServe()
	}
}

// portOf is the port of a listener address. The child reaches a listener on every interface on the loopback.
func portOf(a net.Addr) string {
	if t, ok := a.(*net.TCPAddr); ok {
		return strconv.Itoa(t.Port)
	}
	_, port, _ := net.SplitHostPort(a.String())
	return port
}

// withEnv is the environment of a child of serve: the one of any extension, and the token of a public listener. A
// LIFELOG_TOKEN that the child could inherit is removed: the child gets the token of this listener or none.
func withEnv(e env, exe, file, url string) []string {
	env := slices.DeleteFunc(extensionEnv(os.Environ(), exe, file, url), func(kv string) bool {
		return strings.HasPrefix(kv, "LIFELOG_TOKEN=")
	})
	if pub := e.o.serve.pub; pub != nil {
		env = append(env, "LIFELOG_TOKEN="+pub.Token)
	}
	return env
}

// superviseWith runs the extension at path until it ends for good or ctx ends, by the rule of extensions/README.md:
// exit 0 is an end by its own choice; 2 and 3 want the owner first; any other end starts it again after restartDelay.
func superviseWith(ctx context.Context, e env, name, path, url string) {
	say := func(format string, a ...any) {
		fmt.Fprintf(e.stderr, "lifelog: with %s: %s\n", name, fmt.Sprintf(format, a...))
	}
	exe, err := program(e)
	if err == nil {
		var file string
		if file, err = filepath.Abs(e.o.db); err == nil {
			err = runWith(ctx, e, path, withEnv(e, exe, file, url), say)
		}
	}
	if err != nil {
		say("not run: %v", err)
	}
}

func runWith(ctx context.Context, e env, path string, env []string, say func(string, ...any)) error {
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer stdin.Close()
	for {
		again, err := runWithOnce(ctx, e, path, env, stdin, say)
		if err != nil {
			say("not started: %v, started again in 10 s", err)
		} else if !again {
			return nil
		}
		t := time.NewTimer(restartDelay)
		select {
		case <-ctx.Done():
			t.Stop()
			say("ended")
			return nil
		case <-t.C:
		}
	}
}

// runWithOnce runs the extension once and says how it ended. again tells whether the rule starts it again.
func runWithOnce(ctx context.Context, e env, path string, env []string, stdin *os.File, say func(string, ...any)) (again bool, err error) {
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = stdin, e.stdout, e.stderr, env
	interruptOnCancel(cmd)
	if err := cmd.Start(); err != nil {
		return false, err
	}
	say("started (pid %d)", cmd.Process.Pid)
	err = cmd.Wait()
	if ctx.Err() != nil || err == nil { // the interrupt of the stop, or the extension's own choice
		say("ended")
		return false, nil
	}
	status := -1 // a signal ended it, or Wait failed
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		status = ee.ExitCode()
	}
	if status == 2 || status == 3 {
		say("ended with status %d: not started again (the owner acts first)", status)
		return false, nil
	}
	say("ended with status %d, started again in 10 s", status)
	return true, nil
}
