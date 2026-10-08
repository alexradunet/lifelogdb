package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/db"
)

const testToken = "synthetic-token-0123456789abcdefghij"

func TestPublicFlagsParse(t *testing.T) {
	o, err := parse([]string{"serve", "--public", "--allow-origin", "https://a.example", "--allow-origin=http://localhost:3000", "--token-file", "t.txt", "--tls-cert", "c.pem", "--tls-key=k.pem"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.public || o.tokenFile != "t.txt" || o.tlsCert != "c.pem" || o.tlsKey != "k.pem" || o.addrExplicit ||
		!reflect.DeepEqual(o.allowOrigins, []string{"https://a.example", "http://localhost:3000"}) {
		t.Fatalf("%+v", o)
	}
	if _, err := parse([]string{"serve", "--allow-origin"}); err == nil {
		t.Fatal("--allow-origin without a value accepted")
	}
	o, err = parse([]string{"serve", "--addr", "0.0.0.0:0"})
	if err != nil || !o.addrExplicit || o.public {
		t.Fatalf("%+v %v", o, err)
	}
}

func TestPublicOptionsRefusals(t *testing.T) {
	t.Setenv("LIFELOG_TOKEN", "")
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
			err := runContext(context.Background(), o)
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
	t.Setenv("LIFELOG_TOKEN", testToken)
	if pub, err := publicOptions(opts{public: true}); err != nil || pub.Token != testToken {
		t.Fatalf("%+v %v", pub, err)
	}
	if pub, err := publicOptions(opts{}); err != nil || pub != nil {
		t.Fatalf("without --public the token in the environment is ignored: %+v %v", pub, err)
	}
}

// TestPublicServeProcess runs the binary with --public on a loopback address (a non-loopback bind is covered by
// api.PublicAddress; loopback keeps this test clear of the Windows firewall prompt).
func TestPublicServeProcess(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "lifelog")
	if os.PathSeparator == '\\' {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	live := filepath.Join(root, "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(root, "token.txt")
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Without a token the process refuses before it listens.
	refused := exec.CommandContext(ctx, binary, "--db", live, "serve", "--public", "--addr", "127.0.0.1:0")
	refused.Env = append(os.Environ(), "LIFELOG_TOKEN=")
	if out, err := refused.CombinedOutput(); err == nil || !strings.Contains(string(out), "needs a token") {
		t.Fatalf("serve --public without a token: err=%v out=%s", err, out)
	}
	cmd := exec.CommandContext(ctx, binary, "--db", live, "serve", "--public", "--addr", "127.0.0.1:0", "--token-file", tokenFile)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	startup, err := bufio.NewReader(stderr).ReadString('\n')
	if err != nil {
		t.Fatalf("server startup: %q, %v", startup, err)
	}
	if !strings.Contains(startup, "public: token required") || !strings.Contains(startup, "in clear") {
		t.Fatalf("startup line does not say what it serves: %q", startup)
	}
	_, rest, ok := strings.Cut(strings.TrimSpace(startup), " on ")
	base, _, _ := strings.Cut(rest, " ")
	if !ok || !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("server startup: %q", startup)
	}
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
	handler, err := api.Public(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), api.PublicOptions{Token: testToken, TLS: true})
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
	if res.StatusCode != 204 || res.TLS == nil {
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
