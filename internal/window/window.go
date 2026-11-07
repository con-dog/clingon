// Package window opens clingon in a window of its own, in the user's
// terminal app, filling the screen, set in clingon's font and colours. Only
// that window: the terminal's default profile and every other window stay as
// they are.
//
// clingon's font, Clingon Mono, is JetBrains Mono plus brand logos for
// packs (font/build.py); clingon always runs in this window, so the logos
// can be counted on.
package window

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed font/ClingonMono-Regular.otf
var fontFile []byte

const (
	fontSize = 13 // fontFamily, fontPS and fontFileName are in fontname.go

	// The window's own colours, in clingon's palette (internal/ui/styles.go):
	// its ink, text, cursor and selection.
	bg, fg, bold, cursor, selection = "#1a1724", "#e8e6f0", "#ffffff", "#ff87d7", "#3d3555"

	cols, rows = 170, 48
)

// ansi is the window's 16 standard colours, which most tools (ls, git,
// grep, compilers, test runners) colour their output with: clingon's
// palette instead of the terminal's, so the whole window is one piece. Blue
// stays clear of the prompt's purple, which is 99 and not one of these.
// Black, red, green, yellow, blue, magenta, cyan, white, then bright ones.
//
// Black is a step lighter than the window's own background, in its violet:
// what's raised on it, as a page laid on top (the form's preview, the
// command's block, the cursor's row). Terminal.app has no 24-bit colour, and
// none of the other 256 is a dark violet, so this is the one way to draw one.
var ansi = [16]string{
	"#2f2a44", "#ff5f87", "#5fd7af", "#ffd787", "#87afff", "#ff87d7", "#5fd7ff", "#c6c2d6",
	"#5f5f87", "#ff87af", "#87ffd7", "#ffffaf", "#afafff", "#ffafff", "#afffff", "#ffffff",
}

var ansiNames = [8]string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

// fontDir is where a font is installed for the user alone.
func fontDir(home string) string {
	return filepath.Join(home, "Library", "Fonts")
}

