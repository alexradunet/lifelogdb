package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeShutdownDeadlineCancelsBlockedRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started := make(chan struct{})
	type outcome struct{ read, context error }
	finished := make(chan outcome, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		_, err := io.Copy(io.Discard, r.Body)
		finished <- outcome{err, r.Context().Err()}
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- serveHTTP(ctx, srv, listener, 20*time.Millisecond) }()
	defer srv.Close()
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(connection, "POST / HTTP/1.1\r\nHost: %s\r\nContent-Length: 5\r\n\r\n", listener.Addr()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown error = %v; want expired drain deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked request prevented bounded shutdown")
	}
	select {
	case got := <-finished:
		if got.read == nil || !errors.Is(got.context, context.Canceled) {
			t.Fatalf("expired shutdown left the request running: read=%v context=%v", got.read, got.context)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expired shutdown did not release the blocked handler")
	}
}

func TestServeFailureJoinsShutdownWithoutContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	returned := make(chan error, 1)
	go func() { returned <- serveHTTP(context.Background(), &http.Server{}, listener, time.Second) }()
	select {
	case err := <-returned:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("serve error = %v; want closed listener", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server error left shutdown waiting for command cancellation")
	}
}
