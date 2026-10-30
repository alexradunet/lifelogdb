package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"lifelog/internal/core"
	"lifelog/siren"
)

// The lock of a public listener (docs/plans/160-the-lock.md). A browser logs in with the token and gets a session
// cookie: a random id and its HMAC. The server keeps the state of each session in memory. A session that is idle
// for longer than the setting lock_after, or that the owner locked, gets the lock page and nothing of the database,
// until the PIN opens it. A request with the bearer token never locks: a program has no screen to leave open.
//
// The routes /lock, /unlock, /locked and /awake are about one browser, not about the data, so they are not actions
// of the catalog; /login and /logout are the same.

// SessionCookie is the browser's credential on a public listener, set by POST /login. The older cookie lifelog_token
// held the token itself: it is no longer a credential, and the login clears it.
const SessionCookie = "lifelog_session"

const legacyCookie = "lifelog_token"

// wrongPINLimit is the count of wrong PINs that ends a session: the browser must log in with the token again.
const wrongPINLimit = 5

// idleSlack is added to lock_after. The page script sends POST /awake at most once in 30 seconds, so the server
// waits one minute more than the setting before it counts the session idle.
const idleSlack = time.Minute

// forgetAfter bounds the memory. The state of a session that no request touched for a day is dropped: the longest
// setting is 60 minutes, so such a session is idle past the limit and a request finds it locked, with or without
// the state. The ended ids are kept for the life of the cookie, one year. Pruning runs at most once in a minute.
const (
	forgetAfter = 24 * time.Hour
	pruneEvery  = time.Minute
)

// sessionState is what the server remembers of one session.
type sessionState struct {
	active time.Time // the last activity of the owner (countsAsActivity)
	seen   time.Time // the last request of any kind, for forgetAfter
	locked bool
	wrong  int // the wrong PINs since the last unlock
}

// sessions is the memory of the sessions: under one mutex, no goroutine. A time comes from the caller (the store's
// clock), so a test fixes it.
type sessions struct {
	mu         sync.Mutex
	state      map[string]*sessionState
	ended      map[string]time.Time // session ids that logged out or used up their PINs, with the time they ended
	lastPruned time.Time
}

func newSessions() *sessions {
	return &sessions{state: map[string]*sessionState{}, ended: map[string]time.Time{}}
}

// prune drops what is old. The caller holds the mutex.
func (ss *sessions) prune(now time.Time) {
	if now.Sub(ss.lastPruned) < pruneEvery {
		return
	}
	ss.lastPruned = now
	for id, st := range ss.state {
		if now.Sub(st.seen) > forgetAfter {
			delete(ss.state, id)
		}
	}
	for id, at := range ss.ended {
		if now.Sub(at) > loginMaxAge*time.Second {
			delete(ss.ended, id)
		}
	}
}

func (ss *sessions) isEnded(id string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	_, ended := ss.ended[id]
	return ended
}

// open records a new session, after the login: it is open and active now.
func (ss *sessions) open(id string, now time.Time) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.prune(now)
	ss.state[id] = &sessionState{active: now, seen: now}
}

// locked reports whether the lock holds the session shut at now, and records the request. A session with no state
// (the server started after the login) is locked when the lock is on. idle is lock_after plus idleSlack.
func (ss *sessions) locked(id string, now time.Time, lockOn bool, idle time.Duration) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.prune(now)
	st, known := ss.state[id]
	if !known {
		st = &sessionState{active: now, locked: lockOn}
		ss.state[id] = st
	}
	st.seen = now
	if lockOn && now.Sub(st.active) > idle {
		st.locked = true
	}
	return lockOn && st.locked
}

// activity records the owner's activity of an open session.
func (ss *sessions) activity(id string, now time.Time) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if st, ok := ss.state[id]; ok && !st.locked {
		st.active = now
	}
}

