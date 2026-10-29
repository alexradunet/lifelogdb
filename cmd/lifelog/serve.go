package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/db"
)

// publicOptions reads the --public flags (docs/plans/078-network-server.md): nil without --public, when the
// flags that belong to it are refused rather than ignored; with it, the token from --token-file or $LIFELOG_TOKEN
// is required before any listener opens.
func publicOptions(o opts) (*api.PublicOptions, error) {
	if !o.serve.public {
		switch {
		case o.serve.tokenFile != "":
			return nil, errors.New("--token-file needs --public")
		case len(o.serve.allowOrigins) > 0:
			return nil, errors.New("--allow-origin needs --public")
		case o.serve.tlsCert != "" || o.serve.tlsKey != "":
			return nil, errors.New("--tls-cert and --tls-key need --public")
		}
		return nil, nil
	}
	token := o.token
	if o.serve.tokenFile != "" {
		b, err := os.ReadFile(o.serve.tokenFile)
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
	for _, origin := range o.serve.allowOrigins {
		if err := api.CheckOrigin(origin); err != nil {
			return nil, err
		}
	}
	if (o.serve.tlsCert == "") != (o.serve.tlsKey == "") {
		return nil, errors.New("--tls-cert and --tls-key go together")
	}
	return &api.PublicOptions{Token: token, Origins: o.serve.allowOrigins, TLS: o.serve.tlsCert != ""}, nil
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
// snapshot folder that is not one refuses the command with life.db untouched. It gives the line with the address
// checked and the public options set.
func prepareServe(e env, o opts) (opts, error) {
	pub, err := publicOptions(o)
	if err != nil {
		return o, err
	}
	if pub == nil {
		o.serve.addr, err = api.LoopbackAddress(o.serve.addr)
	} else {
		if !o.serve.addrExplicit {
			o.serve.addr = "0.0.0.0:7777"
		}
		o.serve.addr, err = api.PublicAddress(o.serve.addr)
	}
	if err != nil {
		return o, err
	}
	o.serve.pub = pub
	if len(o.serve.with) > 0 && o.serve.tlsCert != "" {
		return o, errors.New("--with and --tls-cert cannot go together: the extension would have to trust the certificate of the server")
	}
	if len(o.serve.with) > 0 {
		exe, err := program(e)
		if err != nil {
			return o, err
		}
		for i, name := range o.serve.with {
			if slices.Contains(o.serve.with[:i], name) { // two flags that name one thing twice are refused
				return o, fmt.Errorf("--with %s is given twice: one child runs an extension", name)
			}
			path, err := findExtension(exe, name)
			if err != nil {
				return o, fmt.Errorf("--with %s: %w", name, err)
			}
			o.serve.withPaths = append(o.serve.withPaths, path)
		}
	}
	if o.url != "" {
		return o, errors.New("serve opens the file itself: drop --url")
	}
	if o.snapshots != "" {
		if _, err := db.CheckSnapshotDir(o.snapshots); err != nil {
			return o, err
		}
	}
	return o, nil
}

// serve is lifelog serve: the handler on a socket, this machine only or --public, until an interrupt drains it. serve
// builds the handler of its listener itself: the local one takes only a request to its own loopback authority, and a
// public one serves the API behind the token, the allowed origins and the login pages (api.Public). So no code in
// another function decides what guards a public listener.
func serve(ctx context.Context, e env, args []string) error {
	if len(args) != 0 {
		return errors.New(e.cmd.synopsis())
	}
	o, pub := e.o, e.o.serve.pub
	listener, err := net.Listen("tcp", o.serve.addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	var networkHandler http.Handler
	scheme, note := "http", ""
	if pub == nil {
		networkHandler, err = api.NetworkAuthority(listener.Addr(), api.New(e.store))
	} else {
		if networkHandler, err = api.Public(e.store, *pub); err != nil {
			return err
		}
		note = " (public: token required; the token travels in clear, use a private network)"
		if pub.TLS {
			scheme, note = "https", " (public: token required)"
			if listener, err = tlsListener(listener, o.serve.tlsCert, o.serve.tlsKey); err != nil {
				return err
			}
		}
	}
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: listener.Addr().String(), Handler: networkHandler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	// The file's own user_version, not the constant of this program: the line shows which schema the process serves.
	var userVersion int64
	if err := e.store.DB.R.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	fmt.Fprintf(e.stderr, "lifelog: serving %s on %s://%s%s (version %s, user_version %d)\n", o.db, scheme, listener.Addr(), note, version, userVersion)
	// The children end before the server drains: an extension may still write while it ends (extensions/README.md).
	serveCtx, waitWith := startWith(ctx, e, listener.Addr())
	err = serveHTTP(serveCtx, srv, listener, 10*time.Second)
	waitWith()
	// After the drain, before the database closes: the log shows that the stop was clean, or why it was not.
	if err != nil {
		fmt.Fprintf(e.stderr, "lifelog: stopped serving %s with an error: %v\n", o.db, err)
	} else {
		fmt.Fprintf(e.stderr, "lifelog: stopped serving %s\n", o.db)
	}
	return err
}
