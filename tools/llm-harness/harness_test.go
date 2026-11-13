package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/con-dog/clingon/internal/llm"
	"github.com/con-dog/clingon/internal/palette"
)

func screenMsg(n int) llm.Message {
	return llm.Message{Role: "tool", Content: fmt.Sprintf("Screen (action %d):\n\n```\nscreen %d\n```", n, n)}
}

// Old screens go in batches, keeping the latest, so the chat stays bounded
// and its start stays put between batches (for prompt caching).
func TestTrimScreens(t *testing.T) {
	msgs := []llm.Message{{Role: "system"}, {Role: "user", Content: "```\nfirst screen\n```"}}
	for n := 1; n < keepScreens+trimEvery; n++ {
		msgs = append(msgs, screenMsg(n))
	}
	before := fmt.Sprint(msgs)
	trimScreens(msgs)
	if fmt.Sprint(msgs) != before {
		t.Fatal("trimmed before a batch had piled up")
	}
	msgs = append(msgs, screenMsg(keepScreens+trimEvery))
	trimScreens(msgs)
	full := 0
	for _, m := range msgs {
		if m.Role == "tool" && strings.Contains(m.Content, "```") {
			full++
		}
	}
	if full != keepScreens {
		t.Fatalf("%d screens kept, want %d", full, keepScreens)
	}
	if last := msgs[len(msgs)-1].Content; !strings.Contains(last, fmt.Sprintf("screen %d", keepScreens+trimEvery)) {
		t.Errorf("the latest screen went: %q", last)
	}
	if !strings.Contains(msgs[1].Content, "first screen") {
		t.Error("the task's own first screen went")
	}
	if !strings.HasPrefix(msgs[2].Content, "Screen (action 1):") {
		t.Errorf("a left-out screen should keep its heading: %q", msgs[2].Content)
	}
}

// The menu's cursor is a cell in the palette's pink (grey when blurred).
func TestRenderCursor(t *testing.T) {
	cell := func(c palette.Colour) string { return fmt.Sprintf("\x1b[48;5;%dm \x1b[0m", c.Num) }
	raw := cell(palette.Pink) + "  Git\n" + cell(palette.Grey) + "  Files\n" + cell(palette.Pink) + cell(palette.Pink) + " key"
	want := "▶  Git\n▷  Files\n   key"
	if got := render(raw); got != want {
		t.Errorf("render:\n%q\nwant\n%q", got, want)
	}
}

// The harness types through its own control client and knows when the
// screen has settled from tmux's output events.
func TestTermTypesAndSettles(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	tm := &term{server: fmt.Sprintf("clingon-llm-test-%d", os.Getpid()), session: "test", env: os.Environ()}
	if err := tm.start(t.TempDir(), "cat", 40, 10); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = tm.tmux("kill-server") })
	if _, err := tm.settle(200*time.Millisecond, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := tm.sendText("hello"); err != nil {
		t.Fatal(err)
	}
	if err := tm.sendKey("Enter"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	screen, err := tm.settle(200*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(screen, "hello") != 2 { // typed, then cat's echo
		t.Errorf("screen:\n%s", screen)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("settling took %v: it should follow the output, not the deadline", d)
	}
	if w, _ := tm.tmux("display", "-p", "-t", "test", "#{window_width}x#{window_height}"); strings.TrimSpace(w) != "40x11" {
		t.Errorf("window is %s, want the fixed 40x11", w)
	}
}
