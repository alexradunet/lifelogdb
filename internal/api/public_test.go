package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

const testToken = "synthetic-token-0123456789abcdefghij" // 36 characters

// publicHandler is the API behind the public guard, with one allowed browser origin.
func publicHandler(t *testing.T, o api.PublicOptions) (http.Handler, *core.Store) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := &core.Store{DB: d}
	h, err := api.Public(api.New(s, o.Origins...), o)
	if err != nil {
		t.Fatal(err)
	}
	return h, s
}

func request(h http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "192.0.2.10:7777" // what a client on another device sends
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPublicRequiresTheToken(t *testing.T) {
	h, s := publicHandler(t, api.PublicOptions{Token: testToken})
	bearer := map[string]string{"Authorization": "Bearer " + testToken}
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"no credential", nil, 401},
		{"wrong token", map[string]string{"Authorization": "Bearer " + strings.Repeat("x", 36)}, 401},
		{"last byte differs", map[string]string{"Authorization": "Bearer " + testToken[:35] + "k"}, 401},
		{"longer", map[string]string{"Authorization": "Bearer " + testToken + "x"}, 401},
		{"basic scheme", map[string]string{"Authorization": "Basic " + testToken}, 401},
		{"bearer", bearer, 200},
		{"bearer case", map[string]string{"Authorization": "bearer " + testToken}, 200},
		{"cookie", map[string]string{"Cookie": api.CookieName + "=" + testToken}, 200},
		{"wrong cookie", map[string]string{"Cookie": api.CookieName + "=" + strings.Repeat("x", 36)}, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request(h, "GET", "/days/2031-01-01", "", tc.headers)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want %d: %.200s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == 401 {
				body := rec.Body.String()
				if rec.Header().Get("WWW-Authenticate") == "" || !strings.Contains(body, "token required") || strings.Contains(body, "2031-01-01") {
					t.Fatalf("401 answer: header=%q body=%.200s", rec.Header().Get("WWW-Authenticate"), body)
				}
			}
		})
	}
	// A write with the token persists; without it nothing is written.
	if rec := request(h, "POST", "/days/2031-01-01/capture", "text=denied", nil); rec.Code != 401 {
		t.Fatalf("unauthenticated write: %d", rec.Code)
	}
	if rec := request(h, "POST", "/days/2031-01-01/capture", "text=allowed", bearer); rec.Code != 200 {
		t.Fatalf("authenticated write: %d %.200s", rec.Code, rec.Body.String())
	}
	id, err := s.PageID(t.Context(), "2031-01-01")
	if err != nil || id == 0 {
		t.Fatalf("day page after the writes: id=%d err=%v", id, err)
	}
	page, err := s.PageByID(t.Context(), id)
	if err != nil || !strings.Contains(page.Body, "allowed") || strings.Contains(page.Body, "denied") {
		t.Fatalf("body=%q err=%v", page.Body, err)
	}
	// The Host check of the local guard is not applied: the token is the defence.
	if rec := request(h, "GET", "/", "", map[string]string{"Authorization": "Bearer " + testToken, "Host": "arbitrary.invalid:1"}); rec.Code != 200 {
		t.Fatalf("foreign Host with the token: %d", rec.Code)
	}
	// Absolute-form targets stay refused.
	req := httptest.NewRequest("GET", "http://arbitrary.invalid/", nil)
	req.URL.Host = "arbitrary.invalid"
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("absolute-form target: %d", rec.Code)
	}
	// A browser without the cookie is sent to the login page, and only that page is served to it.
	rec = request(h, "GET", "/", "", map[string]string{"Accept": "text/html"})
	if rec.Code != 303 || rec.Header().Get("Location") != "/login" {
		t.Fatalf("browser without cookie: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = request(h, "GET", "/login", "", map[string]string{"Accept": "text/html"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `action="/login"`) || strings.Contains(rec.Body.String(), "2031-01-01") {
		t.Fatalf("login page: %d %.300s", rec.Code, rec.Body.String())
	}
}

