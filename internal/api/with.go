package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"lifelog/client"
	"lifelog/internal/api/view"
	"lifelog/internal/core"
	"lifelog/siren"
)

// A connected app (docs/plans/154-connected-apps-through-the-catalog.md) is a program that `lifelog serve --with NAME`
// runs. It serves its own Siren catalog on a loopback address. This file puts its actions in the catalog of the
// server, under the prefix NAME-, and forwards the requests to /with/NAME to it. The server names no app and no
// domain: the catalog of the app is the whole interface.

// forwardTimeout is the longest wait for a connected app to answer one forwarded request. It is less than the limit
// of the request itself (defaultLimits), so an app that is slow gives the request's 504 timeout and not a cut
// connection. It is a variable so that a test can shorten it.
var forwardTimeout = 30 * time.Second

// appNameRE is the grammar of the name of an action of an app. With the prefix NAME- it is the name of an action of
// the merged catalog.
var appNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// Apps is the registry of the connected apps of one server. The local handler and the public one share one value, so
// both see the same apps. It is safe for use by many goroutines. Make it with NewApps.
type Apps struct {
	mu   sync.RWMutex
	apps map[string]liveApp // by name; a name that is here is known
}

// liveApp is a connected app that the server knows. c is nil while the app does not run.
type liveApp struct {
	c       *client.Client
	title   string
	actions []siren.Action
}

// NewApps returns a registry with no app.
func NewApps() *Apps { return &Apps{apps: map[string]liveApp{}} }

// Expect says that the app NAME exists, before it runs. Until Set, and after Remove, a request to it is 503
// unavailable.
func (a *Apps) Expect(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.apps[name]; !ok {
		a.apps[name] = liveApp{}
	}
}

// Set says that the app NAME runs at base (http://127.0.0.1:PORT), with its home title and its catalog. It refuses
// the whole catalog, and the app stays as it was, when: the name of an action is not a-z 0-9 and -; the href of an
// action is not a path (it does not start with one slash, or it has a scheme, a host, a query or a fragment); the
// name of an action with the prefix NAME- is the name of an action of the static catalog or of another running app;
// or an action has a field of type file. This version of the server does not forward a multipart body.
func (a *Apps) Set(name, base, title string, actions []siren.Action) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, act := range actions {
		if err := checkAppAction(act); err != nil {
			return fmt.Errorf("the app %s: %w", name, err)
		}
		taken := name + "-" + act.Name
		if _, ok := lookup(taken); ok {
			return fmt.Errorf("the app %s: the action %s is in the catalog already", name, taken)
		}
		for other, o := range a.apps {
			if other == name {
				continue
			}
			for _, x := range o.actions {
				if other+"-"+x.Name == taken {
					return fmt.Errorf("the app %s: the action %s is an action of the app %s", name, taken, other)
				}
			}
		}
	}
	a.apps[name] = liveApp{c: client.Remote(base, "", ""), title: title, actions: slices.Clone(actions)}
	return nil
}

// checkAppAction refuses an action that Set does not take, with the reason.
func checkAppAction(act siren.Action) error {
	if !appNameRE.MatchString(act.Name) {
		return fmt.Errorf("the action name %q: use a-z, 0-9 and -", act.Name)
	}
	u, err := url.Parse(act.Href)
	if err != nil || !isPath(act.Href) || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("the action %s: the href %q is not a path", act.Name, act.Href)
	}
	for _, f := range act.Fields {
		if f.Type == "file" {
			return fmt.Errorf("the action %s: the field %s is a file, and a file is not forwarded", act.Name, f.Name)
		}
	}
	return nil
}

// Remove says that the app NAME does not run. Its name stays known.
func (a *Apps) Remove(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.apps[name]; ok {
		a.apps[name] = liveApp{}
	}
}

