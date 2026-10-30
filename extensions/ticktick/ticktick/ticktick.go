// Package ticktick is a small client of the Open API of TickTick, over net/http, and the OAuth2 login that gives it
// a token. It keeps no state and reads no file. The API is as its reference describes it; no test reaches the live
// service, a fake stands for it.
package ticktick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// callTimeout is the longest a call to TickTick may take.
const callTimeout = 10 * time.Second

// maxAnswer is the largest answer that this client reads.
const maxAnswer = 8 << 20

// TimeLayout is the form of an instant in the API: 2006-01-02T15:04:05.000-0700.
const TimeLayout = "2006-01-02T15:04:05.000-0700"

// ErrNoCompletedEndpoint says that the server does not know POST /task/completed (404 or 405). An older server
// answers so, and the caller finds the done tasks in another way.
var ErrNoCompletedEndpoint = errors.New("ticktick: the server has no /task/completed")

// StatusError is an answer with a status that is not 2xx. It keeps no body: the body may hold the owner's words.
type StatusError struct{ Status int }

func (e *StatusError) Error() string { return fmt.Sprintf("TickTick answered %d", e.Status) }

// Project is a list of tasks. The Inbox is not in the list that TickTick gives.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Task is a task. Status is 0 for an open task and 2 for a done one. The instants are strings in TimeLayout.
type Task struct {
	ID            string `json:"id,omitempty"`
	ProjectID     string `json:"projectId,omitempty"`
	Title         string `json:"title"`
	Content       string `json:"content,omitempty"`
	Status        int    `json:"status,omitempty"`
	CompletedTime string `json:"completedTime,omitempty"`
	DueDate       string `json:"dueDate,omitempty"`
	StartDate     string `json:"startDate,omitempty"`
	TimeZone      string `json:"timeZone,omitempty"`
	IsAllDay      bool   `json:"isAllDay,omitempty"`
	Priority      int    `json:"priority,omitempty"` // 0 none, 1 low, 3 medium, 5 high
}

// PriorityWord is the priority as a word: high, medium or low. It is "" for none.
func (t Task) PriorityWord() string {
	switch {
	case t.Priority >= 5:
		return "high"
	case t.Priority >= 3:
		return "medium"
	case t.Priority >= 1:
		return "low"
	}
	return ""
}

// Done is the instant of completion, in the zone of the task (else in the local zone). It is false when the task has
// no completedTime or the time does not parse.
func (t Task) Done() (time.Time, bool) { return t.inZone(t.CompletedTime) }

// Due is the due date, in the zone of the task (else in the local zone). It is false when there is none.
func (t Task) Due() (time.Time, bool) { return t.inZone(t.DueDate) }

func (t Task) inZone(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	at, err := time.Parse(TimeLayout, s)
	if err != nil {
		at, err = time.Parse("2006-01-02T15:04:05-0700", s) // an instant with no milliseconds
		if err != nil {
			return time.Time{}, false
		}
	}
	zone := time.Local
	if t.TimeZone != "" {
		if l, err := time.LoadLocation(t.TimeZone); err == nil {
			zone = l
		}
	}
	return at.In(zone), true
}

// Client calls the API. HTTP may be nil.
type Client struct {
	Base  string // https://api.ticktick.com/open/v1
	Token string
	HTTP  *http.Client
}

// call sends one request. A body is sent as JSON, an answer is decoded as JSON into out (nil: ignored).
func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err) // the token is in a header, so the error holds none
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &StatusError{Status: res.StatusCode}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxAnswer))
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer+1))
	if err != nil {
		return err
	}
	if len(data) > maxAnswer {
		return fmt.Errorf("%s %s: the answer is larger than %d bytes", method, path, maxAnswer)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: the answer is not the JSON expected: %w", method, path, err) // never the body
	}
	return nil
}

// Projects reads the lists.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var out []Project
	err := c.call(ctx, http.MethodGet, "/project", nil, &out)
	return out, err
}

// ProjectData reads the open tasks of a list.
func (c *Client) ProjectData(ctx context.Context, id string) ([]Task, error) {
	var out struct {
		Tasks []Task `json:"tasks"`
	}
	err := c.call(ctx, http.MethodGet, "/project/"+url.PathEscape(id)+"/data", nil, &out)
	return out.Tasks, err
}

// Task reads one task.
func (c *Client) Task(ctx context.Context, project, id string) (Task, error) {
	var out Task
	err := c.call(ctx, http.MethodGet, "/project/"+url.PathEscape(project)+"/task/"+url.PathEscape(id), nil, &out)
	return out, err
}

// Create makes a task. An empty ProjectID puts it in the Inbox. It answers the task with its id.
func (c *Client) Create(ctx context.Context, t Task) (Task, error) {
	var out Task
	err := c.call(ctx, http.MethodPost, "/task", t, &out)
	return out, err
}

// Complete marks a task done.
func (c *Client) Complete(ctx context.Context, project, id string) error {
	return c.call(ctx, http.MethodPost, "/project/"+url.PathEscape(project)+"/task/"+url.PathEscape(id)+"/complete", nil, nil)
}

// Completed reads the done tasks that the server lists. It sends no filter: the live server (probed 2026-10-29)
// answers an empty list to every startDate and endDate it was sent, in every form, and the whole list to an empty
// body, so the caller keeps the tasks it wants by their completedTime. It is ErrNoCompletedEndpoint when the server
// does not have the call.
func (c *Client) Completed(ctx context.Context) ([]Task, error) {
	var out []Task
	err := c.call(ctx, http.MethodPost, "/task/completed", struct{}{}, &out)
	var se *StatusError
	if errors.As(err, &se) && (se.Status == http.StatusNotFound || se.Status == http.StatusMethodNotAllowed) {
		return nil, ErrNoCompletedEndpoint
	}
	return out, err
}

// OAuth is the authorization code flow of TickTick. HTTP may be nil.
type OAuth struct {
	ClientID, ClientSecret string
	AuthorizeURL, TokenURL string
	RedirectURI            string
	HTTP                   *http.Client
}

const scope = "tasks:read tasks:write"

// AuthorizeLink is the address where the owner agrees. state comes back to the redirect unchanged.
func (o OAuth) AuthorizeLink(state string) string {
	q := url.Values{
		"client_id": {o.ClientID}, "scope": {scope}, "state": {state},
		"redirect_uri": {o.RedirectURI}, "response_type": {"code"},
	}
	return o.AuthorizeURL + "?" + q.Encode()
}

// Exchange trades the code of the redirect for the token. The client id and secret go as HTTP Basic.
func (o OAuth) Exchange(ctx context.Context, code string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	form := url.Values{"code": {code}, "grant_type": {"authorization_code"}, "scope": {scope}, "redirect_uri": {o.RedirectURI}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(o.ClientID, o.ClientSecret)
	hc := o.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return "", &StatusError{Status: res.StatusCode}
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxAnswer)).Decode(&out); err != nil || out.AccessToken == "" {
		return "", errors.New("the token answer holds no access_token")
	}
	return out.AccessToken, nil
}
