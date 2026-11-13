#!/bin/sh
# Makes media/hero.png and media/hero.gif: clingon doing a little real work,
# in a sandbox, drawn by render.py from tmux's record of its screen.
#
#   tools/hero/make.sh            # from the repo's root; needs tmux, go and uv
#
# Nothing of yours is in the pictures or touched to make them: clingon is
# built fresh into a temp folder, with a scratch HOME (so none of your packs,
# settings, history or shell config) and a scratch git repo to work in, in a
# tmux server of its own (-L clingon-hero). The AI's key is a dummy, there
# only so its strip shows: nothing is ever sent with it.
set -eu
cd "$(dirname "$0")/../.."
COLS=${COLS:-150} ROWS=${ROWS:-42}
base=$(mktemp -d "${TMPDIR:-/tmp}/clingon-hero.XXXXXX")
home=$base/home work=$base/home/Code/shop frames=${FRAMES:-$base/frames} # FRAMES=dir keeps the captures, to look at
mkdir -p "$work" "$frames" "$base/bin"
: >"$frames/frames.txt"
trap 'tmux -L clingon-hero kill-server 2>/dev/null; rm -rf "$base"' EXIT

go build -buildvcs=false -o "$base/bin/clingon" ./cmd/clingon
sandbox() {
	env -i HOME="$home" PATH="$base/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" SHELL=/bin/zsh \
		TERM=xterm-256color LANG=en_US.UTF-8 USER="${USER:-you}" LOGNAME="${USER:-you}" TMPDIR="${TMPDIR:-/tmp}" \
		GIT_CONFIG_GLOBAL=/dev/null GIT_AUTHOR_NAME=You GIT_AUTHOR_EMAIL=you@example.com \
		GIT_COMMITTER_NAME=You GIT_COMMITTER_EMAIL=you@example.com OPENROUTER_API_KEY=dummy-not-a-key "$@"
}
(
	cd "$work"
	sandbox git init -q -b main .
	printf '# Shop\n\nA little shop.\n' >README.md
	mkdir -p src && printf 'package main\n\nfunc main() { println("open") }\n' >src/main.go
	sandbox git add -A && sandbox git commit -q -m "Open the shop"
	printf '\nOpen nine to five.\n' >>README.md
	printf 'package main\n' >src/till.go
	sandbox clingon pack add git github docker >/dev/null
)

tmux -L clingon-hero kill-server 2>/dev/null || true
sandbox tmux -L clingon-hero -f /dev/null new-session -d -s hero -x "$COLS" -y "$ROWS" -c "$work" "CLINGON_WINDOW=harness exec clingon"

n=0
# shot MILLISECONDS [hero]: the screen as it is now, held that long.
shot() {
	n=$((n + 1))
	name=$(printf '%03d.ansi' "$n")
	tmux -L clingon-hero capture-pane -p -e -t hero >"$frames/$name"
	echo "$name $1 $(tmux -L clingon-hero display-message -p -t hero '#{cursor_x} #{cursor_y} #{cursor_flag}') ${2:-}" >>"$frames/frames.txt"
}
key() { tmux -L clingon-hero send-keys -t hero "$@"; sleep 0.35; }
type() {
	text=$1
	while [ -n "$text" ]; do
		rest=${text#?}
		tmux -L clingon-hero send-keys -t hero -l "${text%"$rest"}"
		text=$rest
		sleep 0.12
		shot 90
	done
}

sleep 3 # the menu, the shell's first prompt, the logo drawn in
shot 1400
key Right && shot 900          # into Git, the first pack
key Down && shot 250
key Down && shot 250
key Down && shot 700
key / && shot 300              # find a command by what it's called
type "new br"
shot 600
key Down && shot 700           # to the results
key Enter && shot 600          # New branch: it asks for the name
type "late-opening"
shot 500
key Enter && sleep 1.2 && shot 1600   # typed into the shell, and run
key / && shot 250              # back in Git's list: the next one
type "status"
key Down && shot 400
key Enter && sleep 1.2 && shot 1800
key / && shot 250
type "log"
key Down && shot 2400 hero     # the picture: a search, its command, and the shell beside it
key Tab && shot 1500           # to the shell: the lit pane is where you are
key BTab && shot 1500          # and back

mkdir -p media
uv run -q --with pillow python3 tools/hero/render.py "$frames" media/hero.png media/hero.gif