// Names are the names of the apps that run, sorted.
func (a *Apps) Names() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var names []string
	for name, x := range a.apps {
		if x.c != nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// find is the app with the name, and whether the name is known. c of the app is nil when it does not run.
func (a *Apps) find(name string) (liveApp, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	x, ok := a.apps[name]
	return x, ok
}

// links are the apps that run, for the rail of the browser: each with its home title, else its name.
func (a *Apps) links() []view.AppLink {
	var out []view.AppLink
	for _, name := range a.Names() {
		x, _ := a.find(name)
		out = append(out, view.AppLink{Name: name, Title: x.title})
		if x.title == "" {
			out[len(out)-1].Title = name
		}
	}
	return out
}

// specs are the actions of the apps that run, as rows of the merged catalog: by app, then in the order of the app.
func (a *Apps) specs() []spec {
	var out []spec
	for _, name := range a.Names() {
		x, _ := a.find(name)
		for _, act := range x.actions {
			out = append(out, specOfApp(name, act))
		}
	}
	return out
}

// specOfApp is the row of the merged catalog for an action of the app: the name and the path have the prefix of the
// app. The short text of the browser (Hint) is not the app's, so it is empty. pathValues, fieldsOf and the forms read
// the row as they read a row of the static catalog.
func specOfApp(app string, act siren.Action) spec {
	return spec{Name: app + "-" + act.Name, Title: act.Title, Description: act.Description, Method: act.Method,
		Path: "/with/" + app + act.Href, Fields: slices.Clone(act.Fields), Owner: act.Owner, Danger: act.Danger}
}

// mountWith gives m the routes of the apps: /with/NAME and everything under it, for every method. They go through
// serve, so a browser gets the shell, the toast and the redirect, and an error is an error entity.
func (h *server) mountWith(m *http.ServeMux) {
	m.HandleFunc("/with/{app}", h.serve(h.withApp))
	m.HandleFunc("/with/{app}/{rest...}", h.serve(h.withApp))
}

// withApp forwards a request for /with/NAME/REST to the app NAME, in the name of the writer of the request, and gives
// back the answer of the app in the namespace of the server. There is no retry: a write that was sent may have been
// done, and the caller decides.
func (h *server) withApp(r *http.Request) (*siren.Entity, error) {
	name := r.PathValue("app")
	app, known := h.with.find(name)
	switch {
	case !known:
		return nil, core.NotFound("no such app %s", name)
	case app.c == nil:
		return nil, notRunning(name)
	}
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/with/"+name) // escaped: the app decodes it, not this server
	if rest == "" {
		rest = "/"
	}
	ctx, cancel := context.WithTimeout(r.Context(), forwardTimeout)
	defer cancel()
	var e *siren.Entity
	var err error
	if act, ok := matchAction(app.actions, r.Method, rest); ok {
		values, err := appFields(r, name, act)
		if err != nil { // the refusal is this server's own, so it does not go through forwardError
			return nil, err
		}
		concrete := siren.Action{Name: act.Name, Method: act.Method, Href: rest} // nothing is left to fill in the path
		e, err = app.c.As(source(r)).Do(ctx, concrete, values)
		if err != nil {
			return nil, forwardError(r, name, err)
		}
		return inNamespace(name, e), nil
	} else if r.Method == http.MethodGet {
		q := r.URL.Query()
		for _, f := range browserFields { // the fields of this server: the app never sees them
			q.Del(f)
		}
		href := rest
		if len(q) > 0 {
			href += "?" + q.Encode()
		}
		e, err = app.c.As(source(r)).Get(ctx, href)
	} else {
		return nil, core.NotFound("the app %s has no action %s %s", name, r.Method, rest)
	}
	if err != nil {
		return nil, forwardError(r, name, err)
	}
	return inNamespace(name, e), nil
}

// appFields checks the request against the action of the app in the way of every action of the catalog, and gives the
// values to send to the app: the fields of the action that the request names. An owner action is refused to a writer
// that is not the owner, so the app is never called for it.
func appFields(r *http.Request, name string, act siren.Action) (map[string]string, error) {
	row := specOfApp(name, act)
	if act.Owner {
		if err := requireOwner(source(r)); err != nil {
			return nil, err
		}
	}
	v, err := fieldsOf(r, row)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, f := range fieldNames(row) {
		if v.Has(f) {
			values[f] = v.Get(f)
		}
	}
	return values, nil
}

// matchAction is the action of the app with the method whose href fits the path, segment by segment: {x} fits one
// segment that is not empty, and any other segment fits itself.
func matchAction(actions []siren.Action, method, path string) (siren.Action, bool) {
	parts := strings.Split(path, "/")
	for _, act := range actions {
		want := strings.Split(act.Href, "/")
		if act.Method != method || len(want) != len(parts) {
			continue
		}
		fits := true
		for i, w := range want {
			if isPlaceholder(w) {
				fits = fits && parts[i] != ""
			} else {
				fits = fits && w == parts[i]
			}
		}
		if fits {
			return act, true
		}
	}
	return siren.Action{}, false
}

func isPlaceholder(segment string) bool {
	return len(segment) > 2 && segment[0] == '{' && segment[len(segment)-1] == '}'
}

func notRunning(name string) error {
	return &core.Error{Status: http.StatusServiceUnavailable, Code: siren.CodeUnavailable, Msg: "the app " + name + " is not running"}
}

// forwardError is the error of this server for a failed call of an app. The refusal of the app passes with its status
// and its code. An app that is not there is 503, a timeout stays a timeout (504), and an answer that is not an entity
// is 502. The cause of a 502 goes to the log and not to the client.
func forwardError(r *http.Request, name string, err error) error {
	var ce *client.Error
	var op *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.As(err, &ce):
		msg := ce.Error()
		if p, ok := ce.Entity.Properties.(map[string]any); ok {
			if m, ok := p["message"].(string); ok {
				msg = m
			}
		}
		code := ce.Code()
		if code == "" {
			code = codeOf(ce.Status)
		}
		return &core.Error{Status: ce.Status, Code: code, Msg: msg}
	case errors.As(err, &op) && op.Op == "dial":
		return notRunning(name)
	}
	logFault(r, "the app "+name+" gave no entity", err)
	return &core.Error{Status: http.StatusBadGateway, Code: siren.CodeUpstream, Msg: "the app " + name + " answered something that is not an entity"}
}

