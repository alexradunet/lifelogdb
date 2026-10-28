package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"lifelog-whatsapp/client"
	"lifelog-whatsapp/model"
	"lifelog-whatsapp/siren"
)

const (
	// queueSize is the number of messages that wait for the turn in progress.
	queueSize = 32
	// maxSteps is the number of model steps in one turn.
	maxSteps = 16
	// turnTimeout is the longest time of one turn. One model step has its own limit (package model).
	turnTimeout = 5 * time.Minute
	// maxResult is the largest tool result that goes to the model.
	maxResult = 64 << 10
	// maxBody is the largest part of the conversation that goes to the model. The end is kept.
	maxBody = 200 << 10
	// pollEvery is the time between two looks at today's page.
	pollEvery = 3 * time.Second
)

// Source is the name of the writer: the rows that the agent writes carry agent:whatsapp.
const Source = "whatsapp"

// Category is the category page that the conversation pages are filed in.
const Category = "Conversations"

// Agent answers the messages of one owner. The conversation of a day is a page of life.db.
type Agent struct {
	Chat   Chat
	Client *client.Client // writes as siren.AgentPrefix + Source
	Model  *model.Client
	Owner  string // the number of the owner, digits only
	State  string // the path of state.json
	Now    func() time.Time
	Log    *log.Logger // nil: the standard logger
	// Tick gives the moments when the worker looks at today's page. nil: every three seconds.
	Tick <-chan time.Time

	logger   *log.Logger
	tools    []tool
	actions  map[string]siren.Action
	conv     *convo // only the worker uses it
	watchErr string
}

// Run answers messages until the chat closes its channel (it returns nil) or ctx ends (it returns the error of ctx).
// It reads the catalog of the API first. One worker runs the turns and the looks at the page. It belongs to Run:
// Run waits for it.
func (a *Agent) Run(ctx context.Context) error {
	a.logger = a.Log
	if a.logger == nil {
		a.logger = log.Default()
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	list, err := a.Client.Catalog(ctx)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if a.tools, err = tools(list); err != nil {
		return err
	}
	a.actions = map[string]siren.Action{}
	for _, x := range list {
		a.actions[x.Name] = x
	}
	for _, name := range []string{"create-page", "save-body"} {
		if _, ok := a.actions[name]; !ok {
			return fmt.Errorf("the catalog has no action %q", name)
		}
	}
	if a.conv, err = loadState(a.State); err != nil {
		return err
	}
	tick := a.Tick
	if tick == nil {
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		tick = t.C
	}
	queue := make(chan Message, queueSize)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-queue:
				if !ok {
					return
				}
				a.phoneTurn(ctx, m)
			case <-tick:
				a.watch(ctx)
			}
		}
	}()
	messages := a.Chat.Messages()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case m, ok := <-messages:
			if !ok {
				break loop
			}
			a.accept(ctx, m, queue)
		}
	}
	close(queue) // the worker answers what is queued, unless ctx ended
	wg.Wait()
	return ctx.Err()
}

// accept applies the rules of who may talk: another sender is ignored and logged, a message of the owner that is
// not text gets one line, and a text enters the queue.
func (a *Agent) accept(ctx context.Context, m Message, queue chan<- Message) {
	switch {
	case m.Sender != a.Owner:
		a.logger.Printf("ignored a message of kind %s from %s", m.Kind, m.Sender)
	case m.Kind != KindText:
		a.say(ctx, "I read text for now; send it as text.")
	default:
		select {
		case queue <- m:
		default:
			a.say(ctx, "still working on your earlier messages")
		}
	}
}

func (a *Agent) say(ctx context.Context, text string) {
	if err := a.Chat.Send(ctx, text); err != nil && ctx.Err() == nil {
		a.logger.Printf("send failed: %v", err)
	}
}

func (a *Agent) typing(ctx context.Context, on bool) {
	if err := a.Chat.Typing(ctx, on); err != nil && ctx.Err() == nil {
		a.logger.Printf("typing failed: %v", err)
	}
}

