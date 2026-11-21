"""Scouts clingon's own packs for the commands worth watching, and films each.

    uv run --with pillow python3 tools/films/film.py OUT_DIR [HOW_MANY] [BUDGET_MB]
    uv run --with pillow python3 tools/films/film.py media/films 20 1

Needs tmux, go, ffmpeg and gifsicle. From the repo's root.

A film is the whole of what clingon is for, in under ten seconds: find a
command by what it's called, fill in its blanks, and see it run in the shell
beside the menu. So the scout tries every command it safely can, the same
way (filter, pick, take each blank's first answer or a sensible word for
it, run), and rates what the shell shows afterwards: a good few lines, some
colour, no error, nothing wider than the pane. The best of them, no two
alike, are filmed.

It works as tools/hero does. clingon is built fresh into a temp folder and
run in a sandbox: a scratch HOME (none of your packs, settings, history or
shell config) and a scratch git repo with a little history in it, in a tmux
server of its own. Each frame is tmux's own record of the screen, drawn by
tools/hero/render.py in clingon's font and colours: no window, no screen
recorder, nobody's desktop. The AI's key is a dummy, there only so its strip
shows; nothing is ever sent with it. Only commands that read, or that change
the scratch repo alone, are tried (see SAFE), and a result with your user
name or machine's name in it is thrown away.

The GIF is made the way wurld.gg's are: a lossless master whose last frames
dissolve into its first, one palette and no dither, gifsicle -O3 squeezed
only as far as the budget needs, and the first frame interlaced so a page
has a rough still to show while the rest loads. It's named .gif.png so
GitHub plays it wherever it's shown; an MP4 comes out beside it.
"""

import json
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
sys.path.insert(0, os.path.join(ROOT, "tools", "hero"))
import render  # noqa: E402  (tools/hero/render.py: the screen, drawn)

# render.py reads the window's colours out of internal/window/window.go, which
# now names them from the palette rather than spelling them out: so a name it
# came back with is looked up in internal/palette here.
with open(os.path.join(ROOT, "internal", "palette", "palette.go")) as _f:
    _palette = dict(re.findall(r'^\t(\w+)\s+= (?:Colour\{\d+, )?"(#[0-9a-f]{6})"', _f.read(), re.M))
for _name in ("BG", "FG", "BOLD", "CURSOR"):
    _value = getattr(render, _name)
    if _value.startswith("palette."):
        setattr(render, _name, _palette[_value.split(".")[1]])

COLS, ROWS = 118, 32
SOCKET = "clingon-films"
FPS = 20
FADE = 8
PER_MODULE = 5
# The packs scouted, and what a command may start with to be tried: things
# that only read, and git, which here can only change the scratch repo.
PACKS = ["git", "files", "system"]
SAFE = ("git ", "ls", "find ", "du ", "wc ", "head ", "tail ", "cat ", "grep ", "date", "cal", "file ", "stat ", "pwd", "echo ", "tree", "sort ", "uniq ")
UNSAFE = ("push", "pull", "fetch", "clone", "remote", "clean", "reset --hard", "rm ", "sudo", "open ", "pbcopy", "curl", "ssh", "worktree", "bisect", "gc", "config --global", "rebase", "-i ", "--interactive", "commit --amend", "| less", "watch ", "tail -f", "git init")
# A word for a blank that offers none of its own, by the blank's name.
WORDS = {"name": "late-opening", "message": "Tidy the till", "branch": "late-opening", "file": "README.md", "path": "src", "dir": "src", "folder": "src", "pattern": "till", "text": "till", "query": "shop", "term": "open", "search": "open", "count": "5", "n": "5", "lines": "5", "commit": "HEAD~1", "ref": "HEAD~2", "tag": "v0.3", "author": "You", "since": "1 week ago", "ext": "go", "extension": "go", "glob": "*.go", "word": "till", "string": "till", "stash": "stash@{0}"}


def run(*args, **kw):
    return subprocess.run(args, check=True, capture_output=True, text=True, **kw).stdout


