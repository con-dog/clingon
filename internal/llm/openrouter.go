// Package llm talks to language models through OpenRouter's chat API, which
// serves every maker's models the same way: messages in, a reply (text and
// tool calls) out. Plain HTTP, no SDK, to keep clingon small.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	HTTP       *http.Client
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
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return Reply{}, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Title", "clingon")
		reply, retry, err := c.send(req)
		if err == nil || !retry {
			return reply, err
		}
		last = err
		select {
		case <-ctx.Done():
			return Reply{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	return Reply{}, last
}

func (c *Client) send(req *http.Request) (reply Reply, retry bool, err error) {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Reply{}, req.Context().Err() == nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Reply{}, true, err
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
		return Reply{}, resp.StatusCode >= 500, fmt.Errorf("OpenRouter: %s: %s", resp.Status, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	if out.Error != nil || resp.StatusCode >= 400 {
		msg := resp.Status
		if out.Error != nil {
			msg = out.Error.Message
		}
		retry := resp.StatusCode == 429 || resp.StatusCode >= 500
		return Reply{}, retry, fmt.Errorf("OpenRouter: %s", msg)
	}
	if len(out.Choices) == 0 {
		return Reply{}, true, errors.New("OpenRouter: no reply")
	}
	ch := out.Choices[0]
	return Reply{Message: ch.Message, Finish: ch.FinishReason, Usage: out.Usage}, false, nil
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