// stamp is the time of a paragraph: HH.MM.
func (a *Agent) stamp(t time.Time) string { return t.In(a.Now().Location()).Format("15.04") }

// phoneTurn answers a message from the phone: it adds the owner's paragraph to today's page, then answers.
func (a *Agent) phoneTurn(ctx context.Context, m Message) {
	tctx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()
	at := a.Now() // one time for the turn: its page, its day and its stamp do not change at midnight
	a.typing(tctx, true)
	c, err := a.today(tctx, at)
	if err == nil {
		_, err = a.save(tctx, c, fmt.Sprintf("\n\n**Owner** (%s): %s", a.stamp(m.At), m.Text))
	}
	if ctx.Err() != nil {
		return
	}
	if err != nil { // without the page the turn has no memory: say so and stop
		a.logger.Printf("conversation page: %v", err)
		a.typing(ctx, false)
		a.say(ctx, "I could not use today's conversation page: "+firstLine(err.Error())+".")
		return
	}
	a.finish(ctx, tctx, c, at)
}

// finish runs the model on the page c, appends the tool lines and the reply, and sends the reply. at is the time
// of the turn.
func (a *Agent) finish(ctx, tctx context.Context, c *convo, at time.Time) {
	reply, lines, err := a.answer(tctx, c.Body, at)
	if ctx.Err() != nil {
		return
	}
	if err != nil { // only the timeout of the turn reaches here
		reply = "I stopped: the turn took more than 5 minutes."
	}
	extra := "\n\n"
	if len(lines) > 0 {
		extra += strings.Join(lines, "\n") + "\n\n"
	}
	extra += fmt.Sprintf("**lifelog** (%s): %s", a.stamp(at), reply)
	// The save has its own limit: the timeout of the turn may be spent, and the reply must reach the page.
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	retried, err := a.save(sctx, c, extra)
	if err != nil && ctx.Err() == nil {
		a.logger.Printf("conversation page: %v", err)
	}
	if ctx.Err() != nil {
		return
	}
	a.logger.Printf("turn answered: %d bytes out, %d tool calls", len(reply), len(lines))
	a.typing(ctx, false)
	a.say(ctx, reply)
	if retried { // the owner wrote on another surface while the model ran: that text is on the page, unanswered
		a.say(ctx, "You wrote on another surface while I answered. Say it again if it needs an answer.")
	}
}

// watch looks at today's page. A body that grew at the end was continued on another surface: it is an owner's
// turn. Any other change is adopted and answered with nothing.
func (a *Agent) watch(ctx context.Context) {
	err := a.look(ctx)
	if ctx.Err() != nil {
		return
	}
	switch msg := fmt.Sprint(err); {
	case err == nil:
		a.watchErr = ""
	case msg != a.watchErr: // one line for each kind of failure, not one every three seconds
		a.watchErr = msg
		a.logger.Printf("watch: %v", err)
	}
}

func (a *Agent) look(ctx context.Context) error {
	at := a.Now()
	title := a.title(at)
	if a.conv == nil || a.conv.Title != title {
		c, found, err := a.lookup(ctx, title)
		if err != nil || !found {
			return err // a day with no talk has no page yet
		}
		a.logger.Printf("adopted the page %s", title)
		return a.adopt(c)
	}
	e, err := a.Client.Get(ctx, "/pages/"+url.PathEscape(a.conv.ID))
	if err != nil {
		return err
	}
	c, err := pageFrom(e, title)
	if err != nil {
		return err
	}
	if c.Version == a.conv.Version {
		return nil
	}
	old := a.conv.Body
	if suffix := strings.TrimSpace(strings.TrimPrefix(c.Body, old)); strings.HasPrefix(c.Body, old) && suffix != "" {
		a.logger.Printf("the page grew by %d bytes elsewhere: a turn", len(suffix))
		tctx, cancel := context.WithTimeout(ctx, turnTimeout)
		defer cancel()
		a.typing(tctx, true)
		if err := a.adopt(c); err != nil {
			return err
		}
		a.finish(ctx, tctx, c, at)
		return nil
	}
	a.logger.Printf("the page was edited elsewhere: adopted, no answer")
	return a.adopt(c)
}

