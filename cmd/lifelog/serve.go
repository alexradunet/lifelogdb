package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/db"
)

// publicOptions reads the --public flags (docs/plans/078-network-server.md): nil without --public, when the
// flags that belong to it are refused rather than ignored; with it, the token from --token-file or $LIFELOG_TOKEN
// is required before any listener opens.
func publicOptions(o opts) (*api.PublicOptions, error) {
	if !o.public {
		switch {
		case o.tokenFile != "":
			return nil, errors.New("--token-file needs --public")
		case len(o.allowOrigins) > 0:
			return nil, errors.New("--allow-origin needs --public")
		case o.tlsCert != "" || o.tlsKey != "":
			return nil, errors.New("--tls-cert and --tls-key need --public")
		}
		return nil, nil
	}
	token := os.Getenv("LIFELOG_TOKEN")
	if o.tokenFile != "" {
		b, err := os.ReadFile(o.tokenFile)
		if err != nil {
			return nil, fmt.Errorf("token file: %w", err)
		}
		token = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	}
	if token == "" {
		return nil, errors.New("--public needs a token: --token-file PATH (one line, 32 characters or more) or $LIFELOG_TOKEN")
	}
	if err := api.CheckToken(token); err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	for _, origin := range o.allowOrigins {
		if err := api.CheckOrigin(origin); err != nil {
			return nil, err
		}
	}
	if (o.tlsCert == "") != (o.tlsKey == "") {
		return nil, errors.New("--tls-cert and --tls-key go together")
	}
	return &api.PublicOptions{Token: token, Origins: o.allowOrigins, TLS: o.tlsCert != ""}, nil
}

// tlsListener serves TLS on a public listener with the owner's certificate; the standard library does the rest.
func tlsListener(l net.Listener, certFile, keyFile string) (net.Listener, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	return tls.NewListener(l, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}), nil
}

// serveHTTP owns shutdown until admitted requests finish or the drain deadline
// expires. The caller must not close their database while Shutdown is still running.
func serveHTTP(ctx context.Context, srv *http.Server, listener net.Listener, drainTimeout time.Duration) error {
	stopped := make(chan struct{})
	drained := make(chan struct{})
	var drainErr error
	go func() {
		defer close(drained)
		select {
		case <-ctx.Done():
		case <-stopped:
		}
		// An interrupt stops new work but lets admitted HTTP requests finish. This
		// separate, bounded lifetime preserves command context values and is joined below.
		drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
		defer cancel()
		err := srv.Shutdown(drainCtx)
		if err != nil {
			// Expiry closes connections and cancels their request contexts instead of
			// leaving a blocked upload or reader alive after the server has returned.
			err = errors.Join(err, srv.Close())
		}
		drainErr = err
	}()
	err := srv.Serve(listener)
	close(stopped)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	<-drained
	return errors.Join(err, drainErr)
}

// prepareServe reads the --public flags and the address before anything opens: a wrong flag, a wrong address or a
// snapshot folder that is not one refuses the command with life.db untouched.
func prepareServe(o *opts) error {
	pub, err := publicOptions(*o)
	if err != nil {
		return err
	}
	if pub == nil {
		o.addr, err = api.LoopbackAddress(o.addr)
	} else {
		if !o.addrExplicit {
			o.addr = "0.0.0.0:7777"
		}
		o.addr, err = api.PublicAddress(o.addr)
	}
	if err != nil {
		return err
	}
	o.pub = pub
	if o.url != "" {
		return errors.New("serve opens the file itself: drop --url")
	}
	if o.snapshots != "" {
		if _, err := db.CheckSnapshotDir(o.snapshots); err != nil {
			return err
		}
	}
	return nil
}

// serve is lifelog serve: the handler on a socket, this machine only or --public, until an interrupt drains it.
func serve(ctx context.Context, e env, _ []string) error {
	o, pub := e.o, e.o.pub
	listener, err := net.Listen("tcp", o.addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	var networkHandler http.Handler
	scheme, note := "http", ""
	if pub == nil {
		networkHandler, err = api.NetworkAuthority(listener.Addr(), e.handler)
	} else {
		networkHandler, err = api.Public(e.handler, *pub)
		note = " (public: token required; the token travels in clear, use a private network)"
		if pub.TLS {
			scheme, note = "https", " (public: token required)"
			if listener, err = tlsListener(listener, o.tlsCert, o.tlsKey); err != nil {
				return err
			}
		}
	}
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: listener.Addr().String(), Handler: networkHandler, ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(os.Stderr, "lifelog: serving %s on %s://%s%s\n", o.db, scheme, listener.Addr(), note)
	return serveHTTP(ctx, srv, listener, 10*time.Second)
}