def tmux(*args):
    return run("tmux", "-L", SOCKET, *args)


class Sandbox:
    """A scratch home with a little shop of a repo in it, and clingon up in it."""

    def __init__(self, base, name):
        self.home = os.path.join(base, name, "home")
        self.work = os.path.join(self.home, "Code", "shop")
        os.makedirs(self.work)
        self.env = {
            "HOME": self.home, "PATH": os.path.join(base, "bin") + ":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
            "SHELL": "/bin/zsh", "TERM": "xterm-256color", "LANG": "en_US.UTF-8", "USER": "you", "LOGNAME": "you",
            "TMPDIR": os.environ.get("TMPDIR", "/tmp"), "GIT_CONFIG_GLOBAL": "/dev/null",
            "GIT_AUTHOR_NAME": "You", "GIT_AUTHOR_EMAIL": "you@example.com", "GIT_COMMITTER_NAME": "You", "GIT_COMMITTER_EMAIL": "you@example.com",
            "OPENROUTER_API_KEY": "dummy-not-a-key",
        }
        self.frames = []
        self._shop()
        subprocess.run(["tmux", "-L", SOCKET, "kill-server"], capture_output=True)
        subprocess.run(["tmux", "-L", SOCKET, "-f", "/dev/null", "new-session", "-d", "-s", "film", "-x", str(COLS), "-y", str(ROWS), "-c", self.work, "CLINGON_WINDOW=harness exec clingon"], check=True, env=self.env)
        time.sleep(3)  # the menu, the shell's first prompt, the logo drawn in

    def sh(self, command, date=None):
        env = dict(self.env)
        if date:
            env["GIT_AUTHOR_DATE"] = env["GIT_COMMITTER_DATE"] = date
        subprocess.run(command, shell=True, check=True, cwd=self.work, env=env, capture_output=True)

    def _shop(self):
        def write(path, text):
            full = os.path.join(self.work, path)
            os.makedirs(os.path.dirname(full), exist_ok=True)
            with open(full, "w") as f:
                f.write(text)

        day = lambda n: "2026-11-%02dT10:00:00" % n  # noqa: E731
        self.sh("git init -q -b main .")
        write("README.md", "# Shop\n\nA little shop.\n")
        write("src/main.go", 'package main\n\nfunc main() { println("open") }\n')
        self.sh("git add -A && git commit -q -m 'Open the shop'", day(2))
        write("src/till.go", "package main\n\nfunc till() int { return 0 }\n")
        self.sh("git add -A && git commit -q -m 'Add a till'", day(3))
        self.sh("git tag v0.1")
        write("src/stock.go", "package main\n\nvar stock = map[string]int{}\n")
        self.sh("git add -A && git commit -q -m 'Count the stock'", day(5))
        self.sh("git switch -q -c sunday-hours")
        write("README.md", "# Shop\n\nA little shop.\n\nOpen Sundays, ten to four.\n")
        self.sh("git commit -q -am 'Open on Sundays'", day(6))
        self.sh("git switch -q main")
        write("src/till.go", "package main\n\nfunc till() int { return 100 }\n")
        self.sh("git commit -q -am 'Start the till with a float'", day(8))
        self.sh("git merge -q --no-ff sunday-hours -m 'Merge sunday-hours'", day(9))
        self.sh("git tag v0.2")
        write("src/main.go", 'package main\n\nfunc main() { println("open for business") }\n')
        self.sh("git commit -q -am 'Say a bit more at the door'", day(11))
        # Something put by for later, and work in hand: one change staged, one not, one file new.
        write("src/stock.go", "package main\n\nvar stock = map[string]int{\"tea\": 12}\n")
        self.sh("git stash push -q -m 'Try a loyalty card'")
        write("src/till.go", "package main\n\nfunc till() int { return 150 }\n")
        self.sh("git add src/till.go")
        write("README.md", "# Shop\n\nA little shop.\n\nOpen Sundays, ten to four.\nOpen late on Thursdays.\n")
        write("src/receipts.go", "package main\n")
        self.sh("clingon pack add " + " ".join(PACKS))

    def key(self, *keys, beat=0.35):
        tmux("send-keys", "-t", "film", *keys)
        time.sleep(beat)

    def text(self):
        return tmux("capture-pane", "-p", "-t", "film")

    def shot(self, ms):
        """The screen as it is now, held that long."""
        ansi = tmux("capture-pane", "-p", "-e", "-t", "film")
        x, y, shown = tmux("display-message", "-p", "-t", "film", "#{cursor_x} #{cursor_y} #{cursor_flag}").split()
        self.frames.append((ansi, ms, (int(x), int(y)) if shown == "1" else None))

    def type(self, words, film):
        for letter in words:
            tmux("send-keys", "-t", "film", "-l", letter)
            time.sleep(0.12)
            if film:
                self.shot(90)

    def close(self):
        subprocess.run(["tmux", "-L", SOCKET, "kill-server"], capture_output=True)


