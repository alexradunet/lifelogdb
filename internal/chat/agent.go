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

// Source is the writer of every row the agent writes, the conversation pages included.
const Source = "agent:chat"

const (
	maxSteps      = 16       // model answers in one turn: a model that never stops calling tools is stopped
	maxToolResult = 64 << 10 // bytes of one tool result sent back to the model
)

// Agent answers ask. Client calls the API in-process as Source; it is set after the handler that holds the Agent
// is made. Now is the server's clock (nil: time.Now): the title of a conversation and the times of its turns.
type Agent struct {
	Model  *Model
	Client *client.Client
	Now    func() time.Time
}

// Ask runs one turn: the model and its tool calls, then the turn appended to the conversation page (a new page when
// conversation is 0). The caller has checked that conversation names a live conversation page.
func (a *Agent) Ask(ctx context.Context, question string, conversation int64) (api.Answer, error) {
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
	var title, body, version string
	if conversation != 0 {
		if title, body, version, err = a.page(ctx, conversation); err != nil {
			return api.Answer{}, err
		}
	}
	msgs := []message{{Role: "system", Content: text(systemPrompt(now))}}
	if strings.TrimSpace(body) != "" {
		msgs = append(msgs, message{Role: "user", Content: text("The conversation so far, from the page " + title + ":\n\n" + body)})
	}
	msgs = append(msgs, message{Role: "user", Content: text(question)})
	steps, reply, runErr := a.run(ctx, msgs, tools)
	if runErr != nil && len(steps) == 0 {
		return api.Answer{}, modelError(runErr)
	}
	turn := transcript(now, question, steps, reply, runErr)
	if conversation == 0 {
		conversation, err = a.newConversation(ctx, actions, now, turn)
	} else {
		err = a.append(ctx, actions, conversation, body, version, turn)
	}
	if err != nil {
		return api.Answer{}, errors.Join(modelError(runErr), err)
	}
	if runErr != nil { // rows were written: the turn says which, and the error says where it is kept
		return api.Answer{}, modelError(fmt.Errorf("%w (the turn is kept on /pages/%d)", runErr, conversation))
	}
	return api.Answer{Conversation: conversation, Reply: reply, Steps: steps}, nil
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

// page is the title, body and version of a conversation page.
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

// newConversation creates the page of a new conversation, holding its first turn, and files it in Conversations
// (created by the first conversation).
func (a *Agent) newConversation(ctx context.Context, actions []api.Action, now time.Time, turn string) (int64, error) {
	if _, err := a.do(ctx, actions, "find", map[string]string{"title": api.ConversationsTitle}); err != nil {
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != http.StatusNotFound {
			return 0, err
		}
		if _, err := a.do(ctx, actions, "create-page", map[string]string{"title": api.ConversationsTitle,
			"body": "The conversations with lifelog: each page is one, filed here by part-of."}); err != nil {
			return 0, err
		}
	}
	e, err := a.do(ctx, actions, "create-page", map[string]string{"title": "Conversation " + now.Format("2006-01-02 15.04.05"), "body": turn})
	if err != nil {
		return 0, err
	}
	p, _ := e.Properties.(map[string]any)
	n, _ := p["id"].(json.Number) // the client decodes numbers as json.Number
	id, err := n.Int64()
	if err != nil {
		return 0, fmt.Errorf("create-page answered with no id: %w", err)
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

// transcript is one turn as the page keeps it: the owner's question, a line for each tool call, the reply.
func transcript(now time.Time, question string, steps []api.Step, reply string, runErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Owner** (%s): %s\n\n", now.Format("15.04"), strings.TrimSpace(question))
	for _, s := range steps {
		switch {
		case s.Error != "":
			fmt.Fprintf(&b, "- `%s`: %s\n", s.Tool, firstLine(s.Error))
		case s.Href != "":
			fmt.Fprintf(&b, "- `%s` → [%s](%s)\n", s.Tool, s.Href, s.Href)
		default:
			fmt.Fprintf(&b, "- `%s`\n", s.Tool)
		}
	}
	if len(steps) > 0 {
		b.WriteString("\n")
	}
	if runErr != nil {
		fmt.Fprintf(&b, "**lifelog** did not answer: %s\n", firstLine(runErr.Error()))
	} else {
		fmt.Fprintf(&b, "**lifelog:** %s\n", reply)
	}
	return b.String()
}

func systemPrompt(now time.Time) string {
	return client.Instructions + "\n\n" +
		"You talk with the owner of this life log, inside lifelog itself. Today is " + now.Format("2006-01-02 (Monday)") +
		", and the local time is " + now.Format("15:04") + ". Answer in the owner's language, briefly. " +
		"Write with the tools when the owner asks for it. When you are not sure what the owner wants written, ask first and write nothing. " +
		"The owner's own actions (register-metric, relocate-reading, snapshot) are not your tools: when one is needed, say so, and the owner does it. " +
		"Write [[Title]] in your reply only for a page that you read in a tool result: a [[Title]] links the conversation to that page."
}

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
