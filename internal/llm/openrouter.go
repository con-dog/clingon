// Package llm talks to language models through OpenRouter's chat API, which
// serves every maker's models the same way: messages in, a reply (text and
// tool calls) out. No SDK, and the request itself goes through the curl macOS
// ships (see post): Go's own HTTP and TLS would be a third of clingon's size.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Endpoint is OpenRouter's chat API.
const Endpoint = "https://openrouter.ai/api/v1/chat/completions"

// DefaultModel is the model used until another's chosen.
const DefaultModel = "anthropic/claude-sonnet-5.5"

// Client sends chats to one model.
type Client struct {
	Key, Model string
	Effort     string // reasoning effort ("low", "medium", "high"); "" leaves it to the model
	Endpoint   string // "" for OpenRouter's
}

// Message is one turn of a chat.
type Message struct {
	Role       string     `json:"role"` // system, user, assistant or tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Reasoning  string     `json:"reasoning,omitempty"` // the model's reasoning, where it shares it
	// ReasoningDetails is passed back as it came, so a model that reasons
	// between tool calls carries on from where it was.
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
}

// ToolCall is the model asking for a tool to be used.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON
	} `json:"function"`
}

// Tool is a tool the model may use: what it's for, and its arguments as
// JSON Schema properties.
type Tool struct {
	Name, Description string
	Params            map[string]any
	Required          []string
}

// Usage is what a reply cost, in tokens.
type Usage struct {
	Prompt     int     `json:"prompt_tokens"`
	Completion int     `json:"completion_tokens"`
	Cost       float64 `json:"cost"` // dollars, as OpenRouter charged it
	Details    struct {
		Cached int `json:"cached_tokens"` // of Prompt, read from the cache
	} `json:"prompt_tokens_details"`
}

// Reply is the model's turn.
type Reply struct {
	Message Message
	Finish  string // stop, tool_calls, length, …
	Usage   Usage
}

