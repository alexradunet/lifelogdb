package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

// TestServeAddressFlag: --addr, in both of its forms, reaches the parse of the loopback address (api.LoopbackAddress,
// whose own test has the table of addresses): LOCALHOST becomes 127.0.0.1, and a refused address stops serve before
// the database opens, as the file is absent.
func TestServeAddressFlag(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.db")
	e, _, _ := testEnv(t)
	for _, at := range []func(addr string) []string{
		func(addr string) []string { return []string{"--addr", addr} },
		func(addr string) []string { return []string{"--addr=" + addr} },
	} {
		o, err := parse(append([]string{"serve"}, at("LOCALHOST:7777")...), noVars)
		if err != nil {
			t.Fatal(err)
		}
		if err := prepareServe(&o); err != nil || o.addr != "127.0.0.1:7777" {
			t.Errorf("serve %v: the address %q, %v; want 127.0.0.1:7777", at("LOCALHOST:7777"), o.addr, err)
		}
		argv := append(at("0.0.0.0:7777"), "serve", "--db", absent)
		if err := runArgs(t.Context(), e, argv); err == nil || !strings.Contains(err.Error(), "serve address") {
			t.Errorf("%v: %v, want the refusal of the address", argv, err)
		}
	}
}

func TestServeAuthorityMount(t *testing.T) {
	// Run the real serve branch against a disposable synthetic database. Read
	// the printed ephemeral listener address rather than guessing a free port.
	p := dbtest.File(t)
	e, _, _ := testEnv(t)
	scheme, address := servedAt(t, serveInProcess(t, e, "serve", "--addr", "localhost:0", "--db", p))
	if strings.HasSuffix(address, ":0") {
		t.Fatalf("ephemeral port not reported: %s", address)
	}
	base := scheme + "://" + address
	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	for _, tc := range []struct {
		host string
		want int
	}{{"", 200}, {"arbitrary.invalid:7777", 403}} {
		req, _ := http.NewRequest("GET", base+"/actions", nil)
		if tc.host != "" {
			req.Host = tc.host
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("Host=%q status=%d", tc.host, res.StatusCode)
		}
	}
}

const testToken = "synthetic-token-0123456789abcdefghij"

func TestPublicFlagsParse(t *testing.T) {
	o, err := parse([]string{"serve", "--public", "--allow-origin", "https://a.example", "--allow-origin=http://localhost:3000", "--token-file", "t.txt", "--tls-cert", "c.pem", "--tls-key=k.pem"}, noVars)
	if err != nil {
		t.Fatal(err)
	}
	if !o.public || o.tokenFile != "t.txt" || o.tlsCert != "c.pem" || o.tlsKey != "k.pem" || o.addrExplicit ||
		!reflect.DeepEqual(o.allowOrigins, []string{"https://a.example", "http://localhost:3000"}) {
		t.Fatalf("%+v", o)
	}
	if _, err := parse([]string{"serve", "--allow-origin"}, noVars); err == nil {
		t.Fatal("--allow-origin without a value accepted")
	}
	o, err = parse([]string{"serve", "--addr", "0.0.0.0:0"}, noVars)
	if err != nil || !o.addrExplicit || o.public {
		t.Fatalf("%+v %v", o, err)
	}
}