func TestPublicLoginCookie(t *testing.T) {
	for _, tls := range []bool{false, true} {
		h, _ := publicHandler(t, api.PublicOptions{Token: testToken, TLS: tls})
		rec := request(h, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Accept": "text/html", "Sec-Fetch-Site": "same-origin"})
		if rec.Code != 303 || rec.Header().Get("Location") != "/" {
			t.Fatalf("tls=%v login: %d %q", tls, rec.Code, rec.Header().Get("Location"))
		}
		var cookie *http.Cookie
		for _, c := range rec.Result().Cookies() {
			if c.Name == api.CookieName {
				cookie = c
			}
		}
		if cookie == nil || cookie.Value != testToken || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Secure != tls {
			t.Fatalf("tls=%v cookie=%+v", tls, cookie)
		}
		// The cookie opens the HTML face; a same-origin form writes; a cross-site form with the cookie is refused.
		with := map[string]string{"Accept": "text/html", "Cookie": api.CookieName + "=" + cookie.Value}
		if rec := request(h, "GET", "/", "", with); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Capture") {
			t.Fatalf("home with the cookie: %d", rec.Code)
		}
		same := map[string]string{"Accept": "text/html", "Cookie": with["Cookie"], "Sec-Fetch-Site": "same-origin"}
		if rec := request(h, "POST", "/days/2031-01-02/capture", "text=form", same); rec.Code != 303 {
			t.Fatalf("same-origin form with the cookie: %d %.200s", rec.Code, rec.Body.String())
		}
		cross := map[string]string{"Accept": "text/html", "Cookie": with["Cookie"], "Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}
		if rec := request(h, "POST", "/days/2031-01-02/capture", "text=csrf", cross); rec.Code != 403 {
			t.Fatalf("cross-site form with the cookie: %d", rec.Code)
		}
		// A wrong token is sent back to the form; a cross-site login is refused; logout clears the cookie.
		if rec := request(h, "POST", "/login", "token=wrong", map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code != 303 || rec.Header().Get("Location") != "/login?failed=1" || len(rec.Result().Cookies()) != 0 {
			t.Fatalf("wrong login: %d %q cookies=%d", rec.Code, rec.Header().Get("Location"), len(rec.Result().Cookies()))
		}
		if rec := request(h, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}); rec.Code != 403 {
			t.Fatalf("cross-site login: %d", rec.Code)
		}
		rec = request(h, "POST", "/logout", "", map[string]string{"Cookie": with["Cookie"], "Sec-Fetch-Site": "same-origin"})
		cleared := false
		for _, c := range rec.Result().Cookies() {
			cleared = cleared || (c.Name == api.CookieName && c.MaxAge < 0)
		}
		if rec.Code != 303 || !cleared {
			t.Fatalf("logout: %d cleared=%v", rec.Code, cleared)
		}
	}
}

func TestPublicAllowedOrigins(t *testing.T) {
	const app = "https://app.example"
	h, _ := publicHandler(t, api.PublicOptions{Token: testToken, Origins: []string{app}})
	bearer := "Bearer " + testToken
	// Preflight from the allowed origin.
	rec := request(h, "OPTIONS", "/pages", "", map[string]string{"Origin": app, "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization,content-type"})
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != app || !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), api.SourceHeader) || !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "POST") || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
	}
	// Preflight from another origin: refused, no CORS headers.
	rec = request(h, "OPTIONS", "/pages", "", map[string]string{"Origin": "https://evil.example", "Access-Control-Request-Method": "POST"})
	if rec.Code != 403 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign preflight: %d %v", rec.Code, rec.Header())
	}
	// A read and a write from the allowed origin, as a browser sends them (fetch with the header).
	rec = request(h, "GET", "/", "", map[string]string{"Origin": app, "Sec-Fetch-Site": "cross-site", "Authorization": bearer})
	if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != app || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("read from the allowed origin: %d %v", rec.Code, rec.Header())
	}
	rec = request(h, "POST", "/pages", "title=From+the+app", map[string]string{"Origin": app, "Sec-Fetch-Site": "cross-site", "Authorization": bearer})
	if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != app || !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "Location") {
		t.Fatalf("write from the allowed origin: %d %v %.200s", rec.Code, rec.Header(), rec.Body.String())
	}
	// The same from any other origin, even with the token: no CORS headers on the read, the write refused.
	rec = request(h, "GET", "/", "", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site", "Authorization": bearer})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign read got CORS headers: %v", rec.Header())
	}
	rec = request(h, "POST", "/pages", "title=From+elsewhere", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site", "Authorization": bearer})
	if rec.Code != 403 {
		t.Fatalf("write from another origin: %d", rec.Code)
	}
	// A program that sends no browser headers writes as before.
	rec = request(h, "POST", "/pages", "title=From+a+program", map[string]string{"Authorization": bearer})
	if rec.Code != 200 {
		t.Fatalf("write without browser headers: %d", rec.Code)
	}
}

