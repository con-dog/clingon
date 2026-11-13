package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/llm"
	"github.com/con-dog/clingon/internal/ui"
	"golang.org/x/term"
)

// aiCmd sets up clingon's AI: `clingon ai` says how it's set up, `ai key`
// keeps an OpenRouter key in the Keychain, `ai model <model>` picks the model.
// Giving it a task is done in the menu, where you can watch it.
func aiCmd(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "":
		s := llm.LoadSettings(config.StateDir())
		key := "none: add one with `clingon ai key`"
		if os.Getenv("OPENROUTER_API_KEY") != "" {
			key = "from $OPENROUTER_API_KEY"
		} else if llm.HasKey(config.StateDir()) {
			key = "kept in the Keychain"
		}
		outln(card("AI", []row{
			{mark: ok, label: "model", detail: s.Model},
			{mark: ok, label: "OpenRouter key", detail: key},
		}, cardWidth()))
		return nil
	case "key":
		k, err := readKey()
		if err != nil {
			return err
		}
		if err := llm.SetKey(config.StateDir(), k); err != nil {
			return err
		}
		say(ok, "kept your OpenRouter key in the Keychain")
		say(ok, "the AI's runs go to OpenRouter and the model's maker: each task, the menu as text, and the shell's last lines")
		return nil
	case "log": // the last run's transcript, to look into how it went
		logs, _ := filepath.Glob(filepath.Join(ui.AILogDir(), "*.md"))
		if len(logs) == 0 {
			return errors.New("no AI runs yet: give it a task in the menu, Clingon › AI › Give a task")
		}
		sort.Strings(logs)
		data, err := os.ReadFile(logs[len(logs)-1])
		if err != nil {
			return err
		}
		flushSaid()
		fmt.Printf("%s\n\n%s", logs[len(logs)-1], data)
		return nil
	case "dev": // for working on the AI itself: not for clingon's users, so not in the menu
		on := len(args) > 1 && args[1] == "on"
		if len(args) < 2 || args[1] != "on" && args[1] != "off" {
			return errors.New("`clingon ai dev on` or `clingon ai dev off`")
		}
		s := llm.LoadSettings(config.StateDir())
		s.Dev = on
		if err := llm.SaveSettings(config.StateDir(), s); err != nil {
			return err
		}
		if on {
			say(ok, "AI dev mode on: what each reply cost, cache reads and all, in `clingon ai log`")
		} else {
			say(ok, "AI dev mode off")
		}
		return nil
	case "model":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return errors.New("name a model, e.g. `clingon ai model anthropic/claude-sonnet-5.5`")
		}
		model := strings.TrimSpace(args[1])
		if !strings.Contains(model, "/") {
			return fmt.Errorf("%q isn't an OpenRouter model: they're maker/model, like anthropic/claude-sonnet-5.5", model)
		}
		st := llm.LoadSettings(config.StateDir())
		st.Model = model
		if err := llm.SaveSettings(config.StateDir(), st); err != nil {
			return err
		}
		say(ok, "the AI uses %s", model)
		return nil
	}
	return fmt.Errorf("clingon ai: %q? Try `clingon ai`, `clingon ai key`, `clingon ai model <model>` or `clingon ai log`", sub)
}

// readKey asks for the key, its typing hidden; piped in, it's read as is.
func readKey() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no key given")
		}
		return strings.TrimSpace(line), nil
	}
	fmt.Fprint(os.Stderr, "OpenRouter key (from openrouter.ai/keys; typing is hidden): ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", errors.New("no key given: nothing changed")
	}
	return strings.TrimSpace(string(b)), nil
}