def commands():
    """Every command in the scouted packs that's safe to try: its pack, name, template and blanks."""
    found = []
    for pack in PACKS:
        folder = os.path.join(ROOT, "packs", pack)
        for file in sorted(os.listdir(folder)):
            if file.startswith("_") or not file.endswith(".md"):
                continue
            text = open(os.path.join(folder, file)).read()
            for block in re.split(r"^## ", text, flags=re.M)[1:]:
                name = block.split("\n", 1)[0].strip()
                code = re.search(r"```sh\n(.*?)```", block, re.S)
                if not code or "[!WARNING]" in block or "[!CAUTION]" in block or "[!DANGER]" in block:
                    continue
                template = code.group(1).strip()
                if "\n" in template or not template.startswith(SAFE) or any(word in template for word in UNSAFE):
                    continue
                blanks = []
                for blank in re.finditer(r"^- `<([^>]+)>`[^\n]*\n((?:  - [^\n]*\n?)*)", block, re.M):
                    blanks.append((blank.group(1), bool(blank.group(2).strip())))
                found.append({"pack": pack, "module": file[:-3], "name": name, "template": template, "blanks": blanks})
    return found


def play(box, command, film):
    """Finds the command, fills in its blanks and runs it. False if it couldn't be done that way."""
    shot = box.shot if film else (lambda ms: None)
    shot(900)
    box.key("/")
    shot(300)
    box.type(command["name"].lower(), film)
    time.sleep(0.4)
    # It must be the first thing the filter finds, as that's what Down goes to.
    lines = box.text().split("\n")
    typed = next((index for index, line in enumerate(lines) if "❯ " + command["name"].lower()[:8] in line), None)
    first = None if typed is None else next((line for line in lines[typed + 1:] if " · " in line), None)
    if first is None or command["name"] + " · " not in first:
        return False
    shot(500)
    box.key("Down")
    shot(450)
    box.key("Enter", beat=0.5)
    for index, (blank, offered) in enumerate(command["blanks"]):
        if "Input %d of %d" % (index + 1, len(command["blanks"])) not in box.text():
            return False
        shot(700)
        if not offered:
            word = WORDS.get(blank.lower().replace("-", " ").split()[-1])
            if word is None:
                return False
            box.type(word, film)
            shot(350)
        box.key("Enter", beat=0.5)
    time.sleep(1.3)  # typed into the shell, and run
    shot(2600)
    return "Input " not in box.text()


def shell_of(text):
    """The shell's side of the screen: what's right of the menu."""
    lines = text.split("\n")
    divider = next((line.index("┬") for line in lines if "┬" in line), None)
    if divider is None:
        return []
    return [line[divider + 1:].rstrip(" │") for line in lines[3:-3]]


