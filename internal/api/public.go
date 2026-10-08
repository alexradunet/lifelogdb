package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"lifelog/internal/core"
)

// A public listener (docs/plans/078-network-server.md, "The decision") is opened by `lifelog serve --public`
// alone: the owner's token is the credential of every request, allowed origins answer CORS, and the browser
// logs in once for a cookie that carries the same token. Without --public none of this is mounted.

// TokenMinLength is the shortest token a public listener accepts.
const TokenMinLength = 32

// CookieName is the browser's credential on a public listener, set by POST /login.
const CookieName = "lifelog_token"

// PublicOptions configures Public.
type PublicOptions struct {
	Token   string   // the owner's token, at least TokenMinLength characters, one line
	Origins []string // browser origins (scheme://host[:port]) allowed to read and write
	TLS     bool     // the listener serves TLS: the login cookie is marked Secure
}

// CheckToken reports whether t may be a public listener's token: one line of at least TokenMinLength characters.
func CheckToken(t string) error {
	if len(t) < TokenMinLength {
		return fmt.Errorf("the token must be at least %d characters", TokenMinLength)
	}
	if strings.ContainsAny(t, "\r\n") {
		return errors.New("the token must be one line")
	}
	return nil
}

// PublicAddress validates a --public serve address: any numeric IP of this machine (0.0.0.0 for all of them) or
// localhost, with a numeric port. No DNS, no service names.
func PublicAddress(authority string) (string, error) {
	host, text, err := net.SplitHostPort(authority)
	if err != nil || host == "" || text == "" {
		return "", fmt.Errorf("serve address %q: use a numeric IP (0.0.0.0 for every interface) or localhost and a numeric port (0–65535)", authority)
	}
	port, err := strconv.ParseUint(text, 10, 16)
	if err != nil || strings.ContainsAny(text, "+-") {
		return "", fmt.Errorf("serve address %q: the port must be numeric (0–65535)", authority)
	}
	if strings.EqualFold(host, "localhost") {
		return net.JoinHostPort("127.0.0.1", strconv.FormatUint(port, 10)), nil
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return "", fmt.Errorf("serve address %q: use a numeric IP (0.0.0.0 for every interface) or localhost and a numeric port (0–65535)", authority)
	}
	return net.JoinHostPort(ip.String(), strconv.FormatUint(port, 10)), nil
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
	guard   *http.CrossOriginProtection // for the login forms, which run before next's own guard
}

// Public wraps the API handler for a public listener. The in-process client never goes through it, and
// NetworkAuthority, the local guard, is not mounted with it: the token defends against the rebinding page the
// Host check exists for.
func Public(next http.Handler, o PublicOptions) (http.Handler, error) {
	if err := CheckToken(o.Token); err != nil {
		return nil, err
	}
	p := &public{next: next, token: []byte(o.Token), origins: map[string]bool{}, tls: o.TLS, guard: http.NewCrossOriginProtection()}
	for _, origin := range o.Origins {
		if err := CheckOrigin(origin); err != nil {
			return nil, err
		}
		if err := p.guard.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
		p.origins[origin] = true
	}
	return p, nil
}

func (p *public) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Absolute-form targets replace Host during parsing; no proxy deployment relies on them.
	if r.URL == nil || r.URL.IsAbs() || r.URL.Host != "" {
		http.Error(w, "HTTP authority refused", http.StatusForbidden)
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
			http.Error(w, "origin refused", http.StatusForbidden)
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
		e, status := errorEntity(&core.Error{Status: http.StatusUnauthorized, Msg: "token required", Code: "token_required"})
		write(w, r, status, e)
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
		e := &Entity{Class: []string{"login"}, Title: "Log in",
			Properties: map[string]any{"failed": r.URL.Query().Get("failed") == "1", "logged_in": p.authorized(r)}}
		renderHTML(w, r, http.StatusOK, e)
		return
	}
	if err := p.guard.Check(r); err != nil {
		http.Error(w, "cross-origin browser write refused", http.StatusForbidden)
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
		http.Error(w, "cross-origin browser write refused", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: p.tls, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
