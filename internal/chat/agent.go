package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
)

// Source is the writer of every row the agent writes, the conversation pages and the summaries included.
const Source = "agent:chat"

const (
	maxSteps        = 16       // model answers in one turn: a model that never stops calling tools is stopped
	maxToolResult   = 64 << 10 // bytes of one tool result sent back to the model
	maxCompactions  = 3        // earlier days summed up at one question: a long silence is caught up a few days at a time
	maxCompactInput = 64 << 10 // bytes of a conversation sent to be summed up
	maxMemory       = 16 << 10 // bytes of the journal page given to the model as its memory
	maxChain        = 100      // previous conversations followed to find the ones not yet summed up
)

// Agent answers ask. Client calls the API in-process as Source; it is set after the handler that holds the Agent
// is made. Now is the clock of a turn's time and of the time in the prompt (nil: time.Now); the day a turn belongs
// to is the server's, read from GET /conversations.
type Agent struct {
	Model  *Model
	Client *client.Client
	Now    func() time.Time
}

// conversation is a conversation as GET /conversations or /conversations/{id} says it: its day, its page (0 when
// the day has none yet), the previous conversation, and the day of the journal page the model is given as memory.
type conversation struct {
	day, title, memory string
	id                 int64
	previous           *previous
}

type previous struct {
	id        int64
	compacted bool
}

// Ask runs one turn of the day's conversation (docs/plans/106-one-continuous-conversation.md): the earlier days'
// conversations not yet summed up are compacted into their journal pages first, then the model answers with the
// memory of the nearest earlier day, and the turn is appended to the page of the day, created at the first question.
func (a *Agent) Ask(ctx context.Context, question string) (api.Answer, error) {
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	actions, err := a.Client.Catalog(ctx)
	if err != nil {
		return api.Answer{}, err
	}
	tools, err := client.Tools(actions, func(x api.Action) bool { return !x.Owner && x.Name != "ask" }) // a model never calls a model
	if err != nil {
		return api.Answer{}, err
	}
	today, err := a.conversationAt(ctx, "/conversations")
	if err != nil {
		return api.Answer{}, err
	}
	steps, wrote, err := a.compact(ctx, actions, today)
	if err != nil {
		return api.Answer{}, err
	}
	if wrote { // the memory may be a page the compaction just made
		if today, err = a.conversationAt(ctx, "/conversations"); err != nil {
			return api.Answer{}, err
		}
	}
	var body, version string
	if today.id != 0 {
		if today.title, body, version, err = a.page(ctx, today.id); err != nil {
			return api.Answer{}, err
		}
	}
	msgs := []message{{Role: "system", Content: text(systemPrompt(today.day, now))}}
	if today.memory != "" {
		memory, err := a.dayPage(ctx, actions, today.memory)
		if err != nil {
			return api.Answer{}, err
		}
		if memory != "" {
			msgs = append(msgs, message{Role: "user", Content: text("Your memory of the days before: the journal page of " + today.memory +
				". It holds what the owner wrote that day and the summary of that day's conversation:\n\n" + cut(memory, maxMemory))})
		}
	}
	if strings.TrimSpace(body) != "" {
		msgs = append(msgs, message{Role: "user", Content: text("The conversation so far today, from the page " + today.title + ":\n\n" + body)})
	}
	msgs = append(msgs, message{Role: "user", Content: text(question)})
	runSteps, reply, runErr := a.run(ctx, msgs, tools)
	steps = append(steps, runSteps...)
	if runErr != nil && len(steps) == 0 {
		return api.Answer{}, modelError(runErr)
	}
	failure := ""
	if runErr != nil {
		failure = runErr.Error()
	}
	turn := api.FormatTurn(now, question, steps, reply, failure)
	id := today.id
	if id == 0 {
		id, err = a.newConversation(ctx, actions, today.day, turn)
	} else {
		err = a.append(ctx, actions, id, body, version, turn)
	}
	if err != nil {
		return api.Answer{}, errors.Join(modelError(runErr), err)
	}
	if runErr != nil { // rows were written: the turn says which, and the error says where it is kept
		return api.Answer{}, modelError(fmt.Errorf("%w (the turn is kept on /pages/%d)", runErr, id))
	}
	return api.Answer{Conversation: id, Reply: reply, Steps: steps}, nil
}

