package ticktick

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fake answers the calls of the API with fixed JSON and records the last request.
type fake struct {
	srv    *httptest.Server
	method string
	path   string
	auth   string
	body   string
	status int
	answer string
}

func newFake(t *testing.T) (*fake, *Client) {
	t.Helper()
	f := &fake{status: 200}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.method, f.path, f.auth, f.body = r.Method, r.URL.Path, r.Header.Get("Authorization"), string(b)
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.answer)
	}))
	t.Cleanup(f.srv.Close)
	return f, &Client{Base: f.srv.URL + "/open/v1", Token: "tok"}
}

func TestEveryCallUsesItsRouteAndTheToken(t *testing.T) {
	ctx := context.Background()
	f, c := newFake(t)
	tests := []struct {
		name         string
		answer       string
		call         func() (any, error)
		method, path string
		want         any
		wantBodyHas  string
	}{
		{"projects", `[{"id":"p1","name":"Home"}]`, func() (any, error) { return c.Projects(ctx) },
			"GET", "/open/v1/project", []Project{{ID: "p1", Name: "Home"}}, ""},
		{"project data", `{"project":{"id":"p1"},"tasks":[{"id":"t1","title":"A"}]}`,
			func() (any, error) { return c.ProjectData(ctx, "p1") },
			"GET", "/open/v1/project/p1/data", []Task{{ID: "t1", Title: "A"}}, ""},
		{"task", `{"id":"t1","title":"A","projectId":"p1"}`, func() (any, error) { return c.Task(ctx, "p1", "t1") },
			"GET", "/open/v1/project/p1/task/t1", Task{ID: "t1", ProjectID: "p1", Title: "A"}, ""},
		{"create", `{"id":"t2","title":"Milk","projectId":"p1"}`,
			func() (any, error) { return c.Create(ctx, Task{Title: "Milk", ProjectID: "p1"}) },
			"POST", "/open/v1/task", Task{ID: "t2", ProjectID: "p1", Title: "Milk"}, `"title":"Milk"`},
		{"complete", ``, func() (any, error) { return nil, c.Complete(ctx, "p1", "t1") },
			"POST", "/open/v1/project/p1/task/t1/complete", nil, ""},
		{"completed", `[{"id":"t1","status":2}]`,
			func() (any, error) {
				return c.Completed(ctx, time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), time.Date(2030, 1, 3, 3, 4, 5, 0, time.UTC), nil)
			},
			"POST", "/open/v1/task/completed", []Task{{ID: "t1", Status: 2}}, `"startDate":"2030-01-02T03:04:05.000+0000"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f.answer = tc.answer
			got, err := tc.call()
			if err != nil {
				t.Fatal(err)
			}
			if f.method != tc.method || f.path != tc.path {
				t.Errorf("request = %s %s, want %s %s", f.method, f.path, tc.method, tc.path)
			}
			if f.auth != "Bearer tok" {
				t.Errorf("Authorization = %q, want Bearer tok", f.auth)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("answer = %+v, want %+v", got, tc.want)
			}
			if !strings.Contains(f.body, tc.wantBodyHas) {
				t.Errorf("body = %s, want it to hold %s", f.body, tc.wantBodyHas)
			}
		})
	}
}

func TestCompletedWithoutTheEndpointIsTold(t *testing.T) {
	f, c := newFake(t)
	for _, status := range []int{404, 405} {
		f.status = status
		_, err := c.Completed(context.Background(), time.Now(), time.Now(), nil)
		if !errors.Is(err, ErrNoCompletedEndpoint) {
			t.Errorf("status %d: err = %v, want ErrNoCompletedEndpoint", status, err)
		}
	}
}

func TestANon2xxAnswerIsAStatusErrorWithoutTheBody(t *testing.T) {
	f, c := newFake(t)
	f.status, f.answer = 401, `{"message":"the owner's private words"}`
	_, err := c.Projects(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 401 {
		t.Fatalf("err = %v, want StatusError 401", err)
	}
	if strings.Contains(err.Error(), "private") {
		t.Errorf("the error holds the body: %v", err)
	}
	f.status = 500
	if _, err := c.Completed(context.Background(), time.Now(), time.Now(), nil); !errors.As(err, &se) || se.Status != 500 {
		t.Errorf("a 500 of Completed = %v, want StatusError 500", err)
	}
}

func TestAnAnswerThatIsNotJSONIsAnErrorWithoutTheBody(t *testing.T) {
	f, c := newFake(t)
	f.answer = `<html>secret</html>`
	_, err := c.Projects(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %v, want an error that holds no body", err)
	}
}

func TestDoneReadsTheInstantInTheZoneOfTheTask(t *testing.T) {
	tests := []struct {
		name, completed, zone, wantDay, wantClock string
		ok                                        bool
	}{
		{"a zone ahead of UTC crosses midnight", "2030-03-01T23:30:00.000+0000", "Asia/Tokyo", "2030-03-02", "08:30", true},
		{"a zone behind UTC crosses midnight", "2030-03-01T02:30:00.000+0000", "America/New_York", "2030-02-28", "21:30", true},
		{"an offset in the string", "2030-03-01T23:30:00.000+0200", "UTC", "2030-03-01", "21:30", true},
		{"no milliseconds", "2030-03-01T10:00:00+0000", "UTC", "2030-03-01", "10:00", true},
		{"none", "", "UTC", "", "", false},
		{"not a time", "yesterday", "UTC", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at, ok := Task{CompletedTime: tc.completed, TimeZone: tc.zone}.Done()
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && (at.Format("2006-01-02") != tc.wantDay || at.Format("15:04") != tc.wantClock) {
				t.Errorf("got %s, want %s %s", at.Format("2006-01-02 15:04"), tc.wantDay, tc.wantClock)
			}
		})
	}
}

func TestDoneWithoutAZoneIsInTheLocalZone(t *testing.T) {
	at, ok := Task{CompletedTime: "2030-03-01T10:00:00.000+0000"}.Done()
	if !ok || at.Location() != time.Local {
		t.Errorf("zone = %v ok = %v, want the local zone", at.Location(), ok)
	}
	at, _ = Task{CompletedTime: "2030-03-01T10:00:00.000+0000", TimeZone: "Not/AZone"}.Done()
	if at.Location() != time.Local {
		t.Errorf("an unknown zone gave %v, want the local zone", at.Location())
	}
}

func TestAuthorizeLinkCarriesTheFields(t *testing.T) {
	o := OAuth{ClientID: "id", AuthorizeURL: "https://x.example/oauth/authorize", RedirectURI: "http://127.0.0.1:8765/callback"}
	u, err := url.Parse(o.AuthorizeLink("st"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	want := map[string]string{"client_id": "id", "scope": "tasks:read tasks:write", "state": "st",
		"redirect_uri": "http://127.0.0.1:8765/callback", "response_type": "code"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if u.Host != "x.example" || u.Path != "/oauth/authorize" {
		t.Errorf("link = %s", u)
	}
}

func TestExchangeSendsBasicAuthAndTheFormFields(t *testing.T) {
	var gotUser, gotPass, gotType string
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotType = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		form = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "the-token"})
	}))
	defer srv.Close()
	o := OAuth{ClientID: "id", ClientSecret: "secret", TokenURL: srv.URL, RedirectURI: "http://127.0.0.1:8765/callback"}
	tok, err := o.Exchange(context.Background(), "the-code")
	if err != nil || tok != "the-token" {
		t.Fatalf("token = %q, err = %v", tok, err)
	}
	if gotUser != "id" || gotPass != "secret" {
		t.Errorf("basic auth = %q %q, want id secret", gotUser, gotPass)
	}
	if gotType != "application/x-www-form-urlencoded" {
		t.Errorf("content type = %q", gotType)
	}
	want := url.Values{"code": {"the-code"}, "grant_type": {"authorization_code"}, "scope": {"tasks:read tasks:write"},
		"redirect_uri": {"http://127.0.0.1:8765/callback"}}
	if !reflect.DeepEqual(form, want) {
		t.Errorf("form = %v, want %v", form, want)
	}
}

func TestExchangeRefusals(t *testing.T) {
	status, answer := 400, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	defer srv.Close()
	o := OAuth{TokenURL: srv.URL}
	var se *StatusError
	if _, err := o.Exchange(context.Background(), "c"); !errors.As(err, &se) || se.Status != 400 {
		t.Errorf("err = %v, want StatusError 400", err)
	}
	status, answer = 200, `{}`
	if _, err := o.Exchange(context.Background(), "c"); err == nil {
		t.Error("an answer with no access_token must be an error")
	}
}