// InstallFont puts Clingon Mono where the user's apps find it, unless
// it's there already, and removes older versions of it. It reports whether
// it installed it just now.
func InstallFont(home string) (bool, error) {
	dst := filepath.Join(fontDir(home), fontFileName)
	if have, err := os.ReadFile(dst); err == nil && bytes.Equal(have, fontFile) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	old, _ := filepath.Glob(filepath.Join(fontDir(home), "ClingonMono-*.otf"))
	for _, f := range old {
		if f != dst {
			_ = os.Remove(f)
		}
	}
	if err := os.WriteFile(dst, fontFile, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// Terminal names the terminal app clingon's window opens in: the one it's
// run from, when clingon knows how to dress a window of it.
func Terminal(env func(string) string) string {
	switch {
	case env("GHOSTTY_RESOURCES_DIR") != "" || env("TERM_PROGRAM") == "ghostty":
		return "ghostty"
	case env("KITTY_WINDOW_ID") != "":
		return "kitty"
	case env("ALACRITTY_WINDOW_ID") != "" || env("ALACRITTY_SOCKET") != "":
		return "alacritty"
	case env("TERM_PROGRAM") == "iTerm.app":
		return "iterm"
	case env("TERM_PROGRAM") == "Apple_Terminal":
		return "terminal"
	}
	return "terminal" // VS Code's terminal and the like: macOS's own app
}

// Plan is how to open the window: a program and its arguments, plus files
// to write first (iTerm's profile, Terminal's launch).
type Plan struct {
	Path      string
	Args      []string
	WriteFile string
	Contents  []byte
	Fresh     bool // WriteFile is one launch's, so it mustn't be there already

	Runner         string // a fixed script to keep up to date, for Terminal
	RunnerContents []byte
}

// PlanFor works out how to open clingon (self) in a new window of
// terminal, starting in dir. clingon runs through the user's login shell,
// so Homebrew's PATH (and tmux) is there even when the app was started
// from the Dock.
func PlanFor(terminal, self, dir, shell, home string) (Plan, error) {
	if shell == "" {
		shell = "/bin/sh"
	}
	inner := "cd " + quote(dir) + " && " + OwnEnv + "=" + terminal + " exec " + quote(self)
	login := []string{shell, "-l", "-c", inner}
	switch terminal {
	case "terminal":
		// Terminal can only start something in a new window by typing it into
		// the window's shell, so it lands in the user's history. What's typed
		// is always the same (terminalScript adds a space, which keeps it out
		// altogether with hist_ignore_space or HISTCONTROL): a fixed script
		// that runs the oldest launch waiting in the queue. Run again from
		// history, it finds none and does nothing, rather than starting
		// clingon without its window, font and profile. A launch runs through
		// the user's login shell: the window that imports clingon's profile
		// runs it directly, without Homebrew's PATH.
		base := filepath.Join(home, "Library", "Caches", "clingon")
		queue := filepath.Join(base, "window")
		runner := filepath.Join(base, "open-window")
		script := filepath.Join(queue, fmt.Sprintf("%d.sh", time.Now().UnixNano()))
		run := "/bin/sh " + runner
		if !safePath.MatchString(runner) {
			run = "/bin/sh " + quote(runner)
		}
		args := []string{"-l", "JavaScript", "-e", terminalScript, terminalProfile(), fontPS,
			filepath.Join(fontDir(home), fontFileName), run,
			filepath.Join(home, "Library", "Caches", terminalProfile()+".terminal"), bg, fg, bold, cursor, selection}
		return Plan{
			Path:      "osascript",
			Args:      append(args, ansi[:]...),
			WriteFile: script,
			Fresh:     true,
			Contents:  []byte("rm -f \"$0\"\nexec " + strings.Join(quoteAll(login), " ") + "\n"),
			Runner:    runner,
			RunnerContents: []byte("for f in " + quote(queue) + "/*.sh; do\n" +
				"  mv \"$f\" \"$f.run\" 2>/dev/null && exec /bin/sh \"$f.run\"\n" +
				"done\nexit 0\n"),
		}, nil
	case "iterm":
		return Plan{
			Path:      "osascript",
			Args:      []string{"-e", itermScript, strings.Join(quoteAll(login), " ")},
			WriteFile: filepath.Join(home, "Library", "Application Support", "iTerm2", "DynamicProfiles", "clingon.json"),
			Contents:  itermProfile(),
		}, nil
	case "ghostty":
		args := []string{"--font-family=" + fontFamily, fmt.Sprintf("--font-size=%d", fontSize),
			"--background=" + bg, "--foreground=" + fg, "--cursor-color=" + cursor, "--selection-background=" + selection,
			fmt.Sprintf("--window-width=%d", cols), fmt.Sprintf("--window-height=%d", rows), "--maximize=true"}
		for i, c := range ansi {
			args = append(args, fmt.Sprintf("--palette=%d=%s", i, c))
		}
		args = append(append(args, "-e"), login...)
		return Plan{Path: "open", Args: append([]string{"-na", "Ghostty.app", "--args"}, args...)}, nil
	case "kitty":
		return Plan{Path: app("kitty", "/Applications/kitty.app/Contents/MacOS/kitty"), Args: append([]string{
			"-o", "font_family=" + fontFamily, "-o", fmt.Sprintf("font_size=%d", fontSize),
			"-o", "background=" + bg, "-o", "foreground=" + fg, "-o", "cursor=" + cursor, "-o", "selection_background=" + selection,
			"-o", fmt.Sprintf("initial_window_width=%dc", cols), "-o", fmt.Sprintf("initial_window_height=%dc", rows),
			"--start-as=maximized", "--directory", dir}, append(kittyPalette(), login...)...)}, nil
	case "alacritty":
		return Plan{Path: app("alacritty", "/Applications/Alacritty.app/Contents/MacOS/alacritty"), Args: append([]string{
			"-o", `font.normal.family="` + fontFamily + `"`, "-o", fmt.Sprintf("font.size=%d", fontSize),
			"-o", `colors.primary.background="` + bg + `"`, "-o", `colors.primary.foreground="` + fg + `"`,
			"-o", `colors.cursor.cursor="` + cursor + `"`, "-o", `colors.selection.background="` + selection + `"`,
			"-o", fmt.Sprintf("window.dimensions.columns=%d", cols), "-o", fmt.Sprintf("window.dimensions.lines=%d", rows),
			"-o", `window.startup_mode="Maximized"`,
			"--working-directory", dir}, append(append(alacrittyPalette(), "-e"), login...)...)}, nil
	}
	return Plan{}, fmt.Errorf("clingon doesn't know how to open a window in %q", terminal)
}

// writeNew makes a file only you can read, and refuses one that's there
// already, or a link: on a shared machine, the temp dir is everyone's.
func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(data)
	return err
}

// Run carries out a plan. Terminals started directly are detached, so the
// new window outlives the one it was opened from.
func Run(p Plan) error {
	if p.Runner != "" {
		if err := os.MkdirAll(filepath.Dir(p.Runner), 0o700); err != nil {
			return err
		}
		if have, err := os.ReadFile(p.Runner); err != nil || !bytes.Equal(have, p.RunnerContents) {
			if err := os.WriteFile(p.Runner, p.RunnerContents, 0o600); err != nil {
				return err
			}
		}
	}
	if p.WriteFile != "" {
		if err := os.MkdirAll(filepath.Dir(p.WriteFile), 0o755); err != nil {
			return err
		}
		if p.Fresh {
			dropStale(filepath.Dir(p.WriteFile))
		}
		write := func(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
		if p.Fresh {
			write = writeNew
		}
		if err := write(p.WriteFile, p.Contents); err != nil {
			return err
		}
	}
	cmd := exec.Command(p.Path, p.Args...)
	if p.Path == "osascript" || p.Path == "open" {
		out, err := cmd.CombinedOutput()
		if err != nil {
			if p.Fresh { // no window will come for it
				_ = os.Remove(p.WriteFile)
			}
			return runError(p.Path, string(out))
		}
		return nil
	}
	// Not clingon's own session's: the new window starts afresh.
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); k != "CLINGON" && k != "TMUX" && k != "TMUX_PANE" {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // reaped, so it doesn't linger as a zombie
	return cmd.Process.Release()
}

// dropStale removes launches no window came for, so a later window doesn't
// run them instead of its own.
func dropStale(queue string) {
	entries, _ := os.ReadDir(queue)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > time.Minute {
			_ = os.Remove(filepath.Join(queue, e.Name()))
		}
	}
}

// runError says why a terminal couldn't be told to open the window. -1743
// is macOS refusing, after the user said no to controlling their terminal.
func runError(path, out string) error {
	if strings.Contains(out, "-1743") {
		return errors.New("macOS isn't letting clingon control your terminal app, and that's how it opens its window: allow it in System Settings › Privacy & Security › Automation, then run clingon again")
	}
	return fmt.Errorf("%s: %s", filepath.Base(path), strings.TrimSpace(out))
}

// OwnEnv marks clingon as running in a window opened just for it; its
// value is the terminal app.
const OwnEnv = "CLINGON_WINDOW"

// TerminalCloser finds the Terminal window clingon is running in, while
// it runs (call it at the start), and returns what closes it once clingon
// has exited (Terminal keeps a window open after its command ends; the
// others close it). Found up front, by its tab's tty: once clingon exits,
// that tty can go to another tab, which must never be closed for it.
func TerminalCloser() (closeWhenDone func()) {
	found := make(chan [2]string, 1)
	go func() {
		tty := exec.Command("tty")
		tty.Stdin = os.Stdin
		out, err := tty.Output()
		if err != nil {
			found <- [2]string{}
			return
		}
		name := strings.TrimSpace(string(out))
		id, _ := exec.Command("osascript", "-e", findScript, name).Output()
		found <- [2]string{name, strings.TrimSpace(string(id))}
	}()
	return func() {
		var w [2]string
		select {
		case w = <-found:
		case <-time.After(time.Second): // never holds up exiting for long
		}
		if w[0] == "" || w[1] == "" {
			return
		}
		closeWindow(w[0], w[1])
	}
}

// closeWindow starts a detached osascript that waits for clingon's tab to
// go idle (so Terminal doesn't ask about running processes), then closes
// it. Best effort: if it can't, the window just stays open.
func closeWindow(tty, id string) {
	cmd := exec.Command("osascript", "-e", closeScript, tty, id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // off the tty, or Terminal waits on it
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// findScript prints the id of the Terminal window with a tab on the given tty.
const findScript = `on run argv
	tell application "Terminal"
		repeat with w in windows
			repeat with t in tabs of w
				if tty of t is item 1 of argv then return id of w
			end repeat
		end repeat
	end tell
	return ""
end run`

// closeScript closes clingon's tab (on the given tty, in the window with
// the given id) once nothing is running in it: the whole window when the
// tab is all it has, else just the tab, by ending its shell, so the user's
// other tabs stay. It stops if the window has gone (Terminal closes it
// itself when clingon was its command), and gives up after ten seconds.
const closeScript = `on run argv
	set theTTY to item 1 of argv
	set theID to (item 2 of argv) as integer
	tell application "Terminal"
		repeat 100 times
			try
				set w to window id theID
			on error
				return
			end try
			repeat with t in tabs of w
				if tty of t is theTTY and not busy of t then
					if (count of tabs of w) is 1 then
						close w
					else
						do script "exit" in t
					end if
					return
				end if
			end repeat
			delay 0.1
		end repeat
	end tell
end run`

// safePath is a path no shell needs quoted: a Terminal profile's command
// may not go through one.
var safePath = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// kittyPalette and alacrittyPalette set the 16 standard colours.
func kittyPalette() []string {
	var out []string
	for i, c := range ansi {
		out = append(out, "-o", fmt.Sprintf("color%d=%s", i, c))
	}
	return out
}

func alacrittyPalette() []string {
	var out []string
	for i, c := range ansi {
		out = append(out, "-o", fmt.Sprintf(`colors.%s.%s="%s"`, map[bool]string{false: "normal", true: "bright"}[i >= 8], ansiNames[i%8], c))
	}
	return out
}

// terminalProfile names clingon's Terminal profile after what's in it, so
// a changed font or palette is a new profile, made afresh.
func terminalProfile() string {
	h := sha256.Sum256([]byte(fontPS + bg + fg + bold + cursor + selection + strings.Join(ansi[:], "")))
	return fmt.Sprintf("clingon %x", h[:4])
}

// app is a terminal's binary: on PATH, else inside its macOS app.
func app(name, macPath string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if _, err := os.Stat(macPath); err == nil {
		return macPath
	}
	return name
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = quote(s)
	}
	return out
}

// terminalScript opens a Terminal.app window with clingon's profile. The
// profile is a file Terminal imports the first time: AppleScript can't set
// a profile's 16 standard colours or what its title shows. Importing opens
// the window too, running the command; after that, a window is opened and
// given the profile. It only ever adds clingon's profile: no profile is ever
// deleted, even an older one of clingon's, and no other is ever changed. Arguments: the profile's name, the
// font's PostScript name and file, the command, where to write the profile,
// then its background, text, bold, cursor and selection colours and the 16
// standard ones.
const terminalScript = `ObjC.import("AppKit");
ObjC.import("CoreText");

function run(argv) {
	const [name, fontPS, fontFile, command, file] = argv;
	const T = Application("Terminal");
	if (T.settingsSets.name().includes(name)) {
		const tab = T.doScript(" " + command);
		tab.currentSettings = T.settingsSets.byName(name);
	} else {
		writeProfile(argv);
		$.NSWorkspace.sharedWorkspace.openFile(file);
		for (let i = 0; i < 50; i++) { // Terminal imports it and opens its window
			delay(0.1);
			try { if (T.windows[0].selectedTab.currentSettings.name() === name) break; } catch (e) {}
		}
	}
	// Terminal drops a font it can't find yet, as a just-installed one can
	// be for a moment: set it until it sticks. (This mends a profile that
	// lost it, too.)
	for (let i = 0; i < 30; i++) {
		const s = T.settingsSets.byName(name);
		try {
			if (s.fontName() === fontPS && s.fontSize() === 13) break;
			s.fontName = fontPS;
			s.fontSize = 13;
		} catch (e) {}
		delay(0.1);
	}
	// Only clingon's own window: the front one may be the user's by now.
	try {
		if (T.windows[0].selectedTab.currentSettings.name() === name) T.windows[0].zoomed = true;
	} catch (e) {}
	T.activate();
}

function writeProfile(argv) {
	const [name, fontPS, fontFile, command, file, ...colors] = argv;
	$.CTFontManagerRegisterFontsForURL($.NSURL.fileURLWithPath(fontFile), 1, null); // just installed, maybe
	const font = $.NSFont.fontWithNameSize(fontPS, 13);
	const archive = o => $.NSKeyedArchiver.archivedDataWithRootObjectRequiringSecureCodingError(o, false, null);
	const color = hex => {
		const n = parseInt(hex.slice(1), 16);
		return archive($.NSColor.colorWithSRGBRedGreenBlueAlpha((n >> 16 & 255) / 255, (n >> 8 & 255) / 255, (n & 255) / 255, 1));
	};
	const p = $.NSMutableDictionary.dictionary;
	const set = (k, v) => p.setObjectForKey(v, k);
	const yes = $.NSNumber.numberWithBool(true), no = $.NSNumber.numberWithBool(false);
	set("name", name);
	set("type", "Window Settings");
	set("ProfileCurrentVersion", $.NSNumber.numberWithDouble(2.07));
	if (!font.isNil()) set("Font", archive(font));
	set("FontAntialias", yes);
	set("CursorType", $.NSNumber.numberWithInt(0)); // a block, blinking, like the menu's
	set("CursorBlink", yes);
	set("columnCount", $.NSNumber.numberWithInt(170));
	set("rowCount", $.NSNumber.numberWithInt(48));
	set("CommandString", command); // the window importing it runs clingon
	set("RunCommandAsShell", yes);
	set("shellExitAction", $.NSNumber.numberWithInt(1)); // close when clingon ends
	set("WindowTitle", "clingon");
	for (const k of ["ShowActiveProcessInTitle", "ShowActiveProcessArgumentsInTitle", "ShowCommandKeyInTitle",
		"ShowDimensionsInTitle", "ShowRepresentedURLInTitle", "ShowRepresentedURLPathInTitle",
		"ShowShellCommandInTitle", "ShowTTYNameInTitle", "ShowWindowSettingsNameInTitle"]) set(k, no);
	const keys = ["BackgroundColor", "TextColor", "TextBoldColor", "CursorColor", "SelectionColor"];
	for (const c of ["Black", "Red", "Green", "Yellow", "Blue", "Magenta", "Cyan", "White"]) keys.push("ANSI" + c + "Color");
	for (const c of ["Black", "Red", "Green", "Yellow", "Blue", "Magenta", "Cyan", "White"]) keys.push("ANSIBright" + c + "Color");
	keys.forEach((k, i) => set(k, color(colors[i])));
	if (!p.writeToFileAtomically(file, true)) throw new Error("couldn't write " + file);
}`

// itermScript opens an iTerm2 window with clingon's profile (from the
// dynamic profile file Run writes first). Argument: the command.
const itermScript = `on run argv
	tell application "iTerm"
		set w to create window with profile "clingon" command (item 1 of argv)
		try
			set zoomed of w to true
		end try
		activate
	end tell
end run`

// itermProfile is clingon's iTerm2 profile, in iTerm's dynamic profiles
// folder: iTerm adds it to its list, without touching any other profile.
// Written by hand, as encoding/json would add half a megabyte to clingon
// for this one file.
func itermProfile() []byte {
	var b strings.Builder
	field := func(key, value string) {
		b.WriteString(",\n      " + strconv.Quote(key) + ": " + value)
	}
	color := func(key, hex string) {
		var r, g, bl int
		_, _ = fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &bl)
		c := func(v int) string { return strconv.FormatFloat(float64(v)/255, 'g', -1, 64) }
		field(key, `{"Red Component": `+c(r)+`, "Green Component": `+c(g)+`, "Blue Component": `+c(bl)+`, "Color Space": "sRGB"}`)
	}
	b.WriteString("{\n  \"Profiles\": [\n    {\n      \"Name\": \"clingon\"")
	field("Guid", strconv.Quote("6c696e67-6f6e-4000-8000-636c696e676f"))
	field("Normal Font", strconv.Quote(fmt.Sprintf("%s %d", fontPS, fontSize)))
	field("ASCII Anti Aliased", "true")
	field("Use Non-ASCII Font", "false")
	field("Columns", strconv.Itoa(cols))
	field("Rows", strconv.Itoa(rows))
	color("Background Color", bg)
	color("Foreground Color", fg)
	color("Bold Color", bold)
	color("Cursor Color", cursor)
	color("Selection Color", selection)
	for i, c := range ansi {
		color(fmt.Sprintf("Ansi %d Color", i), c)
	}
	b.WriteString("\n    }\n  ]\n}\n")
	return []byte(b.String())
}