func TestPublicRemoteClientParity(t *testing.T) {
	h, s := publicHandler(t, api.PublicOptions{Token: testToken})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := client.Remote(srv.URL, "app:phone")
	if _, err := c.Get("/"); err == nil {
		t.Fatal("a remote client without the token was served")
	}
	c.SetToken(testToken)
	root, err := c.Get("/")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := c.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	var capture api.Action
	for _, a := range catalog {
		if a.Name == "capture" {
			capture = a
		}
	}
	if _, err := c.Do(capture, map[string]string{"day": "2031-01-03", "text": "from the phone"}); err != nil {
		t.Fatal(err)
	}
	id, err := s.PageID(t.Context(), "2031-01-03")
	if err != nil || id == 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	res := must(c.Do(find(root, "query"), map[string]string{"sql": fmt.Sprintf("SELECT source FROM entities WHERE id = %d", id)}))
	if got := fmtRows(res); got != "app:phone" {
		t.Fatalf("source=%q", got)
	}
}

func TestPublicOptionsValidation(t *testing.T) {
	h := http.NotFoundHandler()
	for _, token := range []string{"", "short", strings.Repeat("x", 31), testToken + "\n", "line one line two\nline three abcdefghijklmnop"} {
		if _, err := api.Public(h, api.PublicOptions{Token: token}); err == nil {
			t.Errorf("token %q accepted", token)
		}
	}
	for _, origin := range []string{"", "app.example", "https://app.example/", "https://app.example/path", "https://user@app.example", "ftp://app.example", "https://app.example?x=1", "https://"} {
		if _, err := api.Public(h, api.PublicOptions{Token: testToken, Origins: []string{origin}}); err == nil {
			t.Errorf("origin %q accepted", origin)
		}
	}
	for _, origin := range []string{"https://app.example", "http://localhost:3000", "http://192.168.1.20:8080", "http://[::1]:3000"} {
		if _, err := api.Public(h, api.PublicOptions{Token: testToken, Origins: []string{origin}}); err != nil {
			t.Errorf("origin %q refused: %v", origin, err)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"0.0.0.0:7777", "0.0.0.0:7777"}, {"[::]:7777", "[::]:7777"}, {"192.0.2.1:0", "192.0.2.1:0"}, {"localhost:7777", "127.0.0.1:7777"}, {"127.0.0.1:7777", "127.0.0.1:7777"},
	} {
		if got, err := api.PublicAddress(tc.in); err != nil || got != tc.want {
			t.Errorf("PublicAddress(%q)=%q,%v want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"", ":7777", "custom.invalid:7777", "0.0.0.0:http", "0.0.0.0:65536", "0.0.0.0:-1", "0.0.0.0:+1", "0.0.0.0", "[fe80::1%eth0]:7777"} {
		if got, err := api.PublicAddress(in); err == nil {
			t.Errorf("PublicAddress(%q)=%q accepted", in, got)
		}
	}
}