// run is the loop: the model answers with tool calls until it answers in words, at most maxSteps times.
func (a *Agent) run(ctx context.Context, msgs []message, tools []client.Tool) ([]api.Step, string, error) {
	specs := make([]toolSpec, len(tools))
	byName := map[string]client.Tool{}
	for i, t := range tools {
		specs[i] = toolSpec{Type: "function", Function: function{Name: t.Name, Description: t.Description, Parameters: t.Schema}}
		byName[t.Name] = t
	}
	var steps []api.Step
	for range maxSteps {
		m, err := a.Model.complete(ctx, msgs, specs)
		if err != nil {
			return steps, "", err
		}
		if len(m.ToolCalls) == 0 {
			if m.Content == nil || strings.TrimSpace(*m.Content) == "" {
				return steps, "", errors.New("the model answered with no words and no tool call")
			}
			return steps, strings.TrimSpace(*m.Content), nil
		}
		msgs = append(msgs, message{Role: "assistant", Content: m.Content, ToolCalls: m.ToolCalls})
		for _, call := range m.ToolCalls {
			step, result := a.call(ctx, byName, call)
			if err := ctx.Err(); err != nil {
				return steps, "", err
			}
			steps = append(steps, step)
			msgs = append(msgs, message{Role: "tool", ToolCallID: call.ID, Content: text(result)})
		}
	}
	return steps, "", fmt.Errorf("the model did not answer within %d steps", maxSteps)
}

// call runs one tool call. A refusal is the tool's result, so the model reads it and can correct itself.
func (a *Agent) call(ctx context.Context, byName map[string]client.Tool, call toolCall) (api.Step, string) {
	step := api.Step{Tool: call.Function.Name}
	t, ok := byName[call.Function.Name]
	if !ok {
		step.Error = "no tool " + call.Function.Name
		return step, step.Error
	}
	args, err := client.Arguments([]byte(call.Function.Arguments))
	if err != nil {
		step.Error = "the arguments are not a JSON object of strings and numbers: " + err.Error()
		return step, step.Error
	}
	e, err := a.Client.Call(ctx, t, args)
	if err != nil {
		step.Error = err.Error()
		return step, step.Error
	}
	step.Href = selfOf(e)
	raw, err := json.Marshal(e)
	if err != nil {
		step.Error = err.Error()
		return step, step.Error
	}
	if len(raw) > maxToolResult {
		return step, string(raw[:maxToolResult]) + "\n[cut at 64 KiB: follow a link or narrow the read]"
	}
	return step, string(raw)
}

// compact sums up the earlier days' conversations that are not compacted yet, the oldest first and at most
// maxCompactions of them, each into the journal page of its day. The uncompacted conversations are always the
// newest ones: a failure stops the work, as a compact step with the error, so an older page is never skipped and
// hidden behind a newer one. wrote says whether a summary was written. The error is the context's alone: a failed
// compaction is a step, and the question is still answered.
func (a *Agent) compact(ctx context.Context, actions []api.Action, today conversation) (steps []api.Step, wrote bool, err error) {
	var chain []conversation // not compacted, the newest first
	for prev := today.previous; prev != nil && !prev.compacted && len(chain) < maxChain; prev = chain[len(chain)-1].previous {
		c, err := a.conversationAt(ctx, "/conversations/"+strconv.FormatInt(prev.id, 10))
		if err != nil {
			return []api.Step{{Tool: "compact", Error: err.Error()}}, false, ctx.Err()
		}
		chain = append(chain, c)
	}
	for i := len(chain) - 1; i >= max(0, len(chain)-maxCompactions); i-- {
		// Read again: the memory of this day may be a page the compaction of the day before has just made.
		c, err := a.conversationAt(ctx, "/conversations/"+strconv.FormatInt(chain[i].id, 10))
		if err != nil {
			return append(steps, api.Step{Tool: "compact", Error: err.Error()}), wrote, ctx.Err()
		}
		if err := a.compactOne(ctx, actions, c); err != nil {
			return append(steps, api.Step{Tool: "compact", Error: err.Error()}), wrote, ctx.Err()
		}
		steps = append(steps, api.Step{Tool: "compact", Href: "/days/" + c.day})
		wrote = true
	}
	return steps, wrote, nil
}

