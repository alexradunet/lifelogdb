// Package importrun is `lifelog import run`: the facts pass of an import driven by the writer itself (plan 088).
// It follows import status and runs each step that needs no judgement; for the facts of one source file it asks a
// model on this machine, and every write goes through the writer's own operations (docs/guides/importing.md).
package importrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Model answers the facts of one source file: system holds the instructions, user the file and its rules.
type Model interface {
	Facts(ctx context.Context, system, user string, maxTokens int) (string, error)
}

// maxAnswer bounds a model's answer: a facts object of one file is far smaller.
const maxAnswer = 4 << 20

// callTimeout bounds one completion; a small model on a long note takes about a minute.
const callTimeout = 10 * time.Minute

// Local is an OpenAI-compatible chat completions server on this machine (strata, LM Studio, llama.cpp).
type Local struct {
	base  string // the URL up to /v1, without a trailing slash
	Name  string // the model the server runs
	hc    *http.Client
	dials atomic.Int64 // the connections opened
}

// ErrNotLoopback is the refusal of a model host that is not this machine: a source's text never leaves it.
var ErrNotLoopback = errors.New("the model must run on this machine: its host must be a loopback address")

// NewLocal checks the URL and finds the model. The host must resolve to loopback addresses only; the client then
// dials those addresses itself, so a second resolution cannot send the text elsewhere, uses no proxy and follows
// no redirect. An empty name takes the one model the server lists.
func NewLocal(ctx context.Context, rawURL, name string) (*Local, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("model URL %q: want http://127.0.0.1:PORT/v1", rawURL)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("model host %q: %w", u.Hostname(), ErrNotLoopback)
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() {
			return nil, fmt.Errorf("model host %q is %s: %w", u.Hostname(), ip.IP, ErrNotLoopback)
		}
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	addr := net.JoinHostPort(ips[0].IP.String(), port)
	l := &Local{base: strings.TrimRight(u.String(), "/"), Name: name}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	l.hc = &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				l.dials.Add(1)
				return dialer.DialContext(ctx, network, addr)
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if name == "" {
		if l.Name, err = l.onlyModel(ctx); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// onlyModel is the one model GET /models lists; none or several is a refusal: the owner names it with --model.
func (l *Local) onlyModel(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", l.base+"/models", nil)
	if err != nil {
		return "", err
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := l.call(req, &list); err != nil {
		return "", fmt.Errorf("the model server's list of models: %w", err)
	}
	if len(list.Data) != 1 || list.Data[0].ID == "" {
		return "", fmt.Errorf("the model server lists %d models: name one with --model", len(list.Data))
	}
	return list.Data[0].ID, nil
}

// Facts asks for one completion at temperature 0, with the model's thinking off, and returns its text.
func (l *Local) Facts(ctx context.Context, system, user string, maxTokens int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	body, err := json.Marshal(map[string]any{
		"model": l.Name, "temperature": 0, "max_tokens": maxTokens,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"messages":             []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", l.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var answer struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := l.call(req, &answer); err != nil {
		return "", err
	}
	if len(answer.Choices) == 0 {
		return "", errors.New("the model answered no choice")
	}
	return answer.Choices[0].Message.Content, nil
}

func (l *Local) call(req *http.Request, v any) error {
	res, err := l.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer+1))
	if err != nil {
		return err
	}
	if len(data) > maxAnswer {
		return errors.New("the model's answer is too long")
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the model server answered HTTP %d", res.StatusCode)
	}
	return json.Unmarshal(data, v)
}