// adopt makes c the page that the extension knows, and keeps it.
func (a *Agent) adopt(c *convo) error {
	c.SavedAt = a.Now()
	a.conv = c
	return saveState(a.State, c)
}

// title is the title of the conversation page of the day of at.
func (a *Agent) title(at time.Time) string { return "Conversation " + at.Format(time.DateOnly) }

// today is the page of the day of at, read now. It is made when the day has none.
func (a *Agent) today(ctx context.Context, at time.Time) (*convo, error) {
	title := a.title(at)
	if a.conv != nil && a.conv.Title == title {
		e, err := a.Client.Get(ctx, "/pages/"+url.PathEscape(a.conv.ID))
		if err != nil {
			return nil, err
		}
		c, err := pageFrom(e, title)
		if err != nil {
			return nil, err
		}
		a.conv = c
		return c, nil
	}
	c, found, err := a.lookup(ctx, title)
	if err != nil {
		return nil, err
	}
	if !found {
		day := at.Format(time.DateOnly)
		e, err := a.Client.Do(ctx, a.actions["create-page"], map[string]string{"title": title, "body": "[[" + day + "]]", "category": Category})
		if err != nil {
			return nil, err
		}
		if c, err = pageFrom(e, title); err != nil {
			return nil, err
		}
	}
	a.conv = c
	return c, nil
}

// lookup finds the page with that title. The API redirects the title to the page, and the client follows.
func (a *Agent) lookup(ctx context.Context, title string) (*convo, bool, error) {
	e, err := a.Client.Get(ctx, "/pages?title="+url.QueryEscape(title))
	var ce *client.Error
	if errors.As(err, &ce) && ce.Status == 404 {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	c, err := pageFrom(e, title)
	return c, err == nil, err
}

// pageFrom reads the id, the version and the body of a page entity. The body's line endings are made LF: a browser
// submits a textarea with CRLF, and the API stores a body as it was sent, so the saved body and the read body
// would not be the same text. The next save of the extension writes LF.
func pageFrom(e *siren.Entity, title string) (*convo, error) {
	p, _ := e.Properties.(map[string]any)
	id, version, body := fmt.Sprint(p["id"]), fmt.Sprint(p["version"]), p["body"]
	text, ok := body.(string)
	if p["id"] == nil || p["version"] == nil || !ok {
		return nil, errors.New("the answer is not a page with an id, a version and a body")
	}
	return &convo{Title: title, ID: id, Version: version, Body: strings.ReplaceAll(text, "\r\n", "\n")}, nil
}

// save appends extra to the body of c and saves it with the version of c. A stale version reads the page again
// and appends once more, and retried says so: what the owner saved meanwhile is on the page before the appended
// text, and the caller says it was not answered. A second stale version is an error. On success c holds the saved
// page and the state file holds it too.
func (a *Agent) save(ctx context.Context, c *convo, extra string) (retried bool, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		body := strings.TrimRight(c.Body, "\n") + extra
		e, err := a.Client.Do(ctx, a.actions["save-body"], map[string]string{"id": c.ID, "body": body, "version": c.Version})
		var ce *client.Error
		switch {
		case err == nil:
			saved, perr := pageFrom(e, c.Title)
			if perr != nil {
				return retried, perr
			}
			*c = *saved
			return retried, a.adopt(c)
		case errors.As(err, &ce) && ce.Code() == "stale_version":
			e, gerr := a.Client.Get(ctx, "/pages/"+url.PathEscape(c.ID))
			if gerr != nil {
				return retried, gerr
			}
			fresh, perr := pageFrom(e, c.Title)
			if perr != nil {
				return retried, perr
			}
			*c = *fresh
			retried = true
		default:
			return retried, err
		}
	}
	return retried, errors.New("the page changed twice during the save")
}