def rate(command, box):
    """How good a picture the shell makes once the command's run."""
    plain = shell_of(box.text())
    lines = [line for line in plain if line.strip()]
    shown = "\n".join(lines)
    me = (os.environ.get("USER", ""), socket.gethostname().split(".")[0])
    if any(word and word in shown for word in me):
        return None
    if re.search(r"fatal:|error:|not found|usage:|No such file|unknown option|illegal option", shown, re.I):
        return None
    out = max(0, len(lines) - 4)  # less the two prompts and the command typed
    if out < 2:
        return None
    ansi = tmux("capture-pane", "-p", "-e", "-t", "film")
    colours = len(set(re.findall(r"\x1b\[([0-9;]+)m", ansi)))
    width = COLS - 44
    wrapped = sum(1 for line in lines if len(line) >= width - 1)
    return round(min(out, 14) * 0.7 - max(0, out - 20) * 0.6 + min(colours, 14) * 0.5 + len(command["blanks"]) * 1.2 - wrapped * 1.5 - len(command["name"]) * 0.08, 2)


def encode(pictures, out, name, budget):
    """pictures: (PIL image, frames held). Writes NAME.gif.png and NAME.mp4; returns what they came to."""
    frames = tempfile.mkdtemp(prefix="clingon-film-")
    count = 0
    for image, held in pictures:
        first = os.path.join(frames, "%04d.png" % count)
        image.save(first)
        count += 1
        for _ in range(held - 1):
            shutil.copyfile(first, os.path.join(frames, "%04d.png" % count))
            count += 1
    ff = lambda *args: subprocess.run(["ffmpeg", "-v", "error", "-y", *args], check=True)  # noqa: E731
    master, palette, raw = (os.path.join(frames, file) for file in ("loop.mkv", "palette.png", "raw.gif"))
    even = "crop=trunc(iw/2)*2:trunc(ih/2)*2"
    ff("-framerate", str(FPS), "-i", os.path.join(frames, "%04d.png"), "-filter_complex",
       "[0]%s,split[a][b];[a]trim=start_frame=%d,setpts=PTS-STARTPTS[body];[b]trim=end_frame=%d,setpts=PTS-STARTPTS[head];[body][head]xfade=transition=fade:duration=%s:offset=%s"
       % (even, FADE, FADE, FADE / FPS, (count - 2 * FADE) / FPS), "-c:v", "ffv1", master)
    ff("-i", master, "-vf", "palettegen=max_colors=256:stats_mode=full", palette)
    ff("-i", master, "-i", palette, "-lavfi", "[0][1]paletteuse=dither=none:diff_mode=rectangle", "-loop", "0", raw)
    gif = os.path.join(out, name + ".gif.png")
    made = None
    for lossy in (0, 20, 40, 70, 100):  # as lightly squeezed as fits
        squeezed = os.path.join(frames, "squeezed.gif")
        subprocess.run(["gifsicle", "-O3", *(["--lossy=%d" % lossy] if lossy else []), "--loopcount=forever", raw, "-o", squeezed], check=True)
        # Only the first frame interlaced: a rough still at once, while the rest loads.
        subprocess.run(["gifsicle", squeezed, "--interlace", "#0", "--no-interlace", "#1-", "-o", gif], check=True)
        made = {"lossy": lossy, "mb": os.path.getsize(gif) / 1e6}
        if made["mb"] <= budget:
            break
    mp4 = os.path.join(out, name + ".mp4")
    ff("-i", master, "-vf", "format=yuv420p", "-c:v", "libx264", "-preset", "veryslow", "-crf", "16", "-tune", "animation", "-movflags", "+faststart", mp4)
    shutil.rmtree(frames)
    return {**made, "mp4_mb": os.path.getsize(mp4) / 1e6, "seconds": (count - FADE) / FPS}


