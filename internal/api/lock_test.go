package api_test

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/siren"
)

// The lock of a public listener (docs/plans/160-the-lock.md), with a fixed clock and a settings file in a temporary
// folder. The PIN is synthetic.

const (
	testPIN   = "4242"
	secretDay = "2031-01-01"
	secret    = "Zebra Secret Page" // a title that the lock page must never show
)

type lockFixture struct {
	t      *testing.T
	h      http.Handler
	s      *core.Store
	now    time.Time
	cookie string // the Cookie header of the logged-in browser
}

// newLockFixture makes a public handler with the lock set to minutes ("" is off) and a browser that has logged in.
func newLockFixture(t *testing.T, minutes string) *lockFixture {
	t.Helper()
	h, s := publicHandler(t, api.PublicOptions{Token: testToken})
	f := &lockFixture{t: t, h: h, s: s, now: time.Date(2031, 1, 1, 9, 0, 0, 0, time.UTC)}
	s.Clock = func() time.Time { return f.now }
	s.SettingsPath = filepath.Join(t.TempDir(), "life.settings.json")
	hash, err := core.HashPIN(testPIN)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteSettings(core.Settings{LockAfter: minutes, LockPINHash: hash}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(t.Context(), "cli", secret, "synthetic", ""); err != nil {
		t.Fatal(err)
	}
	f.cookie = f.login(h)
	return f
}

// login makes a browser log in on h and returns its Cookie header.
func (f *lockFixture) login(h http.Handler) string {
	f.t.Helper()
	rec := request(h, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate"})
	for _, c := range rec.Result().Cookies() {
		if c.Name == api.SessionCookie {
			return c.Name + "=" + c.Value
		}
	}
	f.t.Fatalf("login set no session cookie: %d", rec.Code)
	return ""
}

// browse is a navigation of the owner's browser.
func (f *lockFixture) browse(path string, extra map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	h := map[string]string{"Accept": "text/html", "Cookie": f.cookie, "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate"}
	for k, v := range extra {
		if v == "" {
			delete(h, k)
		} else {
			h[k] = v
		}
	}
	return request(f.h, "GET", path, "", h)
}

// post is a form post of the owner's browser.
func (f *lockFixture) post(path, body string, extra map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	h := map[string]string{"Accept": "text/html", "Cookie": f.cookie, "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate"}
	for k, v := range extra {
		h[k] = v
	}
	return request(f.h, "POST", path, body, h)
}

// fetch is a script's GET of JSON from the page.
func (f *lockFixture) fetch(path string) *httptest.ResponseRecorder {
	f.t.Helper()
	return request(f.h, "GET", path, "", map[string]string{"Accept": "application/vnd.siren+json", "Cookie": f.cookie, "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors"})
}

func (f *lockFixture) isLocked() bool {
	f.t.Helper()
	rec := f.fetch("/days/" + secretDay)
	switch rec.Code {
	case 200:
		return false
	case 401:
		if !strings.Contains(rec.Body.String(), `"code": "locked"`) {
			f.t.Fatalf("401 without the code locked: %.200s", rec.Body.String())
		}
		return true
	}
	f.t.Fatalf("probe: %d %.200s", rec.Code, rec.Body.String())
	return false
}

func TestSessionCookie(t *testing.T) {
	for _, tls := range []bool{false, true} {
		h, _ := publicHandler(t, api.PublicOptions{Token: testToken, TLS: tls})
		rec := request(h, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Sec-Fetch-Site": "same-origin"})
		var session, legacy *http.Cookie
		for _, c := range rec.Result().Cookies() {
			switch c.Name {
			case api.SessionCookie:
				session = c
			case "lifelog_token":
				legacy = c
			}
		}
		if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" || session.Secure != tls || session.MaxAge != 365*24*60*60 {
			t.Fatalf("tls=%v session cookie %+v", tls, session)
		}
		if strings.Contains(session.Value, testToken) {
			t.Error("the session cookie holds the token")
		}
		if legacy == nil || legacy.MaxAge >= 0 {
			t.Errorf("the login does not clear the old cookie: %+v", legacy)
		}
	}
}

func TestSessionCookieChecks(t *testing.T) {
	f := newLockFixture(t, "")
	value := strings.TrimPrefix(f.cookie, api.SessionCookie+"=")
	id, mac, _ := strings.Cut(value, ".")
	flip := func(s string) string {
		if s[0] == '0' {
			return "1" + s[1:]
		}
		return "0" + s[1:]
	}
	for name, cookie := range map[string]string{
		"changed mac":     api.SessionCookie + "=" + id + "." + flip(mac),
		"changed id":      api.SessionCookie + "=" + flip(id) + "." + mac,
		"no mac":          api.SessionCookie + "=" + id,
		"the token":       api.SessionCookie + "=" + testToken,
		"the old cookie":  "lifelog_token=" + testToken,
		"nothing at all":  "",
		"an empty cookie": api.SessionCookie + "=",
	} {
		rec := request(f.h, "GET", "/", "", map[string]string{"Accept": "text/html", "Cookie": cookie})
		if rec.Code != 303 || rec.Header().Get("Location") != "/login" {
			t.Errorf("%s: %d %q, want the login redirect", name, rec.Code, rec.Header().Get("Location"))
		}
		if rec := request(f.h, "GET", "/pages", "", map[string]string{"Cookie": cookie}); rec.Code != 401 || !strings.Contains(rec.Body.String(), "token_required") {
			t.Errorf("%s: API %d", name, rec.Code)
		}
	}
	// A new token ends every session: the same cookie meets a handler with another token.
	other, err := api.Public(f.s, api.PublicOptions{Token: strings.Repeat("y", 40)})
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(other, "GET", "/", "", map[string]string{"Accept": "text/html", "Cookie": f.cookie}); rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Errorf("a cookie of another token: %d", rec.Code)
	}
	// Logout ends the session: its cookie is dead from then on, even though the HMAC is right.
	if rec := f.post("/logout", "", nil); rec.Code != 303 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := f.browse("/", nil); rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Errorf("an ended session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSessionPagesAreNotCachedAndAssetsKeepTheirHeaders(t *testing.T) {
	f := newLockFixture(t, "")
	if got := f.browse("/days/"+secretDay, nil).Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("a page of a session: Cache-Control %q", got)
	}
	rec := request(f.h, "GET", "/assets/chat.js", "", map[string]string{"Cookie": f.cookie})
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("an asset: %d Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestLockAfterTheIdleMinutes(t *testing.T) {
	f := newLockFixture(t, "5")
	f.now = f.now.Add(6 * time.Minute) // lock_after plus one minute, to the second: still open
	if f.isLocked() {
		t.Fatal("locked at lock_after plus one minute")
	}
	f.now = f.now.Add(6*time.Minute + time.Second)
	rec := f.browse("/days/"+secretDay+"?x=1", nil)
	if want := "/locked?next=" + url.QueryEscape("/days/"+secretDay+"?x=1"); rec.Code != 303 || rec.Header().Get("Location") != want {
		t.Fatalf("idle session: %d %q, want %q", rec.Code, rec.Header().Get("Location"), want)
	}
	if !f.isLocked() {
		t.Error("an idle session is open to the API")
	}
	// A locked session stays locked: a navigation does not open it.
	f.now = f.now.Add(time.Second)
	if rec := f.browse("/", nil); rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/locked") {
		t.Errorf("navigation of a locked session: %d", rec.Code)
	}
}

func TestOnlyTheOwnersActivityKeepsTheSessionOpen(t *testing.T) {
	// Each case: one request at four minutes, then a probe at seven minutes. The session is open at seven minutes
	// only if the request counted as activity (the limit is six).
	for _, tc := range []struct {
		name  string
		do    func(f *lockFixture) *httptest.ResponseRecorder
		count bool
	}{
		{"navigation", func(f *lockFixture) *httptest.ResponseRecorder { return f.browse("/days/"+secretDay, nil) }, true},
		{"form post", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.post("/days/"+secretDay+"/capture", "text=synthetic", nil)
		}, true},
		{"awake", func(f *lockFixture) *httptest.ResponseRecorder { return f.post("/awake", "", nil) }, true},
		{"old browser navigation", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.browse("/days/"+secretDay, map[string]string{"Sec-Fetch-Mode": "", "Sec-Fetch-Site": ""})
		}, true},
		{"fetch", func(f *lockFixture) *httptest.ResponseRecorder { return f.fetch("/days/" + secretDay) }, false},
		{"prerender", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.browse("/days/"+secretDay, map[string]string{"Sec-Purpose": "prefetch;prerender"})
		}, false},
		{"fetch post", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.post("/days/"+secretDay+"/capture", "text=synthetic", map[string]string{"Sec-Fetch-Mode": "same-origin"})
		}, false},
		{"bearer-less script poll with html", func(f *lockFixture) *httptest.ResponseRecorder {
			return f.browse("/days/"+secretDay, map[string]string{"Sec-Fetch-Mode": "cors"})
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLockFixture(t, "5")
			f.now = f.now.Add(4 * time.Minute)
			if rec := tc.do(f); rec.Code >= 400 {
				t.Fatalf("the request failed: %d %.200s", rec.Code, rec.Body.String())
			}
			f.now = f.now.Add(3 * time.Minute)
			if locked := f.isLocked(); locked == tc.count {
				t.Errorf("locked at seven minutes = %v, but counts as activity = %v", locked, tc.count)
			}
		})
	}
}