// answer runs the steps of one turn on the body of the page, at the time at. It returns the reply and one line for
// each tool call. Its error is the error of ctx.
func (a *Agent) answer(ctx context.Context, body string, at time.Time) (string, []string, error) {
	msgs := []model.Message{model.Text("system", systemText(at)), model.Text("user", tail(body))}
	fns := make([]model.Tool, len(a.tools))
	for i, t := range a.tools {
		fns[i] = model.Tool{Name: t.name, Description: t.description, Parameters: t.schema}
	}
	last := ""
	var lines []string
	for step := 1; step <= maxSteps; step++ {
		r, err := a.Model.Chat(ctx, msgs, fns)
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		if err != nil {
			a.logger.Printf("step %d: %v", step, err)
			return "The model failed: " + firstLine(err.Error()) + ".", lines, nil
		}
		calls := r.Message.ToolCalls
		a.logger.Printf("step %d: %d tool calls, %d bytes of text", step, len(calls), len(r.Text()))
		if text := strings.TrimSpace(r.Text()); text != "" {
			last = text
		}
		if len(calls) == 0 {
			if last == "" {
				return "The model sent no text.", lines, nil
			}
			return last, lines, nil
		}
		msgs = append(msgs, r.Message)
		for _, c := range calls {
			result, line := a.call(ctx, c.Function.Name, string(c.Function.Arguments))
			if ctx.Err() != nil {
				return "", nil, ctx.Err()
			}
			lines = append(lines, line)
			msgs = append(msgs, model.Message{Role: "tool", ToolCallID: c.ID, Content: &result})
		}
	}
	if last != "" {
		return last, lines, nil
	}
	return fmt.Sprintf("I stopped after %d steps.", maxSteps), lines, nil
}

// call runs one tool call. It returns the text for the model (the Siren entity as JSON, cut at maxResult, or the
// sentence of an error; an error of the API is its error entity, which has the status) and the line for the page.
func (a *Agent) call(ctx context.Context, name, rawArgs string) (result, line string) {
	var t *tool
	for i := range a.tools {
		if a.tools[i].name == name {
			t = &a.tools[i]
		}
	}
	if t == nil {
		return fmt.Sprintf("error: there is no tool %q", name), fmt.Sprintf("- `%s` failed: unknown tool", name)
	}
	args, err := arguments([]byte(rawArgs))
	if err != nil {
		return "error: " + err.Error(), fmt.Sprintf("- `%s` failed: bad arguments", name)
	}
	e, err := run(ctx, a.Client, *t, args)
	if err != nil {
		code := "error"
		var ce *client.Error
		if errors.As(err, &ce) && ce.Code() != "" {
			code = ce.Code()
		}
		line = fmt.Sprintf("- `%s` failed: %s", name, code)
	} else {
		line = fmt.Sprintf("- `%s`", name)
		if self := e.LinkByRel("self"); self != nil {
			line += fmt.Sprintf(" → [%s](%s)", self.Href, self.Href)
		}
	}
	if e == nil {
		return "error: " + err.Error(), line // no entity: the sentence of the error stands alone
	}
	var b bytes.Buffer
	if werr := siren.WriteJSON(&b, e, ""); werr != nil {
		return "error: " + werr.Error(), line
	}
	return cut(strings.TrimSuffix(b.String(), "\n")), line
}

// cut shortens s to maxResult bytes at a character boundary and says so.
func cut(s string) string {
	if len(s) <= maxResult {
		return s
	}
	n := maxResult
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return fmt.Sprintf("%s\n[cut: the result has %d bytes; the first %d are shown]", s[:n], len(s), n)
}

// tail keeps the last maxBody bytes of the conversation, from a character boundary, and says what it cut.
func tail(s string) string {
	if len(s) <= maxBody {
		return s
	}
	n := len(s) - maxBody
	for n < len(s) && !utf8.RuneStart(s[n]) {
		n++
	}
	return fmt.Sprintf("[the first %d bytes of this conversation are cut]\n%s", n, s[n:])
}

// firstLine is the first line of s.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
