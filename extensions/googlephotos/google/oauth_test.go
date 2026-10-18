package google

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The vector of RFC 7636, appendix B.
func TestPKCEChallengeMatchesTheRFCVector(t *testing.T) {
	got := PKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got != want {
		t.Errorf("challenge = %s, want %s", got, want)
	}
}

func TestAuthURLCarriesTheParametersGoogleNeeds(t *testing.T) {
	a := &Auth{Config: Config{ClientID: "id", AuthURL: DefaultAuthURL}}
	u, err := url.Parse(a.AuthURL("http://127.0.0.1:7790/oauth/callback", "st", "ver"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"response_type": "code", "client_id": "id", "redirect_uri": "http://127.0.0.1:7790/oauth/callback",
		"scope": Scope, "state": "st", "code_challenge": PKCEChallenge("ver"), "code_challenge_method": "S256",
		"access_type": "offline", "prompt": "consent",
	}
	for k, v := range want {
		if got := u.Query().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if u.Host != "accounts.google.com" {
		t.Errorf("host = %s", u.Host)
	}
}

// fakeToken is a token endpoint. It records the forms it got.
type fakeToken struct {
	forms  []url.Values
	status int
	body   map[string]any
}

func (f *fakeToken) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.forms = append(f.forms, r.PostForm)
	w.Header().Set("Content-Type", "application/json")
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	_ = json.NewEncoder(w).Encode(f.body)
}

func newAuth(t *testing.T, f *fakeToken, now time.Time) *Auth {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Auth{
		Config:    Config{ClientID: "cid", ClientSecret: "sec", TokenURL: srv.URL},
		HTTP:      srv.Client(),
		TokenPath: filepath.Join(t.TempDir(), "sub", "token.json"),
		Now:       func() time.Time { return now },
	}
}

func TestExchangeKeepsTheTokenInAFileOnlyTheOwnerReads(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := &fakeToken{body: map[string]any{"access_token": "A1", "refresh_token": "R1", "expires_in": 3600}}
	a := newAuth(t, f, now)
	if err := a.Exchange(context.Background(), "the-code", "ver", "http://127.0.0.1:1/oauth/callback"); err != nil {
		t.Fatal(err)
	}
	form := f.forms[0]
	for k, v := range map[string]string{"grant_type": "authorization_code", "code": "the-code", "code_verifier": "ver",
		"redirect_uri": "http://127.0.0.1:1/oauth/callback", "client_id": "cid", "client_secret": "sec"} {
		if form.Get(k) != v {
			t.Errorf("form %s = %q, want %q", k, form.Get(k), v)
		}
	}
	if !a.Connected() {
		t.Error("not connected after the exchange")
	}
	if runtime.GOOS == "linux" {
		fi, err := os.Stat(a.TokenPath)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("token file mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(filepath.Dir(a.TokenPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the token folder holds %d files, want 1 (no temp file left)", len(entries))
	}
}

func TestAnExpiredTokenIsRefreshedAndKeepsItsRefreshToken(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := &fakeToken{body: map[string]any{"access_token": "A1", "refresh_token": "R1", "expires_in": 3600}}
	a := newAuth(t, f, now)
	ctx := context.Background()
	if err := a.Exchange(ctx, "c", "v", "r"); err != nil {
		t.Fatal(err)
	}
	// A valid token is not refreshed.
	if tok, err := a.AccessToken(ctx); err != nil || tok != "A1" || len(f.forms) != 1 {
		t.Fatalf("AccessToken = %q, %v after %d calls; want A1 and no refresh", tok, err, len(f.forms))
	}
	// Two hours later it is expired. The refresh answer has no refresh_token.
	a.Now = func() time.Time { return now.Add(2 * time.Hour) }
	f.body = map[string]any{"access_token": "A2", "expires_in": 3600}
	tok, err := a.AccessToken(ctx)
	if err != nil || tok != "A2" {
		t.Fatalf("AccessToken = %q, %v; want A2", tok, err)
	}
	form := f.forms[1]
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "R1" {
		t.Errorf("refresh form = %v", form)
	}
	// A new Auth on the same file reads the kept refresh token.
	b := &Auth{Config: a.Config, HTTP: a.HTTP, TokenPath: a.TokenPath, Now: func() time.Time { return now.Add(2 * time.Hour) }}
	f.body = map[string]any{"access_token": "A3", "expires_in": 3600}
	if _, err := b.AccessToken(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.forms[len(f.forms)-1].Get("refresh_token"); got != "R1" {
		t.Errorf("the refresh token after a refresh = %q, want R1", got)
	}
}

func TestARefusedRefreshTokenMeansSignInAgain(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := &fakeToken{body: map[string]any{"access_token": "A1", "refresh_token": "R1", "expires_in": 1}}
	a := newAuth(t, f, now)
	if err := a.Exchange(context.Background(), "c", "v", "r"); err != nil {
		t.Fatal(err)
	}
	f.status, f.body = http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "Token has been expired or revoked."}
	_, err := a.AccessToken(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("error = %v, want ErrNotConnected", err)
	}
}

func TestAccessTokenWithoutAFileIsNotConnected(t *testing.T) {
	a := &Auth{TokenPath: filepath.Join(t.TempDir(), "token.json")}
	if a.Connected() {
		t.Error("Connected is true without a token file")
	}
	if _, err := a.AccessToken(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Errorf("error = %v, want ErrNotConnected", err)
	}
}

func TestLoadCredentialsReadsADesktopClient(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"installed":{"client_id":"i","client_secret":"s","auth_uri":"https://evil.example/","token_uri":"https://evil.example/t","redirect_uris":["http://localhost"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCredentials(good)
	if err != nil {
		t.Fatal(err)
	}
	if c.ClientID != "i" || c.ClientSecret != "s" || c.TokenURL != DefaultTokenURL || c.AuthURL != DefaultAuthURL {
		t.Errorf("config = %+v: the endpoints must be the fixed ones of Google", c)
	}
	web := filepath.Join(dir, "web.json")
	if err := os.WriteFile(web, []byte(`{"web":{"client_id":"i","client_secret":"s"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredentials(web); err == nil {
		t.Error("a web client was accepted, want a refusal")
	}
}