func TestLockNowAndTheLockPage(t *testing.T) {
	f := newLockFixture(t, "15")
	rec := f.post("/lock", "", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/locked" {
		t.Fatalf("lock: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := f.browse("/days/"+secretDay, nil); rec.Code != 303 || rec.Header().Get("Location") != "/locked?next="+url.QueryEscape("/days/"+secretDay) {
		t.Errorf("HTML GET: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if !f.isLocked() {
		t.Error("the API is open")
	}
	if rec := f.post("/days/"+secretDay+"/capture", "text=late", nil); rec.Code != 401 || !strings.Contains(rec.Body.String(), "Locked.") {
		t.Errorf("form post: %d %.300s", rec.Code, rec.Body.String())
	}
	if rec := f.post("/awake", "", nil); rec.Code != 401 {
		t.Errorf("awake of a locked session: %d", rec.Code)
	}
	page := f.browse("/locked?next="+url.QueryEscape("/days/"+secretDay), nil)
	body := page.Body.String()
	if page.Code != 200 || page.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body, `name="pin"`) || !strings.Contains(body, `value="/days/`+secretDay+`"`) {
		t.Fatalf("the lock page: %d %q %.300s", page.Code, page.Header().Get("Cache-Control"), body)
	}
	for _, leak := range []string{secret, "<dialog", `id="chat"`} {
		if strings.Contains(body, leak) {
			t.Errorf("the lock page holds %q", leak)
		}
	}
	// next goes to another origin: dropped.
	for _, next := range []string{"//evil.example/x", `/\evil.example`, "https://evil.example/", "/\t/evil.example", "evil", "/a\nb"} {
		body := f.browse("/locked?next="+url.QueryEscape(next), nil).Body.String()
		if !strings.Contains(body, `name="next" value=""`) {
			t.Errorf("next %q was kept", next)
		}
	}
}

func TestUnlock(t *testing.T) {
	logs := &bytes.Buffer{}
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	f := newLockFixture(t, "15")
	f.post("/lock", "", nil)
	next := url.QueryEscape("/days/" + secretDay)

	// A wrong PIN goes back with the page to return to; the right one opens and goes there.
	rec := f.post("/unlock", "pin=1111&next="+next, nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/locked?failed=1&next="+next {
		t.Fatalf("wrong PIN: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if body := f.browse("/locked?failed=1", nil).Body.String(); !strings.Contains(body, "Refused.") {
		t.Error("the lock page does not say that the PIN was refused")
	}
	rec = f.post("/unlock", "pin="+testPIN+"&next="+next, nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/days/"+secretDay {
		t.Fatalf("right PIN: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if f.isLocked() {
		t.Error("still locked after the right PIN")
	}
	// The unlock gave the count back: four more wrong PINs do not end the session.
	f.post("/lock", "", nil)
	for range 4 {
		f.post("/unlock", "pin=1111", nil)
	}
	if rec := f.post("/unlock", "pin="+testPIN+"&next=//evil.example", nil); rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Fatalf("right PIN, foreign next: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// The fifth wrong PIN in a row ends the session and clears the cookie.
	f.post("/lock", "", nil)
	for i := range 4 {
		if rec := f.post("/unlock", "pin=2222", nil); rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/locked") {
			t.Fatalf("wrong PIN %d: %d %q", i+1, rec.Code, rec.Header().Get("Location"))
		}
	}
	rec = f.post("/unlock", "pin=2222", nil)
	cleared := false
	for _, c := range rec.Result().Cookies() {
		cleared = cleared || (c.Name == api.SessionCookie && c.MaxAge < 0)
	}
	if rec.Code != 303 || rec.Header().Get("Location") != "/login" || !cleared {
		t.Fatalf("fifth wrong PIN: %d %q cleared=%v", rec.Code, rec.Header().Get("Location"), cleared)
	}
	// The right PIN no longer helps: the session is ended.
	if rec := f.post("/unlock", "pin="+testPIN, nil); rec.Code != 401 {
		t.Errorf("unlock of an ended session: %d", rec.Code)
	}
	if rec := f.browse("/", nil); rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Errorf("an ended session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, pin := range []string{"1111", "2222", testPIN} {
		if strings.Contains(logs.String(), pin) {
			t.Errorf("the log holds the PIN %s", pin)
		}
	}
	if got := strings.Count(logs.String(), "the PIN does not match"); got != 10 {
		t.Errorf("%d log lines for wrong PINs, want 10:\n%s", got, logs.String())
	}
}

// The Lock button does not give the tries back: four wrong PINs, a lock, and the next wrong PIN ends the session.
func TestALockKeepsTheCountOfWrongPINs(t *testing.T) {
	f := newLockFixture(t, "15")
	f.post("/lock", "", nil)
	for range 4 {
		f.post("/unlock", "pin=1111", nil)
	}
	if rec := f.post("/lock", "", nil); rec.Code != 303 {
		t.Fatalf("lock of a locked session: %d", rec.Code)
	}
	if rec := f.post("/unlock", "pin=1111", nil); rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Errorf("fifth wrong PIN after a lock: %d %q, want the end of the session", rec.Code, rec.Header().Get("Location"))
	}
}

func TestARestartLocksEverySession(t *testing.T) {
	f := newLockFixture(t, "15")
	if f.isLocked() {
		t.Fatal("locked at once")
	}
	restarted, err := api.Public(f.s, api.PublicOptions{Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	f.h = restarted
	if !f.isLocked() {
		t.Fatal("a session that the new server does not know is open")
	}
	if rec := f.post("/unlock", "pin="+testPIN, nil); rec.Code != 303 {
		t.Fatalf("unlock: %d", rec.Code)
	}
	if f.isLocked() {
		t.Error("the PIN did not open the session")
	}
}

func TestABearerRequestNeverLocks(t *testing.T) {
	f := newLockFixture(t, "5")
	f.now = f.now.Add(10 * time.Hour)
	rec := request(f.h, "GET", "/days/"+secretDay, "", map[string]string{"Authorization": "Bearer " + testToken})
	if rec.Code != 200 {
		t.Errorf("bearer after ten idle hours: %d", rec.Code)
	}
	// The same, with the session cookie beside the header: the header decides.
	rec = request(f.h, "GET", "/days/"+secretDay, "", map[string]string{"Authorization": "Bearer " + testToken, "Cookie": f.cookie})
	if rec.Code != 200 {
		t.Errorf("bearer with a locked cookie: %d", rec.Code)
	}
}

func TestWithTheLockOffNothingLocks(t *testing.T) {
	f := newLockFixture(t, "")
	f.now = f.now.Add(10 * time.Hour)
	if f.isLocked() {
		t.Error("locked with lock_after off")
	}
	rec := f.post("/lock", "", nil)
	if rec.Code != 409 {
		t.Errorf("/lock with the lock off: %d, want 409", rec.Code)
	}
	if f.isLocked() {
		t.Error("/lock locked a session with the lock off")
	}
	// A PIN with lock_after off is off too, and lock_after without a PIN is off.
	if err := f.s.WriteSettings(core.Settings{LockAfter: "5"}); err != nil {
		t.Fatal(err)
	}
	if f.isLocked() {
		t.Error("locked with no PIN stored")
	}
	if rec := f.post("/unlock", "pin=1111", nil); rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Errorf("/unlock with the lock off: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// A change is live at once: the owner turns the lock on, and the idle session is locked.
	hash, _ := core.HashPIN(testPIN)
	f.now = f.now.Add(10 * time.Hour)
	if err := f.s.WriteSettings(core.Settings{LockAfter: "5", LockPINHash: hash}); err != nil {
		t.Fatal(err)
	}
	if !f.isLocked() {
		t.Error("an idle session is open after the lock was turned on")
	}
}

func TestAnUnreadableLockSettingLocksTheBrowser(t *testing.T) {
	logs := &bytes.Buffer{}
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	f := newLockFixture(t, "15")
	if err := os.WriteFile(f.s.SettingsPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !f.isLocked() {
		t.Error("an unreadable settings file opened the session")
	}
	// A fault of the server is not a wrong PIN: it is 500, and it is not counted, so it never ends the session.
	for i := range 6 {
		if rec := f.post("/unlock", "pin="+testPIN, nil); rec.Code != 500 {
			t.Fatalf("unlock %d with unreadable settings: %d %q", i+1, rec.Code, rec.Header().Get("Location"))
		}
	}
	if strings.Contains(logs.String(), "the PIN does not match") {
		t.Errorf("a read error counted as a wrong PIN:\n%s", logs.String())
	}
	hash, err := core.HashPIN(testPIN)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.WriteSettings(core.Settings{LockAfter: "15", LockPINHash: hash}); err != nil {
		t.Fatal(err)
	}
	if rec := f.post("/unlock", "pin="+testPIN, nil); rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Errorf("unlock after the file is good again: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if !strings.Contains(logs.String(), "cannot be read") {
		t.Errorf("the read error is not logged:\n%s", logs.String())
	}
	if rec := request(f.h, "GET", "/days/"+secretDay, "", map[string]string{"Authorization": "Bearer " + testToken}); rec.Code != 200 {
		t.Errorf("bearer with unreadable settings: %d", rec.Code)
	}
}

func TestTheOriginGuardHoldsForTheLockRoutes(t *testing.T) {
	f := newLockFixture(t, "15")
	cross := map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}
	for _, path := range []string{"/lock", "/unlock", "/awake"} {
		if rec := f.post(path, "pin="+testPIN, cross); rec.Code != 403 {
			t.Errorf("cross-site POST %s: %d", path, rec.Code)
		}
	}
	if f.isLocked() {
		t.Error("a cross-site POST /lock locked the session")
	}
}

func TestLockSettings(t *testing.T) {
	h, s := publicHandler(t, api.PublicOptions{Token: testToken})
	s.SettingsPath = filepath.Join(t.TempDir(), "life.settings.json")
	owner := map[string]string{"Authorization": "Bearer " + testToken, "Accept": "application/vnd.siren+json"}
	save := func(headers map[string]string, body string) *httptest.ResponseRecorder {
		hh := map[string]string{}
		for k, v := range owner {
			hh[k] = v
		}
		for k, v := range headers {
			hh[k] = v
		}
		return request(h, "POST", "/settings", body, hh)
	}
	for _, pin := range []string{"abc1", "123", "123456789", "12 34", "١٢٣٤"} {
		if rec := save(nil, "lock_after=10&lock_pin="+url.QueryEscape(pin)); rec.Code != 422 {
			t.Errorf("PIN %q: %d, want 422", pin, rec.Code)
		}
	}
	if rec := save(nil, "lock_after=7&lock_pin=1234"); rec.Code != 422 {
		t.Errorf("lock_after 7: %d, want 422", rec.Code)
	}
	if rec := save(nil, "lock_after=10"); rec.Code != 422 {
		t.Errorf("lock_after with no PIN: %d, want 422", rec.Code)
	}
	if _, err := os.Stat(s.SettingsPath); err == nil {
		t.Error("a refused save wrote the file")
	}
	if rec := save(map[string]string{siren.SourceHeader: "agent:x"}, "lock_pin=1234"); rec.Code != 403 {
		t.Errorf("an agent sets the PIN: %d, want 403", rec.Code)
	}
	if rec := save(nil, "lock_after=10&lock_pin=13579"); rec.Code != 200 {
		t.Fatalf("a good save: %d %.300s", rec.Code, rec.Body.String())
	}
	file, err := os.ReadFile(s.SettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(file), "13579") || !strings.Contains(string(file), "lock_pin_hash") || strings.Contains(string(file), `"lock_pin"`) {
		t.Errorf("the settings file: %s", file)
	}
	got, _ := s.ReadSettings()
	if got.LockMinutes() != 10 || !core.PINMatches(got.LockPINHash, "13579") || core.PINMatches(got.LockPINHash, "13570") {
		t.Errorf("stored settings: %+v", got)
	}
	// The read shows the minutes, never the hash or the PIN; the action is offered with no PIN in it.
	read := request(h, "GET", "/settings", "", owner)
	for _, leak := range []string{"pbkdf2", "13579", "lock_pin_hash"} {
		if strings.Contains(read.Body.String(), leak) {
			t.Errorf("GET /settings shows %q", leak)
		}
	}
	if !strings.Contains(read.Body.String(), `"lock_after": "10"`) {
		t.Errorf("GET /settings: %.600s", read.Body.String())
	}
	// An empty PIN keeps the stored one (a form never shows it); a new PIN replaces it; off keeps the hash.
	if rec := save(nil, "lock_after=30&lock_pin="); rec.Code != 200 {
		t.Fatalf("empty PIN: %d", rec.Code)
	}
	got, _ = s.ReadSettings()
	if got.LockMinutes() != 30 || !core.PINMatches(got.LockPINHash, "13579") {
		t.Errorf("empty PIN did not keep the stored one: %+v", got)
	}
	if rec := save(nil, "lock_after=off&lock_pin=2468"); rec.Code != 200 {
		t.Fatalf("off: %d", rec.Code)
	}
	got, _ = s.ReadSettings()
	if got.LockAfter != "" || got.LockMinutes() != 0 || !core.PINMatches(got.LockPINHash, "2468") {
		t.Errorf("after off: %+v", got)
	}
	// The same through a browser form: the PIN field is empty in the form.
	form := request(h, "GET", "/settings", "", map[string]string{"Authorization": "Bearer " + testToken, "Accept": "text/html"})
	if !strings.Contains(form.Body.String(), `name="lock_pin"`) || strings.Contains(form.Body.String(), "2468") || strings.Contains(form.Body.String(), "pbkdf2") {
		t.Errorf("the settings form: %d", form.Code)
	}
}

func TestPINHash(t *testing.T) {
	a, err := core.HashPIN("0042")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := core.HashPIN("0042")
	if a == b {
		t.Error("two hashes of one PIN are equal: the salt is not random")
	}
	if !core.PINMatches(a, "0042") || core.PINMatches(a, "42") || core.PINMatches(a, "") {
		t.Error("PINMatches")
	}
	for _, bad := range []string{"", "x", "pbkdf2-sha256$1$a$b", "pbkdf2-sha256$0$AA$AA", "md5$1$AA$AA"} {
		if core.PINMatches(bad, "0042") {
			t.Errorf("%q matches", bad)
		}
	}
}