// compactOne sums up one conversation into the journal page of its day: the model writes the summary from the
// transcript and from the memory that conversation had, capture appends it to the day page, then the trailer marks
// the conversation page compacted. The summary is written first: a trailer refused as stale leaves the summary on
// the day page and the conversation to be summed up again, which the owner sees twice, never a summary lost.
func (a *Agent) compactOne(ctx context.Context, actions []api.Action, c conversation) error {
	title, body, version, err := a.page(ctx, c.id)
	if err != nil {
		return err
	}
	msgs := []message{{Role: "system", Content: text(compactPrompt)}}
	if c.memory != "" {
		memory, err := a.dayPage(ctx, actions, c.memory)
		if err != nil {
			return err
		}
		if memory != "" {
			msgs = append(msgs, message{Role: "user", Content: text("The journal page of " + c.memory + ", the memory lifelog had on " + c.day + ":\n\n" + cut(memory, maxMemory))})
		}
	}
	msgs = append(msgs, message{Role: "user", Content: text("The conversation of " + c.day + ", from the page " + title + ":\n\n" + cut(body, maxCompactInput))})
	m, err := a.Model.complete(ctx, msgs, nil)
	if err != nil {
		return fmt.Errorf("the summary of %s: %w", c.day, err)
	}
	if len(m.ToolCalls) > 0 || m.Content == nil || strings.TrimSpace(*m.Content) == "" {
		return fmt.Errorf("the summary of %s: the model gave no summary", c.day)
	}
	if _, err := a.do(ctx, actions, "capture", map[string]string{"day": c.day,
		"text": "**Conversation with lifelog** ([[" + title + "]]): " + strings.TrimSpace(*m.Content)}); err != nil {
		return fmt.Errorf("the summary of %s: %w", c.day, err)
	}
	if _, err := a.do(ctx, actions, "save-body", map[string]string{"id": strconv.FormatInt(c.id, 10),
		"body": strings.TrimRight(body, "\n") + "\n\n" + api.FormatCompacted(c.day), "version": version}); err != nil {
		return fmt.Errorf("the trailer of %s: %w", title, err)
	}
	return nil
}

// conversationAt reads a conversation resource.
func (a *Agent) conversationAt(ctx context.Context, href string) (conversation, error) {
	e, err := a.Client.Get(ctx, href)
	if err != nil {
		return conversation{}, err
	}
	p, _ := e.Properties.(map[string]any)
	c := conversation{id: number(p["id"])}
	c.day, _ = p["day"].(string)
	c.title, _ = p["title"].(string)
	if prev, ok := p["previous"].(map[string]any); ok {
		compacted, _ := prev["compacted"].(bool)
		c.previous = &previous{id: number(prev["id"]), compacted: compacted}
	}
	for _, l := range e.Links {
		for _, r := range l.Rel {
			if r == "memory" {
				c.memory = l.Title
			}
		}
	}
	return c, nil
}

// number is an integer property as the client decodes it: a json.Number; 0 when absent.
func number(v any) int64 {
	n, _ := v.(json.Number)
	i, _ := n.Int64()
	return i
}

// page is the title, body and version of a page.
func (a *Agent) page(ctx context.Context, id int64) (title, body, version string, err error) {
	e, err := a.Client.Get(ctx, "/pages/"+strconv.FormatInt(id, 10))
	if err != nil {
		return "", "", "", err
	}
	p, _ := e.Properties.(map[string]any)
	title, _ = p["title"].(string)
	body, _ = p["body"].(string)
	version, _ = p["version"].(string)
	return title, body, version, nil
}

