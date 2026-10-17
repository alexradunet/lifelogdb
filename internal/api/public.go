package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
)

// A public listener (docs/plans/078-network-server.md, "The decision") is opened by `lifelog serve --public`
// alone: the owner's token is the credential of every request, allowed origins answer CORS, and the browser
// logs in once for a cookie that carries the same token. Without --public none of this is mounted.

// tokenMinLength is the shortest token a public listener accepts.
const tokenMinLength = 32

// CookieName is the browser's credential on a public listener, set by POST /login.
const CookieName = "lifelog_token"

// PublicOptions configures Public.
type PublicOptions struct {
	Token   string   // the owner's token, at least tokenMinLength characters, one line
	Origins []string // browser origins (scheme://host[:port]) allowed to read and write
	TLS     bool     // the listener serves TLS: the login cookie is marked Secure
}

// CheckToken reports whether t may be a public listener's token: one line of at least tokenMinLength characters.
func CheckToken(t string) error {
	if len(t) < tokenMinLength {
		return fmt.Errorf("the token must be at least %d characters", tokenMinLength)
	}
	if strings.ContainsAny(t, "\r\n") {
		return errors.New("the token must be one line")
	}
	return nil
}

// PublicAddress validates a --public serve address: any numeric IP of this machine (0.0.0.0 for all of them) or
// localhost, with a numeric port. No DNS, no service names. It parses the address as LoopbackAddress does.
func PublicAddress(authority string) (string, error) {
	_, ip, port, ok := authorityOf(authority)
	if !ok {
		return "", fmt.Errorf("serve address %q: use a numeric IP (0.0.0.0 for every interface) or localhost and a numeric port (0–65535)", authority)
	}
	host := "127.0.0.1" // localhost
	if ip.IsValid() {
		host = ip.String()
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// CheckOrigin admits an exact browser origin: a scheme, a host, an optional port and nothing else.
func CheckOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || origin != u.Scheme+"://"+u.Host {
		return fmt.Errorf("origin %q: use scheme://host[:port] exactly", origin)
	}
	return nil
}

// public is the handler of a public listener: the credential check, CORS for the allowed origins and the login
// pages, in front of the API handler.
type public struct {
	next    http.Handler
	token   []byte
	origins map[string]bool
	tls     bool
	guard   *http.CrossOriginProtection // the guard of next; the login forms run before next, so they check it too
}

// Public is the handler of a public listener: the API on s, behind the token, CORS and the login pages. The allowed
// origins are given once: one guard trusts them, for the writes of the API and for the login forms. The in-process
// client never goes through it, and NetworkAuthority, the local guard, is not mounted with it: the token defends
// against the rebinding page the Host check exists for.
func Public(s *core.Store, o PublicOptions) (http.Handler, error) {
	if err := CheckToken(o.Token); err != nil {
		return nil, err
	}
	p := &public{token: []byte(o.Token), origins: map[string]bool{}, tls: o.TLS, guard: http.NewCrossOriginProtection()}
	for _, origin := range o.Origins {
		if err := CheckOrigin(origin); err != nil {
			return nil, err
		}
		if err := p.guard.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
		p.origins[origin] = true
	}
	p.next = newOn(s, p.guard)
	return p, nil
}

func (p *public) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if absoluteForm(r) {
		writeError(w, r, authorityRefusal())
		return
	}
	origin := r.Header.Get("Origin")
	allowed := origin != "" && p.origins[origin]
	if allowed {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		h.Set("Access-Control-Expose-Headers", "Location")
	}
	if r.Method == http.MethodOptions {
		if !allowed {
			writeError(w, r, &core.Error{Status: http.StatusForbidden, Msg: "origin refused"})
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, "+SourceHeader)
		h.Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch {
	case r.URL.Path == "/login" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		p.login(w, r)
		return
	case r.URL.Path == "/logout" && r.Method == http.MethodPost:
		p.logout(w, r)
		return
	}
	if !p.authorized(r) {
		if r.Method == http.MethodGet && wantsHTML(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="lifelog"`)
		writeError(w, r, &core.Error{Status: http.StatusUnauthorized, Msg: "token required", Code: "token_required"})
		return
	}
	p.next.ServeHTTP(w, r)
}

// authorized reports a request carrying the token, in the Authorization header or the login cookie.
func (p *public) authorized(r *http.Request) bool {
	if scheme, credential, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return p.matches(strings.TrimSpace(credential))
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return p.matches(c.Value)
	}
	return false
}

func (p *public) matches(credential string) bool {
	return subtle.ConstantTimeCompare([]byte(credential), p.token) == 1
}

// login is the browser's way in: GET shows the form, POST with the token sets the cookie. A program sends the
// header instead. The page is the only one served without a credential, and it shows nothing of the database.
func (p *public) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		e := &siren.Entity{Class: []string{"login"}, Title: "Log in",
			Properties: map[string]any{"failed": r.URL.Query().Get("failed") == "1", "logged_in": p.authorized(r)}}
		answer(w, r, http.StatusOK, e, frame{pageOnly: true})
		return
	}
	if err := p.guard.Check(r); err != nil {
		writeError(w, r, crossOriginRefusal())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || !p.matches(r.PostForm.Get("token")) {
		http.Redirect(w, r, "/login?failed=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: string(p.token), Path: "/", HttpOnly: true, Secure: p.tls, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (p *public) logout(w http.ResponseWriter, r *http.Request) {
	if err := p.guard.Check(r); err != nil {
		writeError(w, r, crossOriginRefusal())
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: p.tls, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