// lock locks the session now. It keeps the count of wrong PINs: a lock must not give the tries back. An ended
// session gets no state again: a request that was held while it ended must not bring it back.
func (ss *sessions) lock(id string, now time.Time) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, ended := ss.ended[id]; ended {
		return
	}
	st, ok := ss.state[id]
	if !ok {
		st = &sessionState{}
		ss.state[id] = st
	}
	st.active, st.seen, st.locked = now, now, true
}

// tryPIN counts a try before the PIN is compared. Requests that the server holds while their bodies come, or that
// come at once, then get no more than wrongPINLimit tries together. It is false for an ended session and for one that
// has used its tries.
func (ss *sessions) tryPIN(id string, now time.Time) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, ended := ss.ended[id]; ended {
		return false
	}
	st, ok := ss.state[id]
	if !ok {
		st = &sessionState{locked: true}
		ss.state[id] = st
	}
	st.seen = now
	if st.wrong >= wrongPINLimit {
		return false
	}
	st.wrong++
	return true
}

// opened opens the session after the right PIN, and gives the tries back. It is false when the session ended while
// the PIN was compared.
func (ss *sessions) opened(id string, now time.Time) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, ended := ss.ended[id]; ended {
		return false
	}
	ss.state[id] = &sessionState{active: now, seen: now}
	return true
}

// failed records a wrong PIN, whose try tryPIN has counted. It returns the count, and ends the session when the
// tries are used up.
func (ss *sessions) failed(id string, now time.Time) (n int, ended bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, done := ss.ended[id]; done {
		return wrongPINLimit, true
	}
	if st, ok := ss.state[id]; ok {
		n = st.wrong
	}
	if n >= wrongPINLimit {
		delete(ss.state, id)
		ss.ended[id] = now
		return n, true
	}
	return n, false
}

func (ss *sessions) end(id string, now time.Time) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.state, id)
	ss.ended[id] = now
}

// The cookie value is ID.MAC in hex: ID is 16 random bytes, MAC the HMAC-SHA256 of the ID. The key is derived from
// the token, so a new token file ends every session, and the server stores no list of sessions on disk.
func (p *public) macOf(id string) string {
	m := hmac.New(sha256.New, p.sessionKey)
	m.Write([]byte(id))
	return hex.EncodeToString(m.Sum(nil))
}

func sessionKeyOf(token []byte) []byte {
	m := hmac.New(sha256.New, token)
	m.Write([]byte("lifelog session"))
	return m.Sum(nil)
}

// newSession makes the cookie value of a new session, and its id.
func (p *public) newSession() (value, id string, err error) {
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	id = hex.EncodeToString(b)
	return id + "." + p.macOf(id), id, nil
}

// session is the id of the valid session cookie of r. A forged or changed cookie, and the cookie of an ended
// session, are none.
func (p *public) session(r *http.Request) (string, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return "", false
	}
	id, mac, ok := strings.Cut(c.Value, ".")
	if !ok || len(id) != 32 || !hmac.Equal([]byte(mac), []byte(p.macOf(id))) || p.sessions.isEnded(id) {
		return "", false
	}
	return id, true
}

