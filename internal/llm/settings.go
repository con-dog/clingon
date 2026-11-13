package llm

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// keychainService names the OpenRouter key in the macOS Keychain.
const keychainService = "clingon-openrouter"

// Key is the OpenRouter key: $OPENROUTER_API_KEY if it's set, else the one
// kept in the Keychain, else "".
func Key() string {
	if k := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); k != "" {
		return k
	}
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-w").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// HasKey says whether there's a key to use: $OPENROUTER_API_KEY, or one
// SetKey kept, as the settings in dir (clingon's config folder) say. It
// doesn't ask the Keychain, which takes a process each time and the menu asks
// often; a key taken out of the Keychain by hand is found missing when a run
// starts, by Key.
func HasKey(dir string) bool {
	return strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) != "" || LoadSettings(dir).Keyed
}

// SetKey keeps key in the Keychain, replacing any kept before, and notes in
// the settings in dir that there is one. It's given to `security` on its
// input, not its command line, so other programs listing what's running never
// see it.
func SetKey(dir, key string) error {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, " \t\n\"'\\") {
		return errors.New("that doesn't look like an OpenRouter key")
	}
	cmd := exec.Command("security", "-i")
	cmd.Stdin = strings.NewReader("add-generic-password -U -a clingon -s " + keychainService + " -w " + key + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New("couldn't keep the key in the Keychain: " + strings.TrimSpace(string(out)))
	}
	return setKeyed(dir, true)
}

// RemoveKey takes the key out of the Keychain.
func RemoveKey(dir string) error {
	if out, err := exec.Command("security", "delete-generic-password", "-s", keychainService).CombinedOutput(); err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return setKeyed(dir, false)
}

func setKeyed(dir string, on bool) error {
	s := LoadSettings(dir)
	s.Keyed = on
	return SaveSettings(dir, s)
}

// Settings are the AI's own settings, in clingon's config folder.
type Settings struct {
	Model string `json:"model"`
	Dev   bool   `json:"dev,omitempty"`   // for working on clingon's AI: what each reply cost, in the log
	Keyed bool   `json:"keyed,omitempty"` // SetKey kept a key in the Keychain
}

func settingsFile(dir string) string { return filepath.Join(dir, "ai.json") }

// LoadSettings reads the settings in dir (clingon's config folder), with
// the default model if none's chosen.
func LoadSettings(dir string) Settings {
	s := Settings{Model: DefaultModel}
	if data, err := os.ReadFile(settingsFile(dir)); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if strings.TrimSpace(s.Model) == "" {
		s.Model = DefaultModel
	}
	return s
}

// SaveSettings keeps the settings in dir.
func SaveSettings(dir string, s Settings) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsFile(dir), data, 0o644)
}
