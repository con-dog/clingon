package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A chat goes to OpenRouter's chat API as the model's own request: tools as
// functions, the key as a bearer token; the reply's tool calls come back.
func TestChat(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","reasoning":"thinking","tool_calls":[{"id":"c1","type":"function","function":{"name":"press","arguments":"{\"keys\":[\"Down\"]}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`)
	}))
	defer srv.Close()
	c := &Client{Key: "sk-or-test", Model: "deepseek/deepseek-v4-pro", Effort: "low", Endpoint: srv.URL}
	reply, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}},
		[]Tool{{Name: "press", Description: "Press keys", Params: map[string]any{"keys": map[string]any{"type": "array"}}, Required: []string{"keys"}}})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-or-test" || got["model"] != "deepseek/deepseek-v4-pro" || got["tool_choice"] != "auto" {
		t.Errorf("request: auth %q, %v", auth, got)
	}
	if _, cached := got["cache_control"]; cached {
		t.Error("cache markers are only for Claude models")
	}
	if r, _ := got["reasoning"].(map[string]any); r["effort"] != "low" {
		t.Errorf("reasoning %v", got["reasoning"])
	}
	tools, _ := got["tools"].([]any)
	if len(tools) != 1 || !strings.Contains(mustJSON(tools[0]), `"name":"press"`) {
		t.Errorf("tools %v", got["tools"])
	}
	if reply.Finish != "tool_calls" || len(reply.Message.ToolCalls) != 1 || reply.Message.ToolCalls[0].Function.Name != "press" || reply.Message.Reasoning != "thinking" || reply.Usage.Prompt != 10 {
		t.Errorf("reply %+v", reply)
	}
}

func TestChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"message":"No auth credentials found","code":401}}`)
	}))
	defer srv.Close()
	c := &Client{Key: "bad", Model: "x/y", Endpoint: srv.URL}
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err == nil || !strings.Contains(err.Error(), "No auth credentials") {
		t.Errorf("err %v", err)
	}
	if _, err := (&Client{Model: "x/y", Endpoint: srv.URL}).Chat(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "clingon ai key") {
		t.Errorf("no key: %v", err)
	}
}

func TestShortName(t *testing.T) {
	for in, want := range map[string]string{"anthropic/claude-sonnet-5.5": "sonnet-5.5", "deepseek/deepseek-v4.1-flash": "deepseek-v4.1-flash", "~anthropic/claude-sonnet-latest": "sonnet-latest", "openai/gpt-5:nitro": "gpt-5"} {
		if got := ShortName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	if s := LoadSettings(dir); s.Model != DefaultModel {
		t.Errorf("default %q", s.Model)
	}
	if err := SaveSettings(dir, Settings{Model: "deepseek/deepseek-v4-pro"}); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(dir); s.Model != "deepseek/deepseek-v4-pro" {
		t.Errorf("saved %q", s.Model)
	}
}

func TestKeyFromEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", " sk-or-env ")
	if Key() != "sk-or-env" {
		t.Errorf("key %q", Key())
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// HasKey goes by the settings' note that a key was kept, or the variable:
// it never asks the Keychain, which takes a process each time.
func TestHasKeyNeedsNoKeychain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "")
	if HasKey(dir) {
		t.Fatal("no note, no variable: no key")
	}
	if err := setKeyed(dir, true); err != nil {
		t.Fatal(err)
	}
	if !HasKey(dir) || LoadSettings(dir).Model != DefaultModel {
		t.Error("noted: a key, and the model's still the default")
	}
	_ = setKeyed(dir, false)
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	if !HasKey(dir) {
		t.Error("the variable's a key")
	}
}