func (p *public) setSession(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: p.tls, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

// clearLegacy clears the cookie lifelog_token of an older login.
func (p *public) clearLegacy(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: legacyCookie, Value: "", Path: "/", HttpOnly: true, Secure: p.tls, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// lockConfig reads the lock from the settings file at each request, so a change is live at once. A store with no
// settings file has no lock. A read that fails is an error, and the caller treats the session as locked.
func (p *public) lockConfig() (minutes int, hash string, err error) {
	if p.store.SettingsPath == "" {
		return 0, "", nil
	}
	v, err := p.store.ReadSettings()
	if err != nil {
		return 0, "", err
	}
	return v.LockMinutes(), v.LockPINHash, nil
}

// lockMinutesKey is the one request value of this file: the minutes of lock_after, put on the request once. The
// shell reads them to draw the Lock button and the script lock.js. No value means that the lock is off.
type lockMinutesKey struct{}

func lockMinutesOf(r *http.Request) int {
	m, _ := r.Context().Value(lockMinutesKey{}).(int)
	return m
}

// countsAsActivity reports a request that the owner made, and not the page: a navigation that is not a prefetch or a
// prerender (no Sec-Purpose header), and a form post. The poll of the chat script and a speculative load are not.
// A browser that sends no Sec-Fetch-Mode header (an old one) counts for a GET that asks for HTML.
func countsAsActivity(r *http.Request) bool {
	if r.Header.Get("Sec-Purpose") != "" {
		return false
	}
	mode := r.Header.Get("Sec-Fetch-Mode")
	switch r.Method {
	case http.MethodGet:
		return mode == "navigate" || (mode == "" && wantsHTML(r))
	case http.MethodPost:
		return mode == "navigate"
	}
	return false
}

// localPath is next when it is a path of this origin, else "". It must start with one "/" and no "\": a browser reads
// "//host" and "/\host" as another origin. A control character is refused too: a browser drops a tab or a line
// break from a URL, which could turn "/<tab>/host" into "//host".
func localPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		return ""
	}
	for _, c := range next {
		if c < 0x20 || c == 0x7f || c == '\\' {
			return ""
		}
	}
	return next
}

// serveSession serves a request of a browser with a session cookie. The caller has checked the cookie.
func (p *public) serveSession(w http.ResponseWriter, r *http.Request, id string) {
	now := p.store.Now()
	minutes, hash, err := p.lockConfig()
	if err != nil {
		// Fail closed: a settings file that cannot be read must not open a locked browser.
		log.Printf("lifelog: %s %s: the lock settings cannot be read, so the session is locked: %v", r.Method, r.URL.Path, err)
	}
	lockOn := minutes > 0 || err != nil
	idle := time.Duration(minutes)*time.Minute + idleSlack
	// A read that failed locks the session whatever its state.
	locked := p.sessions.locked(id, now, lockOn, idle) || err != nil
	if !strings.HasPrefix(r.URL.Path, "/assets/") { // the assets are the same for every browser: they keep their cache
		w = &noStore{ResponseWriter: w}
	}
	if minutes > 0 {
		r = r.WithContext(context.WithValue(r.Context(), lockMinutesKey{}, minutes))
	}
	switch {
	case r.URL.Path == "/lock" && r.Method == http.MethodPost:
		p.lockNow(w, r, id, now, lockOn && err == nil)
		return
	case r.URL.Path == "/locked" && r.Method == http.MethodGet:
		p.lockedPage(w, r, locked)
		return
	case r.URL.Path == "/unlock" && r.Method == http.MethodPost:
		p.unlock(w, r, id, now, hash, lockOn, err)
		return
	case r.URL.Path == "/awake" && r.Method == http.MethodPost:
		p.awake(w, r, id, now, locked)
		return
	}
	if locked {
		if r.Method == http.MethodGet && wantsHTML(r) {
			// next is the path alone: a query, such as the words of a search, is the owner's and would show on the lock page.
			http.Redirect(w, r, "/locked?next="+url.QueryEscape(r.URL.EscapedPath()), http.StatusSeeOther)
			return
		}
		writeError(w, r, &core.Error{Status: http.StatusUnauthorized, Msg: "locked: unlock this browser with the PIN at /locked", Code: siren.CodeLocked})
		return
	}
	if countsAsActivity(r) {
		p.sessions.activity(id, now)
	}
	p.next.ServeHTTP(w, r)
}

// lockNow is POST /lock: the owner locks this browser. With the lock off it is 409: there is no PIN to open it with.
func (p *public) lockNow(w http.ResponseWriter, r *http.Request, id string, now time.Time, lockOn bool) {
	if err := p.guard.Check(r); err != nil {
		writeError(w, r, crossOriginRefusal())
		return
	}
	if !lockOn {
		writeError(w, r, &core.Error{Status: http.StatusConflict, Msg: "the lock is off: set lock_after and lock_pin in Settings"})
		return
	}
	p.sessions.lock(id, now)
	http.Redirect(w, r, "/locked", http.StatusSeeOther)
}