// dayPage is the body of the journal page of a day; "" when the day has none.
func (a *Agent) dayPage(ctx context.Context, actions []api.Action, day string) (string, error) {
	e, err := a.do(ctx, actions, "find", map[string]string{"title": day})
	if err != nil {
		var ce *client.Error
		if errors.As(err, &ce) && ce.Status == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	p, _ := e.Properties.(map[string]any)
	body, _ := p["body"].(string)
	return body, nil
}

// newConversation creates the page of the day's conversation, holding its first turn, and files it in Conversations
// (created by the first conversation). A page that appeared since the day was read (a second question at once) gets
// the turn appended instead.
func (a *Agent) newConversation(ctx context.Context, actions []api.Action, day, turn string) (int64, error) {
	if _, err := a.do(ctx, actions, "find", map[string]string{"title": api.ConversationsTitle}); err != nil {
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != http.StatusNotFound {
			return 0, err
		}
		if _, err := a.do(ctx, actions, "create-page", map[string]string{"title": api.ConversationsTitle,
			"body": "The conversations with lifelog: each page is one day's, filed here by part-of."}); err != nil {
			return 0, err
		}
	}
	e, err := a.do(ctx, actions, "create-page", map[string]string{"title": api.ConversationTitle(day), "body": turn})
	if err != nil {
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != http.StatusConflict {
			return 0, err
		}
		today, err2 := a.conversationAt(ctx, "/conversations")
		if err2 != nil || today.id == 0 {
			return 0, errors.Join(err, err2)
		}
		_, body, version, err2 := a.page(ctx, today.id)
		if err2 != nil {
			return 0, err2
		}
		return today.id, a.append(ctx, actions, today.id, body, version, turn)
	}
	p, _ := e.Properties.(map[string]any)
	id := number(p["id"])
	if id == 0 {
		return 0, errors.New("create-page answered with no id")
	}
	_, err = a.do(ctx, actions, "link", map[string]string{"id": strconv.FormatInt(id, 10), "to": api.ConversationsTitle, "kind": "part-of"})
	return id, err
}

// append adds a turn to the end of a conversation page.
func (a *Agent) append(ctx context.Context, actions []api.Action, id int64, body, version, turn string) error {
	if body = strings.TrimRight(body, "\n"); body != "" {
		turn = body + "\n\n" + turn
	}
	_, err := a.do(ctx, actions, "save-body", map[string]string{"id": strconv.FormatInt(id, 10), "body": turn, "version": version})
	return err
}

func (a *Agent) do(ctx context.Context, actions []api.Action, name string, values map[string]string) (*api.Entity, error) {
	for _, x := range actions {
		if x.Name == name {
			return a.Client.Do(ctx, x, values)
		}
	}
	return nil, fmt.Errorf("the catalog has no action %s", name)
}

// cut bounds a text sent to the model, and says so.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n[cut at %d KiB]", n>>10)
}

func systemPrompt(day string, now time.Time) string {
	weekday := ""
	if t, err := time.Parse(time.DateOnly, day); err == nil {
		weekday = " (" + t.Weekday().String() + ")"
	}
	return client.Instructions + "\n\n" +
		"You talk with the owner of this life log, inside lifelog itself. Today is " + day + weekday +
		", and the local time is " + now.Format("15:04") + ". Answer in the owner's language, briefly. " +
		"Your memory of the days before is the journal page given to you; after it comes the conversation of today. " +
		"Write with the tools when the owner asks for it. When you are not sure what the owner wants written, ask first and write nothing. " +
		"The owner's own actions (register-metric, relocate-reading, snapshot) are not your tools: when one is needed, say so, and the owner does it. " +
		"Write [[Title]] in your reply only for a page that you read in a tool result: a [[Title]] links the conversation to that page."
}

const compactPrompt = "You sum up one day's conversation between the owner of a life log and lifelog. " +
	"Write in the owner's language, as plain CommonMark with no heading and no greeting, at most 150 words: " +
	"what the owner asked, what was found, what was written (keep every link as it is: [[Title]] and /paths), and what stays open for the next day. " +
	"Write [[Title]] only for a title that is in the conversation. " +
	"The summary goes into the owner's journal page of that day, and the next day's lifelog reads it as its memory."

// modelError is a failure of the model as the API answers it: 502, except a context that ran out or was cancelled,
// which the API answers as it answers any other.
func modelError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &core.Error{Status: http.StatusBadGateway, Code: "model", Msg: err.Error(), Err: err}
}

func selfOf(e *api.Entity) string {
	if e == nil {
		return ""
	}
	for _, l := range e.Links {
		for _, r := range l.Rel {
			if r == "self" {
				return l.Href
			}
		}
	}
	return ""
}
