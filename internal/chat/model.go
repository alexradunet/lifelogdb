// Package chat is the agent of the action ask (docs/plans/102-a-conversation-with-lifelog.md): a model the owner
// configures answers a question with the tools of the catalog, called in-process as agent:chat, and the turn is
// appended to a conversation page.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Model is an endpoint of the OpenAI chat-completions protocol: a base URL that ends in /v1, a model id and an
// optional key. LM Studio, llama.cpp, Ollama and most hosted providers speak it.
type Model struct {
	URL, Name, Key string
	HTTP           *http.Client // nil: http.DefaultClient
}

// ModelFromEnv is the model of LIFELOG_MODEL_URL, LIFELOG_MODEL and LIFELOG_MODEL_KEY; nil when the first two are
// not both set, and then ask is refused.
func ModelFromEnv() *Model {
	url, name := os.Getenv("LIFELOG_MODEL_URL"), os.Getenv("LIFELOG_MODEL")
	if url == "" || name == "" {
		return nil
	}
	return &Model{URL: strings.TrimRight(url, "/"), Name: name, Key: os.Getenv("LIFELOG_MODEL_KEY")}
}

// message is one message of the protocol. Content is a pointer: an assistant message with tool calls may have none.
type message struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type toolSpec struct {
	Type     string   `json:"type"`
	Function function `json:"function"`
}

// maxAnswer bounds what is read of one answer of the model.
const maxAnswer = 8 << 20

func text(s string) *string { return &s }

// complete sends the messages and the tools, and returns the model's next message.
func (m *Model) complete(ctx context.Context, msgs []message, tools []toolSpec) (message, error) {
	body, err := json.Marshal(map[string]any{"model": m.Name, "messages": msgs, "tools": tools})
	if err != nil {
		return message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.URL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.Key != "" {
		req.Header.Set("Authorization", "Bearer "+m.Key)
	}
	hc := m.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return message{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer+1))
	if err != nil {
		return message{}, err
	}
	if len(raw) > maxAnswer {
		return message{}, errors.New("the model's answer is larger than 8 MiB")
	}
	if res.StatusCode != http.StatusOK {
		return message{}, fmt.Errorf("the model answered %s: %s", res.Status, firstLine(string(raw)))
	}
	var out struct {
		Choices []struct {
			Message message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return message{}, fmt.Errorf("the model's answer is not chat-completions JSON: %w", err)
	}
	if len(out.Choices) == 0 {
		return message{}, errors.New("the model's answer has no choice")
	}
	return out.Choices[0].Message, nil
}

// firstLine is the start of an error body: enough to say what went wrong, never a whole page.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