type toolJSON struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// Chat sends the conversation so far and returns the model's reply.
func (c *Client) Chat(ctx context.Context, msgs []Message, tools []Tool) (Reply, error) {
	if c.Key == "" {
		return Reply{}, errors.New("no OpenRouter key: add one with `clingon ai key`")
	}
	if strings.ContainsAny(c.Key, "\r\n") { // it's sent as a header, which a line break would end
		return Reply{}, errors.New("that doesn't look like an OpenRouter key")
	}
	body := map[string]any{"model": c.Model, "messages": msgs, "max_tokens": 16000,
		"usage": map[string]bool{"include": true}} // what it cost, cache reads and all
	if len(tools) > 0 {
		var ts []toolJSON
		for _, t := range tools {
			var j toolJSON
			j.Type = "function"
			j.Function.Name, j.Function.Description = t.Name, t.Description
			req := t.Required
			if req == nil {
				req = []string{}
			}
			j.Function.Parameters = map[string]any{"type": "object", "properties": t.Params, "required": req}
			ts = append(ts, j)
		}
		body["tools"], body["tool_choice"] = ts, "auto"
	}
	if c.Effort != "" {
		body["reasoning"] = map[string]any{"effort": c.Effort}
	}
	if strings.HasPrefix(strings.TrimPrefix(c.Model, "~"), "anthropic/") {
		body["cache_control"] = map[string]string{"type": "ephemeral"} // the conversation so far, cached: each turn only adds to it
	}
	data, err := json.Marshal(body)
	if err != nil {
		return Reply{}, err
	}
	url := c.Endpoint
	if url == "" {
		url = Endpoint
	}
	var last error
	for attempt := range 3 { // a busy or briefly failing provider gets another go
		reply, retry, err := c.send(ctx, url, data)
		if err == nil || !retry {
			return reply, err
		}
		last = err
		if attempt == 2 { // the last go: nothing to wait for
			break
		}
		select {
		case <-ctx.Done():
			return Reply{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	return Reply{}, last
}

func (c *Client) send(ctx context.Context, url string, data []byte) (reply Reply, retry bool, err error) {
	code, raw, err := post(ctx, url, c.Key, data)
	if err != nil {
		return Reply{}, ctx.Err() == nil, err
	}
	var out struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
		Error *struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal(raw, &out); jerr != nil {
		return Reply{}, code >= 500, fmt.Errorf("OpenRouter: %s: %s", status(code), strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	if out.Error != nil || code >= 400 {
		msg := status(code)
		if out.Error != nil {
			msg = out.Error.Message
		}
		retry := code == 429 || code >= 500
		return Reply{}, retry, fmt.Errorf("OpenRouter: %s", msg)
	}
	if len(out.Choices) == 0 {
		return Reply{}, true, errors.New("OpenRouter: no reply")
	}
	ch := out.Choices[0]
	return Reply{Message: ch.Message, Finish: ch.FinishReason, Usage: out.Usage}, false, nil
}

// curl is the one macOS ships, by its full path: always there, and never
// another that happens to be on PATH.
const curl = "/usr/bin/curl"

// maxReply is the most of a reply that's read.
const maxReply = 32 << 20

// post sends body to url as JSON with key as the bearer token, and returns
// the HTTP status and the reply's body. curl does the sending. The key goes
// to it in a config file down a pipe of its own, never on its command line,
// so other programs listing what's running never see it; the body goes on
// its input. Cancelling ctx stops it. The key must be one line (Chat sees
// to that).
func post(ctx context.Context, url, key string, body []byte) (code int, reply []byte, err error) {
	conf, feed, err := os.Pipe()
	if err != nil {
		return 0, nil, err
	}
	// --disable (which must come first) leaves the user's own ~/.curlrc out
	// of it; ExtraFiles' first file is the child's descriptor 3.
	cmd := exec.CommandContext(ctx, curl, "--disable", "--config", "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{conf}
	cmd.Stdin = bytes.NewReader(body)
	out, errOut := &capped{}, &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = out, errOut
	err = cmd.Start()
	conf.Close()
	if err != nil {
		feed.Close()
		return 0, nil, err
	}
	_, _ = feed.WriteString(curlConfig(url, key))
	feed.Close()
	err = cmd.Wait()
	switch {
	case ctx.Err() != nil:
		return 0, nil, ctx.Err()
	case out.over:
		return 0, nil, errors.New("OpenRouter: the reply was too big to read")
	case err != nil:
		// "curl: (6) Could not resolve host: openrouter.ai", without the start.
		msg := strings.TrimSpace(errOut.String())
		if _, after, ok := strings.Cut(msg, ") "); ok && strings.HasPrefix(msg, "curl: (") {
			msg = after
		}
		if msg == "" {
			msg = err.Error()
		}
		return 0, nil, errors.New("OpenRouter: " + msg)
	}
	// The status follows the body, on a line of its own (write-out, below).
	raw := out.Bytes()
	at := bytes.LastIndexByte(raw, '\n')
	if at < 0 {
		return 0, nil, errors.New("OpenRouter: no reply")
	}
	code, err = strconv.Atoi(string(raw[at+1:]))
	if err != nil {
		return 0, nil, errors.New("OpenRouter: no reply")
	}
	return code, raw[:at], nil
}

// curlConfig is the request as a curl config file: one option a line, as
// its long name without the dashes.
func curlConfig(url, key string) string {
	quote := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	var b strings.Builder
	b.WriteString("silent\nshow-error\ngloboff\ncompressed\nmax-time = 300\n") // five minutes, the longest a reply's waited for
	b.WriteString("url = \"" + quote(url) + "\"\n")
	b.WriteString("header = \"Authorization: Bearer " + quote(key) + "\"\n")
	b.WriteString("header = \"Content-Type: application/json\"\n")
	b.WriteString("header = \"X-Title: clingon\"\n")
	b.WriteString("header = \"Expect:\"\n") // send the body at once: don't ask first and wait a second for the go-ahead
	b.WriteString("data-binary = \"@-\"\n")
	b.WriteString("write-out = \"\\n%{http_code}\"\n")
	// A server on this machine is never reached through a proxy, whatever
	// the environment says.
	for _, local := range []string{"http://127.0.0.1", "http://localhost", "http://[::1]"} {
		if strings.HasPrefix(url, local) {
			b.WriteString("noproxy = \"*\"\n")
		}
	}
	return b.String()
}

// capped is a buffer that takes maxReply bytes and no more.
type capped struct {
	bytes.Buffer
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.Len()+len(p) > maxReply {
		c.over = true
		return 0, errors.New("too big")
	}
	return c.Buffer.Write(p)
}

// status is an HTTP status as it's said: "429 Too Many Requests".
func status(code int) string {
	text := map[int]string{
		400: "Bad Request", 401: "Unauthorized", 402: "Payment Required", 403: "Forbidden",
		404: "Not Found", 408: "Request Timeout", 413: "Payload Too Large", 429: "Too Many Requests",
		500: "Internal Server Error", 502: "Bad Gateway", 503: "Service Unavailable", 504: "Gateway Timeout",
	}[code]
	return strings.TrimSpace(strconv.Itoa(code) + " " + text)
}

// ShortName is a model's name for a chip: "anthropic/claude-sonnet-5.5"
// is "sonnet-5.5".
func ShortName(model string) string {
	m := strings.TrimPrefix(model, "~")
	if _, after, ok := strings.Cut(m, "/"); ok {
		m = after
	}
	m = strings.TrimPrefix(m, "claude-")
	if before, _, ok := strings.Cut(m, ":"); ok {
		m = before
	}
	return m
}