// lockedPage is GET /locked: the page with the PIN field and nothing of the database. A session that is not locked
// goes on to next.
func (p *public) lockedPage(w http.ResponseWriter, r *http.Request, locked bool) {
	next := localPath(r.URL.Query().Get("next"))
	if !locked {
		if next == "" {
			next = "/"
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	e := &siren.Entity{Class: []string{"locked"}, Title: "Locked",
		Properties: map[string]any{"failed": r.URL.Query().Get("failed") == "1", "next": next}}
	answer(w, r, http.StatusOK, e, frame{pageOnly: true})
}

// unlock is POST /unlock. The right PIN opens the session and goes to next. A wrong one goes back to the lock page;
// the fifth ends the session. The log gets one line for each wrong PIN and never the PIN.
func (p *public) unlock(w http.ResponseWriter, r *http.Request, id string, now time.Time, hash string, lockOn bool, cfgErr error) {
	if err := p.guard.Check(r); err != nil {
		writeError(w, r, crossOriginRefusal())
		return
	}
	if cfgErr != nil {
		// A fault of the server is not a wrong PIN: it is not counted, so it never ends the owner's session.
		writeError(w, r, fmt.Errorf("read the lock settings: %w", cfgErr))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	parseErr := r.ParseForm()
	next := localPath(r.PostForm.Get("next"))
	open := next // where the right PIN goes
	if open == "" {
		open = "/"
	}
	// The session can end while this request waits for its body (another request, a logout): each step below asks
	// the memory of the sessions again, under its mutex.
	ended := func() {
		p.sessions.end(id, now)
		p.setSession(w, "", -1)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
	// With the lock off there is nothing to open: the session goes on, and no try is counted.
	if !lockOn {
		if !p.sessions.opened(id, now) {
			ended()
			return
		}
		http.Redirect(w, r, open, http.StatusSeeOther)
		return
	}
	if !p.sessions.tryPIN(id, now) {
		log.Printf("lifelog: %s %s: no tries left, the session ends", r.Method, r.URL.Path)
		ended()
		return
	}
	if hash != "" && parseErr == nil && core.PINMatches(hash, r.PostForm.Get("pin")) {
		if !p.sessions.opened(id, now) {
			ended()
			return
		}
		http.Redirect(w, r, open, http.StatusSeeOther)
		return
	}
	n, done := p.sessions.failed(id, now)
	log.Printf("lifelog: %s %s: the PIN does not match (%d of %d)", r.Method, r.URL.Path, n, wrongPINLimit)
	if done {
		ended()
		return
	}
	to := "/locked?failed=1"
	if next != "" {
		to += "&next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// awake is POST /awake: the page script says that the owner typed, clicked or scrolled. A locked session gets 401
// locked, so the script can show the lock page.
func (p *public) awake(w http.ResponseWriter, r *http.Request, id string, now time.Time, locked bool) {
	if err := p.guard.Check(r); err != nil {
		writeError(w, r, crossOriginRefusal())
		return
	}
	if locked {
		writeError(w, r, &core.Error{Status: http.StatusUnauthorized, Msg: "locked: unlock this browser with the PIN at /locked", Code: siren.CodeLocked})
		return
	}
	p.sessions.activity(id, now)
	w.WriteHeader(http.StatusNoContent)
}

// noStore makes every answer of a session uncacheable, whatever its handler set: a picture that its route keeps for a
// year would show from the cache in a locked browser. The header is set at the first WriteHeader or Write.
type noStore struct {
	http.ResponseWriter
	wrote bool
}

func (n *noStore) WriteHeader(code int) {
	if !n.wrote {
		n.wrote = true
		n.Header().Set("Cache-Control", "no-store")
	}
	n.ResponseWriter.WriteHeader(code)
}

func (n *noStore) Write(b []byte) (int, error) {
	if !n.wrote {
		n.WriteHeader(http.StatusOK)
	}
	return n.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the connection (the deadlines of serve.go).
func (n *noStore) Unwrap() http.ResponseWriter { return n.ResponseWriter }