func TestPublicOptionsRefusals(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.db")
	short := filepath.Join(t.TempDir(), "short.txt")
	if err := os.WriteFile(short, []byte("too short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	two := filepath.Join(t.TempDir(), "two.txt")
	if err := os.WriteFile(two, []byte(testToken+"\n"+testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(good, []byte(testToken+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		o    opts
		want string
	}{
		{"token file without --public", opts{tokenFile: good}, "--token-file needs --public"},
		{"origin without --public", opts{allowOrigins: []string{"https://a.example"}}, "--allow-origin needs --public"},
		{"tls without --public", opts{tlsCert: "c", tlsKey: "k"}, "need --public"},
		{"--public without a token", opts{public: true}, "needs a token"},
		{"short token file", opts{public: true, tokenFile: short}, "at least 32"},
		{"two-line token file", opts{public: true, tokenFile: two}, "one line"},
		{"missing token file", opts{public: true, tokenFile: filepath.Join(t.TempDir(), "none")}, "token file"},
		{"one tls flag", opts{public: true, tokenFile: good, tlsCert: "c"}, "go together"},
		{"bad origin", opts{public: true, tokenFile: good, allowOrigins: []string{"a.example"}}, "origin"},
		{"non-loopback address without --public", opts{addr: "0.0.0.0:0"}, "serve address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.o
			o.args = []string{"serve"}
			o.db = absent
			if o.addr == "" {
				o.addr = "127.0.0.1:0"
			}
			// Every refusal precedes the database opening: the file is absent.
			e, _, _ := testEnv(t)
			e.o = o
			err := runContext(t.Context(), e)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
		})
	}
	// A good token file, with its line ending trimmed, and the env fallback.
	pub, err := publicOptions(opts{public: true, tokenFile: good, allowOrigins: []string{"https://a.example"}})
	if err != nil || pub.Token != testToken || pub.TLS || !reflect.DeepEqual(pub.Origins, []string{"https://a.example"}) {
		t.Fatalf("%+v %v", pub, err)
	}
	withToken := func(name string) string { return map[string]string{"LIFELOG_TOKEN": testToken}[name] }
	for _, tc := range []struct {
		argv   []string
		public bool
	}{{[]string{"serve", "--public"}, true}, {[]string{"serve"}, false}} {
		o, err := parse(tc.argv, withToken)
		if err != nil {
			t.Fatal(err)
		}
		pub, err := publicOptions(o)
		switch {
		case tc.public && (err != nil || pub.Token != testToken):
			t.Fatalf("%v: %+v %v, want the token of the environment", tc.argv, pub, err)
		case !tc.public && (err != nil || pub != nil):
			t.Fatalf("without --public the token in the environment is ignored: %+v %v", pub, err)
		}
	}
}

// TestPublicServe runs serve --public in-process on a loopback address (a non-loopback bind is covered by
// api.PublicAddress; loopback keeps this test clear of the Windows firewall prompt).
func TestPublicServe(t *testing.T) {
	live := dbtest.File(t)
	tokenFile := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	// Without a token the command refuses before it listens: main writes the error and exits 1.
	e, _, _ := testEnv(t) // no LIFELOG_TOKEN in its environment
	if err := runArgs(ctx, e, []string{"--db", live, "serve", "--public", "--addr", "127.0.0.1:0"}); err == nil || !strings.Contains(err.Error(), "needs a token") {
		t.Fatalf("serve --public without a token: %v", err)
	}
	const app = "https://app.example"
	startup := serveInProcess(t, e, "--db", live, "serve", "--public", "--addr", "127.0.0.1:0", "--token-file", tokenFile, "--allow-origin", app)
	if !strings.Contains(startup, "public: token required") || !strings.Contains(startup, "in clear") {
		t.Fatalf("startup line does not say what it serves: %q", startup)
	}
	scheme, address := servedAt(t, startup)
	if scheme != "http" || !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatalf("server startup: %q", startup)
	}
	base := scheme + "://" + address
	hc := &http.Client{Timeout: 10 * time.Second}
	get := func(token string) int {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", base+"/actions", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := get(""); got != 401 {
		t.Fatalf("without the token: %d", got)
	}
	if got := get(strings.Repeat("x", 36)); got != 401 {
		t.Fatalf("with a wrong token: %d", got)
	}
	if got := get(testToken); got != 200 {
		t.Fatalf("with the token: %d", got)
	}
	// --allow-origin reaches the guard of the API's writes: a browser write from that origin is done, and one from
	// another origin is refused.
	post := func(origin string) int {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "POST", base+"/pages", strings.NewReader("title=From+"+strings.TrimPrefix(origin, "https://")))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Origin", origin)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := post(app); got != 200 {
		t.Fatalf("a write from the allowed origin: %d", got)
	}
	if got := post("https://evil.example"); got != 403 {
		t.Fatalf("a write from another origin: %d", got)
	}
}

func TestTLSListener(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "lifelog test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tlsListener(nil, certFile, filepath.Join(dir, "missing.pem")); err == nil {
		t.Fatal("a missing key was accepted")
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tlsListener(raw, certFile, keyFile)
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	// A synthetic database: the API that api.Public builds needs a store. Its cleanup closes it after the server's
	// shutdown below: a cleanup runs after the ones registered later.
	handler, err := api.Public(&core.Store{DB: dbtest.New(t)}, api.PublicOptions{Token: testToken, TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler, ErrorLog: log.New(io.Discard, "", 0)} // the plain request below logs a handshake error
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, srv, listener, 5*time.Second) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("shutdown timed out")
		}
	})
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", "https://"+listener.Addr().String()+"/", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.TLS == nil {
		t.Fatalf("status=%d tls=%v", res.StatusCode, res.TLS != nil)
	}
	// A plain request on the TLS port is answered by the server's own 400, never by the handler.
	plain, err := hc.Get("http://" + listener.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	plain.Body.Close()
	if plain.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain HTTP on the TLS listener: %d", plain.StatusCode)
	}
}

func TestServeShutdownDeadlineCancelsBlockedRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started := make(chan struct{})
	type outcome struct{ read, context error }
	finished := make(chan outcome, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		_, err := io.Copy(io.Discard, r.Body)
		finished <- outcome{err, r.Context().Err()}
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- serveHTTP(ctx, srv, listener, 20*time.Millisecond) }()
	defer srv.Close()
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(connection, "POST / HTTP/1.1\r\nHost: %s\r\nContent-Length: 5\r\n\r\n", listener.Addr()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown error = %v; want expired drain deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked request prevented bounded shutdown")
	}
	select {
	case got := <-finished:
		if got.read == nil || !errors.Is(got.context, context.Canceled) {
			t.Fatalf("expired shutdown left the request running: read=%v context=%v", got.read, got.context)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expired shutdown did not release the blocked handler")
	}
}

func TestServeFailureJoinsShutdownWithoutContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	returned := make(chan error, 1)
	go func() { returned <- serveHTTP(context.Background(), &http.Server{}, listener, time.Second) }()
	select {
	case err := <-returned:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("serve error = %v; want closed listener", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server error left shutdown waiting for command cancellation")
	}
}