def main():
    out = os.path.abspath(sys.argv[1])
    many = int(sys.argv[2]) if len(sys.argv) > 2 else 20
    budget = float(sys.argv[3]) if len(sys.argv) > 3 else 1.0
    os.makedirs(out, exist_ok=True)
    base = tempfile.mkdtemp(prefix="clingon-films.")
    os.makedirs(os.path.join(base, "bin"))
    run("go", "build", "-buildvcs=false", "-o", os.path.join(base, "bin", "clingon"), "./cmd/clingon", cwd=ROOT)
    try:
        # The scout: every safe command, tried once, and rated on what it shows.
        # (With SCOUTED=1, the last scout's ratings are used again: for when
        # it's only how a film's made that's changed.)
        rated = []
        again = os.path.join(out, "scout.json")
        if os.environ.get("SCOUTED") and os.path.exists(again):
            rated = [(entry.pop("rating"), entry) for entry in json.load(open(again))]
            for _, command in rated:
                command["blanks"] = [tuple(blank) for blank in command["blanks"]]
        for index, command in enumerate([] if rated else commands()):
            box = Sandbox(base, "scout-%03d" % index)
            try:
                rating = rate(command, box) if play(box, command, film=False) else None
            finally:
                box.close()
            print("%6s  %s > %s > %s" % ("-" if rating is None else rating, command["pack"], command["module"], command["name"]), flush=True)
            if rating is not None:
                rated.append((rating, command))
        rated.sort(key=lambda pair: -pair[0])
        with open(os.path.join(out, "scout.json"), "w") as f:  # every command that made a picture, best first
            json.dump([{"rating": rating, **command} for rating, command in rated], f, indent=2)
        # The best, filmed: no more than a handful from any one module, so
        # they're not all alike, and none that's still going when the film
        # ends (a pager, waiting for a key: the picture is the pager's then).
        listed, per, tried = [], {}, 0
        for rating, command in rated:
            key = (command["pack"], command["module"])
            if len(listed) >= many or per.get(key, 0) >= PER_MODULE:
                continue
            tried += 1
            box = Sandbox(base, "film-%03d" % tried)
            try:
                play(box, command, film=True)
                going = "is running in the shell" in box.text()
            finally:
                box.close()
            if going:
                continue
            per[key] = per.get(key, 0) + 1
            pictures = [(render.draw(ansi, COLS, ROWS, 1, cursor), max(1, round(ms * FPS / 1000))) for ansi, ms, cursor in box.frames]
            # (The first, again at the end: what the last frames dissolve into.)
            pictures.append((pictures[0][0], FADE))
            name = "%02d-%s-%s" % (len(listed) + 1, command["pack"], re.sub(r"[^a-z0-9]+", "-", command["name"].lower()).strip("-"))
            made = encode(pictures, out, name, budget)
            width, height = pictures[0][0].size
            listed.append({"file": name + ".gif.png", "width": width, "height": height, "rating": rating, **made, **command})
            print("%s.gif.png  %.2f MB  %d x %d, lossy %d, %.1f s  (mp4 %.2f MB)" % (name, made["mb"], width, height, made["lossy"], made["seconds"], made["mp4_mb"]), flush=True)
        with open(os.path.join(out, "films.json"), "w") as f:
            json.dump(listed, f, indent=2)
        with open(os.path.join(out, "index.html"), "w") as f:
            f.write('<!doctype html><meta charset="utf-8"><title>clingon: scouted films</title><style>body{margin:24px;background:#111;color:#ddd;font:14px/1.4 ui-monospace,Menlo,monospace}main{display:grid;grid-template-columns:repeat(auto-fill,minmax(480px,1fr));gap:20px}figure{margin:0}img{max-width:100%%;display:block}figcaption{padding:6px 2px;color:#aaa}b{color:#fff}</style><h1>clingon: %d scouted films</h1><main>%s</main>'
                    % (len(listed), "".join('<figure><img loading="lazy" src="%s"><figcaption><b>%s</b><br>%s &rsaquo; %s &rsaquo; %s - %.2f MB, %d x %d</figcaption></figure>' % (e["file"], e["file"], e["pack"], e["module"], e["name"], e["mb"], e["width"], e["height"]) for e in listed)))
    finally:
        shutil.rmtree(base, ignore_errors=True)


if __name__ == "__main__":
    main()
