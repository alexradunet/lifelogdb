package importrun

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewLocalRefusesAHostThatIsNotThisMachine(t *testing.T) {
	for _, u := range []string{
		"http://10.0.0.5:8080/v1",      // a private address
		"http://192.168.1.20/v1",       // a machine on the home network
		"http://[2001:db8::1]:8080/v1", // an IPv6 address
		"http://example.com/v1",        // a public name (refused also when it cannot be resolved)
	} {
		if l, err := NewLocal(ctx, u, "m"); !errors.Is(err, ErrNotLoopback) || l != nil {
			t.Errorf("%s: %v, %v", u, l, err)
		}
	}
	for _, u := range []string{"ftp://127.0.0.1/v1", "http://user@127.0.0.1/v1", "127.0.0.1:8080", "http:///v1"} {
		if _, err := NewLocal(ctx, u, "m"); err == nil {
			t.Errorf("%s was accepted", u)
		}
	}
}

// modelServer is a loopback model server with one model; it records the completions it was asked for.
func modelServer(t *testing.T, models ...string) (*httptest.Server, *atomic.Int64, func() []map[string]any) {
	t.Helper()
	var hits atomic.Int64
	var mu sync.Mutex
	var asked []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		data := []map[string]string{}
		for _, m := range models {
			data = append(data, map[string]string{"id": m})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad JSON", 400)
			return
		}
		mu.Lock()
		asked = append(asked, body)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": `{"writes": []}`}}}})
	})
	mux.HandleFunc("POST /v1/elsewhere", func(w http.ResponseWriter, r *http.Request) { t.Error("a redirect was followed") })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), asked...)
	}
}

func TestLocalAsksTheOneModelAtTemperatureZero(t *testing.T) {
	// a proxy in the environment is not used: the text goes to the model on this machine only
	t.Setenv("HTTP_PROXY", "http://10.255.255.1:9")
	t.Setenv("HTTPS_PROXY", "http://10.255.255.1:9")
	srv, hits, asked := modelServer(t, "tiny-model")
	l, err := NewLocal(ctx, srv.URL+"/v1/", "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "tiny-model" {
		t.Errorf("model %q, want the one the server lists", l.Name)
	}
	answer, err := l.Facts(ctx, "the instructions", "the file", 123)
	if err != nil || answer != `{"writes": []}` {
		t.Fatalf("answer %q, %v", answer, err)
	}
	completions := asked()
	if hits.Load() != 2 || len(completions) != 1 {
		t.Fatalf("requests %d, completions %d", hits.Load(), len(completions))
	}
	got := completions[0]
	thinking, _ := got["chat_template_kwargs"].(map[string]any)
	msgs, _ := got["messages"].([]any)
	if got["model"] != "tiny-model" || got["temperature"] != 0.0 || got["max_tokens"] != 123.0 || thinking["enable_thinking"] != false || len(msgs) != 2 {
		t.Errorf("the completion asked: %v", got)
	}
}

func TestLocalWantsANameWhenTheServerListsSeveralModels(t *testing.T) {
	srv, _, _ := modelServer(t, "a", "b")
	if _, err := NewLocal(ctx, srv.URL+"/v1", ""); err == nil || !strings.Contains(err.Error(), "--model") {
		t.Errorf("two models and no name: %v", err)
	}
	if l, err := NewLocal(ctx, srv.URL+"/v1", "b"); err != nil || l.Name != "b" {
		t.Errorf("a named model: %v, %v", l, err)
	}
}

func TestLocalFollowsNoRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			http.Redirect(w, r, "/v1/elsewhere", http.StatusTemporaryRedirect)
			return
		}
		t.Errorf("a redirect was followed to %s", r.URL.Path)
	}))
	defer srv.Close()
	l, err := NewLocal(ctx, srv.URL+"/v1", "m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Facts(ctx, "s", "u", 10); err == nil || !strings.Contains(err.Error(), "307") {
		t.Errorf("a redirect: %v", err)
	}
}

func TestLocalBoundsTheAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices": [{"message": {"content": "`))
		w.Write([]byte(strings.Repeat("x", maxAnswer)))
		w.Write([]byte(`"}}]}`))
	}))
	defer srv.Close()
	l, err := NewLocal(ctx, srv.URL+"/v1", "m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Facts(ctx, "s", "u", 10); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("an answer over the bound: %v", err)
	}
}
