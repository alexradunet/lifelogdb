package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"lifelog-ticktick/ticktick"
	"lifelog/client"
	"lifelog/siren"
)

// The end-to-end test builds the real lifelog and lifelog-ticktick into one folder and runs
// `lifelog serve --with ticktick` on a new life.db, with a fake TickTick. It holds the whole line: the handshake, the
// merged catalog, the capture with its import_key, the forwarding to the app and the exit rule. The live TickTick
// and its login have no test.

// wait is a guard against a deadlock, not a measure of speed.
const wait = 2 * time.Minute

// lockedBuffer is the stderr of the server: the process writes it and the test reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// fakeTick is a TickTick in memory with two lists, an Inbox and the two switches of the test.
type fakeTick struct {
	mu           sync.Mutex
	tasks        map[string]*ticktick.Task
	noCompleted  bool
	unauthorized bool
	nextID       int
}

var lists = []ticktick.Project{{ID: "p-home", Name: "Home"}, {ID: "p-work", Name: "Work"}}

func (f *fakeTick) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unauthorized || r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/open/v1")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case r.Method == "GET" && path == "/project":
		_ = json.NewEncoder(w).Encode(lists)
	case r.Method == "GET" && len(parts) == 3 && parts[2] == "data":
		open := []ticktick.Task{}
		for _, t := range f.tasks {
			if t.ProjectID == parts[1] && t.Status == 0 {
				open = append(open, *t)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": open})
	case r.Method == "GET" && len(parts) == 4 && parts[2] == "task":
		if t, ok := f.tasks[parts[3]]; ok && t.ProjectID == parts[1] {
			_ = json.NewEncoder(w).Encode(t)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case r.Method == "POST" && path == "/task":
		var in ticktick.Task
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.nextID++
		in.ID = fmt.Sprintf("new%d", f.nextID)
		f.tasks[in.ID] = &in
		_ = json.NewEncoder(w).Encode(in)
	case r.Method == "POST" && len(parts) == 5 && parts[4] == "complete":
		if t, ok := f.tasks[parts[3]]; ok {
			t.Status, t.CompletedTime = 2, time.Now().Add(-time.Minute).UTC().Format(ticktick.TimeLayout)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case r.Method == "POST" && path == "/task/completed":
		if f.noCompleted {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		done := []ticktick.Task{}
		for _, t := range f.tasks {
			if t.Status == 2 {
				done = append(done, *t)
			}
		}
		_ = json.NewEncoder(w).Encode(done)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeTick) add(t ticktick.Task) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t.TimeZone = "UTC" // the day of a completion is then the same in the fake and in the test
	f.tasks[t.ID] = &t
}

func (f *fakeTick) complete(id string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[id]
	t.Status, t.CompletedTime = 2, at.UTC().Format(ticktick.TimeLayout)
}

func (f *fakeTick) byTitle(title string) (ticktick.Task, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.Title == title {
			return *t, true
		}
	}
	return ticktick.Task{}, false
}

// bins is the folder with lifelog and lifelog-ticktick.
type bins struct{ dir, lifelog string }

func build(t *testing.T) bins {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to build lifelog: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "lifelog")); err != nil {
		t.Skipf("the lifelog sources are not at %s: %v", root, err)
	}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	b := bins{dir: t.TempDir()}
	b.lifelog = filepath.Join(b.dir, "lifelog"+exe)
	for _, args := range [][]string{
		{"build", "-C", root, "-o", b.lifelog, "./cmd/lifelog"},
		{"build", "-o", filepath.Join(b.dir, "lifelog-ticktick"+exe), "./cmd/lifelog-ticktick"},
	} {
		if out, err := exec.Command(goBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return b
}

// server is a running `lifelog serve --with ticktick`.
type server struct {
	t      *testing.T
	b      bins
	cmd    *exec.Cmd
	stderr *lockedBuffer
	exited chan error
	base   string
}

// startServer writes the files of the state folder (the config that points at the fake, and the token), then runs the
// server on a new life.db. XDG_CONFIG_HOME puts the state folder in a temporary folder: the owner's folder stays alone.
func startServer(t *testing.T, b bins, fake *httptest.Server) *server {
	t.Helper()
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("LIFELOG_TOKEN", "")
	state := filepath.Join(config, "lifelog-ticktick")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"api": %q, "poll": "1h"}`, fake.URL+"/open/v1")
	for name, content := range map[string]string{"config.json": cfg, "token.json": `{"access_token": "test-token"}`} {
		if err := os.WriteFile(filepath.Join(state, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(t.TempDir(), "life.db")
	if out, err := exec.Command(b.lifelog, "init", db).CombinedOutput(); err != nil {
		t.Fatalf("lifelog init: %v\n%s", err, out)
	}
	s := &server{t: t, b: b, stderr: new(lockedBuffer), exited: make(chan error, 1)}
	s.cmd = exec.Command(b.lifelog, "--db", db, "serve", "--addr", "127.0.0.1:0", "--with", "ticktick")
	s.cmd.Stderr = s.stderr
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.exited <- s.cmd.Wait() }()
	t.Cleanup(s.stop)
	line := s.waitFor("lifelog: serving ")
	_, rest, _ := strings.Cut(line, " on http://")
	addr, _, _ := strings.Cut(rest, " ")
	s.base = "http://" + addr
	return s
}

// waitFor waits until the stderr holds the text and returns the line that holds it. It reads the log, never the clock.
func (s *server) waitFor(text string) string {
	s.t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		for _, l := range strings.Split(s.stderr.String(), "\n") {
			if strings.Contains(l, text) {
				return l
			}
		}
		select {
		case err := <-s.exited:
			s.exited <- err
			s.t.Fatalf("the server ended before %q: %v\n%s", text, err, s.stderr)
		case <-time.After(20 * time.Millisecond):
		}
	}
	s.t.Fatalf("no %q in the log after %s:\n%s", text, wait, s.stderr)
	return ""
}

func (s *server) stop() {
	select {
	case err := <-s.exited:
		s.exited <- err
		return
	default:
	}
	_ = s.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-s.exited:
	case <-time.After(wait):
		_ = s.cmd.Process.Kill()
		s.t.Error("the server did not stop")
	}
}

func (s *server) client() *client.Client { return client.Remote(s.base, "", "") }

// do runs the real command line: `lifelog do ACTION ARGS... --url BASE`.
func (s *server) do(args ...string) string {
	s.t.Helper()
	out, err := exec.Command(s.b.lifelog, append(append([]string{"do"}, args...), "--url", s.base)...).CombinedOutput()
	if err != nil {
		s.t.Fatalf("lifelog do %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// act runs an action of the merged catalog through the client.
func (s *server) act(name string, values map[string]string) *siren.Entity {
	s.t.Helper()
	actions, err := s.client().Catalog(s.t.Context())
	if err != nil {
		s.t.Fatal(err)
	}
	for _, a := range actions {
		if a.Name == name {
			e, err := s.client().Do(s.t.Context(), a, values)
			if err != nil {
				s.t.Fatalf("%s: %v", name, err)
			}
			return e
		}
	}
	s.t.Fatalf("the catalog has no action %s", name)
	return nil
}

// written is the count that ticktick-sync answered.
func (s *server) written(e *siren.Entity) string {
	s.t.Helper()
	p, _ := e.Properties.(map[string]any)
	n, ok := p["written"].(json.Number)
	if !ok {
		s.t.Fatalf("the answer of ticktick-sync has no written count: %+v", e)
	}
	return n.String()
}

// body is the body of the day page of the day, "" when there is none.
func (s *server) body(day string) string {
	s.t.Helper()
	e, err := s.client().Get(s.t.Context(), "/days/"+day)
	if err != nil {
		s.t.Fatal(err)
	}
	p, _ := e.Properties.(map[string]any)
	view, _ := p["view"].([]any)
	for _, row := range view {
		r, _ := row.(map[string]any)
		if r["what"] == "day page" {
			body, _ := r["detail"].(string)
			return body
		}
	}
	return ""
}

func dayOf(at time.Time) string { return at.UTC().Format("2006-01-02") }

func TestTickTickOnTheRealLifelog(t *testing.T) {
	if testing.Short() {
		t.Skip("the end-to-end test builds lifelog; -short skips it")
	}
	b := build(t)
	for _, tc := range []struct {
		name        string
		noCompleted bool
	}{{"with /task/completed", false}, {"vanish and fetch", true}} {
		t.Run(tc.name, func(t *testing.T) { holdsTheWholeLine(t, b, tc.noCompleted) })
	}
	t.Run("a refused token ends the app with status 2", func(t *testing.T) {
		fake := &fakeTick{tasks: map[string]*ticktick.Task{}, unauthorized: true}
		srv := httptest.NewServer(fake)
		defer srv.Close()
		s := startServer(t, b, srv)
		s.waitFor("with ticktick: ended with status 2: not started again")
		if !strings.Contains(s.stderr.String(), "login again: lifelog ticktick login") {
			t.Errorf("the log has no line that tells the owner to log in:\n%s", s.stderr)
		}
	})
}

func holdsTheWholeLine(t *testing.T, b bins, noCompleted bool) {
	now := time.Now()
	today := now.Format("2006-01-02") // the day that the app calls today
	fake := &fakeTick{tasks: map[string]*ticktick.Task{}, noCompleted: noCompleted}
	fake.add(ticktick.Task{ID: "t-rent", ProjectID: "p-home", Title: "Pay rent"})
	fake.add(ticktick.Task{ID: "t-mom", ProjectID: "p-home", Title: "Call mom"})
	fake.add(ticktick.Task{ID: "t-report", ProjectID: "p-work", Title: "Write report", DueDate: today + "T00:00:00.000+0000"})
	wantFirst := "poll: wrote 0 entries"
	passportAt := now.Add(-time.Hour)
	if !noCompleted {
		// The server knows /task/completed, so a task done before the start is found by the first poll.
		fake.add(ticktick.Task{ID: "t-passport", ProjectID: "inbox1", Title: "Renew passport"})
		fake.complete("t-passport", passportAt)
		wantFirst = "poll: wrote 1 entries"
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	s := startServer(t, b, srv)
	s.waitFor("with ticktick: a catalog of 3 actions")
	s.waitFor(wantFirst)
	if noCompleted {
		s.waitFor("the server has no /task/completed")
	}

	t.Run("the catalog of the app is in the catalog of the server", func(t *testing.T) {
		actions, err := s.client().Catalog(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, a := range actions {
			got[a.Name] = a.Href
		}
		for name, href := range map[string]string{"ticktick-add-task": "/with/ticktick/tasks",
			"ticktick-complete-task": "/with/ticktick/tasks/{id}/complete", "ticktick-sync": "/with/ticktick/sync"} {
			if got[name] != href {
				t.Errorf("%s = %q, want %q", name, got[name], href)
			}
		}
	})

	if !noCompleted {
		t.Run("a task done before the start is an entry of its day, from the Inbox", func(t *testing.T) {
			body := s.body(dayOf(passportAt))
			if !regexp.MustCompile(`\(\d\d:\d\d, via ticktick\) Done: Renew passport(\n|$)`).MatchString(body) {
				t.Errorf("body of %s = %q, want an entry Done: Renew passport with no list", dayOf(passportAt), body)
			}
		})
	}

	doneAt := time.Now().Add(-5 * time.Minute)
	fake.complete("t-rent", doneAt)
	day := dayOf(doneAt)
	entry := regexp.MustCompile(`\(\d\d:\d\d, via ticktick\) Done: Pay rent · Home`)
	var afterFirst string
	t.Run("a task done in TickTick is an entry of its day, once", func(t *testing.T) {
		if n := s.written(s.act("ticktick-sync", nil)); n != "1" {
			t.Errorf("the sync wrote %s entries, want 1", n)
		}
		afterFirst = s.body(day)
		if !entry.MatchString(afterFirst) {
			t.Errorf("body of %s = %q, want %v", day, afterFirst, entry)
		}
		if n := strings.Count(afterFirst, "Done: Pay rent"); n != 1 {
			t.Errorf("the entry is in the body %d times, want 1", n)
		}
	})

	t.Run("a second sync writes nothing", func(t *testing.T) {
		if n := s.written(s.act("ticktick-sync", nil)); n != "0" {
			t.Errorf("the second sync wrote %s entries, want 0", n)
		}
		if got := s.body(day); got != afterFirst {
			t.Errorf("the body changed:\n%q\nwas\n%q", got, afterFirst)
		}
	})

	t.Run("lifelog do ticktick-add-task makes the task in the list", func(t *testing.T) {
		s.do("ticktick-add-task", "title=Milk", "list=Home")
		milk, ok := fake.byTitle("Milk")
		if !ok || milk.ProjectID != "p-home" {
			t.Fatalf("task in TickTick = %+v, %v, want Milk in p-home", milk, ok)
		}
	})

	t.Run("ticktick-complete-task completes it and the next sync writes its entry", func(t *testing.T) {
		s.act("ticktick-sync", nil) // the way that has no /task/completed sees a task open before it sees it done
		milk, _ := fake.byTitle("Milk")
		s.act("ticktick-complete-task", map[string]string{"id": milk.ID})
		if done, _ := fake.byTitle("Milk"); done.Status != 2 {
			t.Fatalf("Milk in TickTick has status %d, want 2", done.Status)
		}
		if n := s.written(s.act("ticktick-sync", nil)); n != "1" {
			t.Errorf("the sync wrote %s entries, want 1", n)
		}
		completed, _ := time.Parse(ticktick.TimeLayout, func() string { m, _ := fake.byTitle("Milk"); return m.CompletedTime }())
		if body := s.body(dayOf(completed)); !strings.Contains(body, "Done: Milk · Home") {
			t.Errorf("body = %q, want Done: Milk · Home", body)
		}
	})

	t.Run("the home of the app shows TickTick and the open tasks as a browser sees it", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), "GET", s.base+"/with/ticktick/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/html")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		page, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 || !strings.Contains(string(page), "TickTick") || !strings.Contains(string(page), "Write report") {
			t.Errorf("status %d; the page holds TickTick: %v, Write report: %v", res.StatusCode,
				strings.Contains(string(page), "TickTick"), strings.Contains(string(page), "Write report"))
		}
		home, err := s.client().Get(t.Context(), "/with/ticktick/")
		if err != nil {
			t.Fatal(err)
		}
		if p, _ := home.Properties.(map[string]any); p["open"] == nil || p["lists"] == nil {
			t.Errorf("the home has no open and lists: %+v", home.Properties)
		}
	})

	t.Run("a list is a page of the browser, with its tasks as links and its add form filled", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), "GET", s.base+"/with/ticktick/lists/p-home", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/html")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		page := string(raw)
		if res.StatusCode != 200 {
			t.Fatalf("status %d, want 200", res.StatusCode)
		}
		if !regexp.MustCompile(`<a [^>]*href="/with/ticktick/tasks/t-mom"[^>]*>[^<]*Call mom`).MatchString(page) {
			t.Errorf("the page has no link to /with/ticktick/tasks/t-mom with the title Call mom:\n%s", page)
		}
		form := regexp.MustCompile(`(?s)<form[^>]*action="/with/ticktick/tasks"[^>]*>.*?</form>`).FindString(page)
		if form == "" {
			t.Fatalf("the page has no form that posts to /with/ticktick/tasks:\n%s", page)
		}
		// The list has options, so the view draws a select, with the list of the page selected.
		if !regexp.MustCompile(`<select[^>]*name="list"[^>]*>.*<option selected>Home</option>`).MatchString(form) {
			t.Errorf("the form has no list with Home selected:\n%s", form)
		}
	})

	t.Run("a task that TickTick does not know is not found", func(t *testing.T) {
		actions, _ := s.client().Catalog(t.Context())
		for _, a := range actions {
			if a.Name != "ticktick-complete-task" {
				continue
			}
			_, err := s.client().Do(t.Context(), a, map[string]string{"id": "nope"})
			if ce, ok := err.(*client.Error); !ok || ce.Status != 404 || ce.Code() != "not_found" {
				t.Errorf("err = %v, want 404 not_found", err)
			}
		}
	})
}
