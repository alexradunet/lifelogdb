package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
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

// noModel is the reply of a turn when the settings of lifelog name no model.
const noModel = "No model is set: open Settings in lifelog."

// errNoModel says that the settings hold no model_url or no model.
var errNoModel = errors.New("no model is set")

// Agent answers the messages of one owner. The conversation of a day is a page of life.db.
type Agent struct {
	Chat   Chat
	Client *client.Client // sends siren.AgentPrefix + Source on every request, and so do the tools of the model
	// Settings reads GET /settings at each turn. It has the owner's token and no source: the settings hold the key
	// of the model, and the API refuses them to an agent. Only model() uses it. nil: Client.
	Settings *client.Client
	Owner    string // the number of the owner, digits only
	State    string // the path of state.json
	Now      func() time.Time
	Log      *log.Logger // nil: the standard logger
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
	for _, name := range []string{"say", "save-body"} {
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

// phoneTurn answers a message from the phone: the action say adds the owner's paragraph to today's conversation,
// then the model answers. The API stamps the paragraph and makes the page on the first turn of the day.
func (a *Agent) phoneTurn(ctx context.Context, m Message) {
	tctx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()
	at := a.Now() // one time for the turn: its stamp does not change at midnight
	a.typing(tctx, true)
	c, err := a.ownerSays(tctx, m.Text)
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

// ownerSays sends the owner's text with the action say. Its answer is the conversation after the append: the
// extension adopts it as its own save.
func (a *Agent) ownerSays(ctx context.Context, text string) (*convo, error) {
	e, err := a.Client.Do(ctx, a.actions["say"], map[string]string{"day": "today", "text": text})
	if err != nil {
		return nil, err
	}
	c, err := convoFrom(e, "")
	if err != nil {
		return nil, err
	}
	return c, a.adopt(c)
}

// model reads the settings of lifelog and makes the client of the model. It is called at each turn, so a change in
// Settings is seen at once. The read is the owner's: the client Settings carries no source. The tools of the model
// use Client, so their read of /settings is an agent's, and the API refuses it. The key is never in an error.
func (a *Agent) model(ctx context.Context) (*model.Client, error) {
	c := a.Settings
	if c == nil {
		c = a.Client
	}
	e, err := c.Get(ctx, "/settings")
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	p, _ := e.Properties.(map[string]any)
	url, _ := p["model_url"].(string)
	name, _ := p["model"].(string)
	key, _ := p["model_key"].(string)
	if url == "" || name == "" {
		return nil, errNoModel
	}
	return model.New(url, name, key), nil
}

// OwnerEvery is the time between two reads of the settings while no owner number is set.
var OwnerEvery = 30 * time.Second

// WaitOwner reads the owner's number from GET /settings (the property owner_number) with c, which has the owner's
// token and no source. With none set, it logs one line, then reads again at each tick (nil: every OwnerEvery) until
// a number is set. It returns the error of ctx when ctx ends. A number that is set is used for the whole run.
func WaitOwner(ctx context.Context, c *client.Client, logger *log.Logger, tick <-chan time.Time) (string, error) {
	if tick == nil {
		t := time.NewTicker(OwnerEvery)
		defer t.Stop()
		tick = t.C
	}
	last := "" // the line said last: each state is said once, and again when it changes
	for {
		e, err := c.Get(ctx, "/settings")
		line := ""
		switch {
		case ctx.Err() != nil:
			return "", ctx.Err()
		case err != nil:
			line = fmt.Sprintf("settings: %v", err)
		default:
			p, _ := e.Properties.(map[string]any)
			if n, _ := p["owner_number"].(string); n != "" {
				return n, nil
			}
			line = "no owner number: set it in Settings (owner_number)"
		}
		if line != last {
			logger.Print(line)
			last = line
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-tick:
		}
	}
}

// reply makes the reply of a turn on the body of the page. The error is the error of ctx.
func (a *Agent) reply(ctx context.Context, body string, at time.Time) (string, []string, error) {
	m, err := a.model(ctx)
	switch {
	case ctx.Err() != nil:
		return "", nil, ctx.Err()
	case errors.Is(err, errNoModel):
		a.logger.Printf("%v: open Settings in lifelog", err)
		return noModel, nil, nil
	case err != nil:
		a.logger.Printf("%v", err)
		return "I could not read the settings: " + firstLine(err.Error()) + ".", nil, nil
	}
	return a.answer(ctx, m, body, at)
}

// finish runs the model on the page c, appends the tool lines and the reply, and sends the reply. at is the time
// of the turn.
func (a *Agent) finish(ctx, tctx context.Context, c *convo, at time.Time) {
	reply, lines, err := a.reply(tctx, c.Body, at)
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
// turn. A page that is new to the extension is answered when its last turn is the owner's. Any other change is
// adopted and answered with nothing.
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
	day := at.Format(time.DateOnly)
	if a.conv != nil && a.conv.Day != day {
		// The day changed. A turn said before midnight on another surface may be unanswered: answer it on its own page.
		if err := a.closeDay(ctx, at); err != nil {
			return err
		}
	}
	c, found, err := a.read(ctx, day)
	if err != nil {
		return err
	}
	if a.conv == nil || a.conv.Day != day {
		if !found { // a day with no talk has no page yet
			return nil
		}
		a.logger.Printf("adopted the conversation of %s", day)
		if err := a.adopt(c); err != nil {
			return err
		}
		if c.pending { // the first turn of the day was said on another surface
			a.answerPending(ctx, c, at)
		}
		return nil
	}
	if !found {
		return errors.New("the conversation page of " + day + " is gone")
	}
	if c.Version == a.conv.Version {
		return nil
	}
	old := a.conv.Body
	if suffix := strings.TrimSpace(strings.TrimPrefix(c.Body, old)); strings.HasPrefix(c.Body, old) && suffix != "" {
		a.logger.Printf("the page grew by %d bytes elsewhere: a turn", len(suffix))
		if err := a.adopt(c); err != nil {
			return err
		}
		a.answerPending(ctx, c, at)
		return nil
	}
	a.logger.Printf("the page was edited elsewhere: adopted, no answer")
	return a.adopt(c)
}

// closeDay reads the page of the day that ended once more and answers its last turn when that is the owner's. Then
// the extension holds no page, so the next look is on the new day.
func (a *Agent) closeDay(ctx context.Context, at time.Time) error {
	c, found, err := a.read(ctx, a.conv.Day)
	if err != nil {
		return err
	}
	if found && c.pending {
		a.logger.Printf("answering a turn that waited on the page of %s", c.Day)
		if err := a.adopt(c); err != nil {
			return err
		}
		a.answerPending(ctx, c, at)
	}
	a.conv = nil
	return nil
}

// answerPending answers the last turn of c, which the extension has adopted. at is the time of the turn.
func (a *Agent) answerPending(ctx context.Context, c *convo, at time.Time) {
	tctx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()
	a.typing(tctx, true)
	a.finish(ctx, tctx, c, at)
}

// adopt makes c the page that the extension knows, and keeps it.
func (a *Agent) adopt(c *convo) error {
	c.SavedAt = a.Now()
	a.conv = c
	return saveState(a.State, c)
}

// read is the conversation of a day, GET /days/{day}/conversation. found is false when the day has no page yet.
func (a *Agent) read(ctx context.Context, day string) (c *convo, found bool, err error) {
	e, err := a.Client.Get(ctx, "/days/"+day+"/conversation")
	if err != nil {
		return nil, false, err
	}
	if p, _ := e.Properties.(map[string]any); p["id"] == nil {
		return nil, false, nil
	}
	c, err = convoFrom(e, day)
	return c, err == nil, err
}

// convoFrom reads the id, the version, the body and whether the last turn is the owner's, of a conversation entity or of a page entity. The day is the
// property day of a conversation, or fallback. The body's line endings are made LF: a browser submits a textarea
// with CRLF, and the API stores a body as it was sent, so the saved body and the read body would not be the same
// text. The next save of the extension writes LF.
func convoFrom(e *siren.Entity, fallback string) (*convo, error) {
	p, _ := e.Properties.(map[string]any)
	id, version, body := fmt.Sprint(p["id"]), fmt.Sprint(p["version"]), p["body"]
	text, ok := body.(string)
	if p["id"] == nil || p["version"] == nil || !ok {
		return nil, errors.New("the answer is not a page with an id, a version and a body")
	}
	day, _ := p["day"].(string)
	if day == "" {
		day = fallback
	}
	// The last turn is the owner's: nobody has answered it. A page entity has no turns, so it is never pending.
	turns, _ := p["turns"].([]any)
	pending := false
	if len(turns) > 0 {
		last, _ := turns[len(turns)-1].(map[string]any)
		pending = last["who"] == "owner"
	}
	return &convo{Day: day, ID: id, Version: version, Body: strings.ReplaceAll(text, "\r\n", "\n"), pending: pending}, nil
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
			saved, perr := convoFrom(e, c.Day)
			if perr != nil {
				return retried, perr
			}
			*c = *saved
			return retried, a.adopt(c)
		case errors.As(err, &ce) && ce.Code() == "stale_version":
			fresh, found, gerr := a.read(ctx, c.Day)
			if gerr != nil || !found {
				return retried, errors.Join(gerr, errors.New("the conversation page cannot be read again"))
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
func (a *Agent) answer(ctx context.Context, m *model.Client, body string, at time.Time) (string, []string, error) {
	msgs := []model.Message{model.Text("system", systemText(at)), model.Text("user", tail(body))}
	fns := make([]model.Tool, len(a.tools))
	for i, t := range a.tools {
		fns[i] = model.Tool{Name: t.name, Description: t.description, Parameters: t.schema}
	}
	last := ""
	var lines []string
	for step := 1; step <= maxSteps; step++ {
		r, err := m.Chat(ctx, msgs, fns)
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