// inNamespace changes the answer of an app to the namespace of this server: each address that starts with a slash
// gets the prefix /with/NAME, and each action name gets the prefix NAME-. A link, an embedded link or an action whose
// address is not a path (it has a scheme or a host) is dropped: a connected app is on a loopback address, and
// nothing it says leads elsewhere.
func inNamespace(name string, e *siren.Entity) *siren.Entity {
	prefix := "/with/" + name
	e.Links = relinked(prefix, name, e.Links)
	e.Entities = relinked(prefix, name, e.Entities)
	e.Actions = prefixed(prefix, name, e.Actions)
	return e
}

// relinked keeps the links with a path. It also rewrites the actions of an embedded link, as the actions of the entity.
func relinked(prefix, name string, links []siren.Link) []siren.Link {
	var out []siren.Link
	for _, l := range links {
		if isPath(l.Href) {
			l.Href = prefix + l.Href
			l.Actions = prefixed(prefix, name, l.Actions)
			out = append(out, l)
		}
	}
	return out
}

// prefixed gives each action with a path the prefix of its address and of its name, and drops the others.
func prefixed(prefix, name string, actions []siren.Action) []siren.Action {
	var out []siren.Action
	for _, a := range actions {
		if isPath(a.Href) {
			a.Href, a.Name = prefix+a.Href, name+"-"+a.Name
			out = append(out, a)
		}
	}
	return out
}

// isPath says that href is a path on the same server: one slash first, no scheme, no host.
func isPath(href string) bool {
	u, err := url.Parse(href)
	return err == nil && strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") && u.Scheme == "" && u.Host == ""
}
