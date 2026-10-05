package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientExactNumbers(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, token := range []string{"9007199254740993", "9223372036854775807", "-9223372036854775808"} {
			t.Run(fmt.Sprintf("%v/%s", remote, token), func(t *testing.T) {
				h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "POST" {
						if r.URL.Path != "/echo/"+token || r.FormValue("value") != token {
							t.Errorf("rounded dispatch: %s %s", r.URL.Path, r.FormValue("value"))
						}
						fmt.Fprint(w, `{}`)
						return
					}
					fmt.Fprintf(w, `{"properties":{"nested":[%s]},"result":{"nested":{"id":%s}},"actions":[{"name":"echo","method":"POST","href":"/echo/{id}","fields":[{"name":"id","in":"path","value":%s},{"name":"value","value":%s}]}]}`, token, token, token, token)
				})
				c := InProcess(h, "cli")
				if remote {
					srv := httptest.NewServer(h)
					defer srv.Close()
					c = Remote(srv.URL, "cli")
				}
				e, err := c.Get("/")
				if err != nil {
					t.Fatal(err)
				}
				values := []any{e.Properties.(map[string]any)["nested"].([]any)[0], e.Result.(map[string]any)["nested"].(map[string]any)["id"], e.Actions[0].Fields[0].Value}
				for _, v := range values {
					n, ok := v.(json.Number)
					if !ok || string(n) != token {
						t.Fatalf("got %T %v want exact %s", v, v, token)
					}
				}
				if _, err := c.Do(e.Actions[0], nil); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestClientRejectsTrailingJSON(t *testing.T) {
	for _, body := range []string{`{} {}`, `{} null`, `{} trailing`} {
		c := InProcess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }), "cli")
		if _, err := c.Get("/"); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}
