// Package app is what a connected app serves its catalog with. A connected app is an extension that `lifelog serve
// --with NAME` runs and that serves its own Siren catalog on a loopback address; the server merges the actions into its
// own. The package serves and never dials: the contract is in extensions/README.md.
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"lifelog/siren"
)

// ExitOwnerActs is the exit status of a connected app that cannot go on until the owner acts (for example, pairs a
// device). `serve` does not start it again (extensions/README.md, "Run by serve").
const ExitOwnerActs = 2

const (
	listenEnv      = "LIFELOG_LISTEN"
	headerTimeout  = 10 * time.Second
	shutdownDrain  = 10 * time.Second
	sirenMediaType = "application/vnd.siren+json"
)

// Listen serves h on the loopback address that `serve` gives in LIFELOG_LISTEN, until ctx ends. Then it drains the
// requests in progress for 10 seconds at most. It returns nil on a clean end. It refuses an address that is not a
// loopback address: a connected app is reachable by the server alone (extensions/README.md).
func Listen(ctx context.Context, h http.Handler) error {
	addr := os.Getenv(listenEnv)
	if addr == "" {
		return fmt.Errorf("%s is not set: run this program under `lifelog serve --with <name>`", listenEnv)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s=%q is not host:port: %w", listenEnv, addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s=%q: a connected app listens on a loopback address only", listenEnv, addr)
	}
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: headerTimeout}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err := <-done: // the server failed before the end of ctx
		return err
	case <-ctx.Done():
	}
	drain, cancel := context.WithTimeout(context.Background(), shutdownDrain) // ctx has ended: the drain needs its own
	defer cancel()
	if err := srv.Shutdown(drain); err != nil {
		return err
	}
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Error is an error of a handler with the status and the code that the client gets. Any other error that a handler
// returns is answered as 500 with the code internal, and its text is not sent.
type Error struct {
	Status int
	Code   string
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

// Fail writes an error entity. It has the same class and properties as the error entity of the API, so a client
// treats both alike.
func Fail(w http.ResponseWriter, status int, code, msg string) {
	e := &siren.Entity{
		Class:      []string{"error"},
		Title:      "Error",
		Properties: map[string]any{"status": status, "code": code, "message": msg},
		Links:      []siren.Link{{Rel: []string{"index"}, Href: "/", Title: "Home"}},
	}
	write(w, status, e)
}

func write(w http.ResponseWriter, status int, e *siren.Entity) {
	w.Header().Set("Content-Type", sirenMediaType)
	w.WriteHeader(status)
	_ = siren.WriteJSON(w, e, "") // a failed write means the client went away
}

// Catalog is the handler of a connected app. It serves GET /actions (the actions with their templated hrefs), GET /
// (the entity that home returns) and one route for each action: its method and its href as a net/http pattern, so
// {id} in an href is a path field. run holds the handler of each action by its name. Each handler returns an entity,
// sent with status 200, or an error (see Error). A POST that returns an entity with a self link also gets a Location
// header. Catalog panics when an action has no handler or a handler has no action: this is a fault of the program, and
// it must show at the start.
func Catalog(title string, home func(*http.Request) (*siren.Entity, error), actions []siren.Action,
	run map[string]func(*http.Request) (*siren.Entity, error)) http.Handler {
	named := map[string]bool{}
	for _, a := range actions {
		if run[a.Name] == nil {
			panic(fmt.Sprintf("app.Catalog: the action %q has no handler", a.Name))
		}
		named[a.Name] = true
	}
	for name := range run {
		if !named[name] {
			panic(fmt.Sprintf("app.Catalog: the handler %q has no action", name))
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		Fail(w, http.StatusNotFound, "not_found", "no such resource")
	})
	mux.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, &siren.Entity{
			Class:   []string{"actions"},
			Title:   "Actions",
			Links:   []siren.Link{{Rel: []string{"self"}, Href: "/actions", Title: "Actions"}, {Rel: []string{"index"}, Href: "/", Title: "Home"}},
			Actions: actions,
		})
	})
	mux.HandleFunc("GET /{$}", serve(func(r *http.Request) (*siren.Entity, error) {
		e, err := home(r)
		if e != nil && e.LinkByRel("self") == nil {
			e.Links = append(e.Links, siren.Link{Rel: []string{"self"}, Href: "/", Title: title})
		}
		return e, err
	}))
	for _, a := range actions {
		mux.HandleFunc(a.Method+" "+a.Href, serve(run[a.Name]))
	}
	return mux
}

// serve turns a handler that returns an entity into an http.HandlerFunc.
func serve(f func(*http.Request) (*siren.Entity, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e, err := f(r)
		var ae *Error
		switch {
		case errors.As(err, &ae):
			Fail(w, ae.Status, ae.Code, ae.Msg)
			return
		case err != nil || e == nil:
			Fail(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		if self := e.LinkByRel("self"); r.Method == http.MethodPost && self != nil {
			w.Header().Set("Location", self.Href)
		}
		write(w, http.StatusOK, e)
	}
}

// StateDir is the state folder of a connected app: os.UserConfigDir()/lifelog-NAME. It creates the folder with mode
// 0700 when it is missing. A unit with a read-only home lists this folder as writable (extensions/README.md).
func StateDir(name string) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "lifelog-"+name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
