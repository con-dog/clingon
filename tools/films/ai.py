"""Films clingon's AI doing a task, with whoever answers the relay as the model.

    uv run --with pillow python3 tools/films/ai.py OUT_DIR NAME MODEL "the task"
    uv run --with pillow python3 tools/films/ai.py media/films ai-pretty-log anthropic/claude-fable-5.2 "Show me the last ten commits as a graph"

clingon's AI sends the menu's screen to a model and presses the keys the
model answers with. Here the model is whoever is at the relay: each request
clingon makes is written to RELAY/req-N.json and held until RELAY/resp-N.json
is there, which is the move to make ({"name": "run", "arguments": {...}}, one
of clingon's own AI tools). With RELAY set to a folder that already has the
answers from a run before, the same moves are made again without waiting. So an assistant in a terminal, or a person, can
sit in the model's seat and drive, and the film shows clingon's AI as it is:
its strip, its steps, the frame while it drives. Nothing goes online.

clingon reaches its model at one fixed address, so the clingon filmed is
built from a copy of this checkout with that one line pointing at the relay
instead. Nothing in the repo is changed, and the sandbox is film.py's: a
scratch HOME and a scratch repo.

The screen is photographed many times a second all the way through. The
time the model takes to answer is cut down to a moment of the spinner, and
no screen is held long, so a task of a dozen moves is half a minute at
most: these films are a tenth the bytes of the others, and can afford it.
"""

import http.server
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import threading
import time

import film

film.SOCKET = "clingon-ai"
PORT = 47391
SAMPLE = 0.07  # seconds between photographs of the screen
THINK = 0.45   # how much of each wait for the model is kept
HOLD = 1.3     # the longest any one screen is held, but the last
LAST = 2.6
LONGEST = 30.0


def main():
    out, name, model, task = os.path.abspath(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4]
    os.makedirs(out, exist_ok=True)
    base = tempfile.mkdtemp(prefix="clingon-ai.")
    relay = os.environ.get("RELAY") or os.path.join(base, "relay")
    os.makedirs(relay, exist_ok=True)
    print("relay: %s" % relay, flush=True)

    # clingon, built from a copy with its model's address pointed here.
    copy = os.path.join(base, "src")
    files = subprocess.run(["git", "ls-files"], cwd=film.ROOT, check=True, capture_output=True, text=True).stdout.split("\n")
    for file in filter(None, files):
        if file.startswith("media/"):
            continue
        os.makedirs(os.path.dirname(os.path.join(copy, file)), exist_ok=True)
        shutil.copyfile(os.path.join(film.ROOT, file), os.path.join(copy, file))
    client = os.path.join(copy, "internal", "llm", "openrouter.go")
    source = open(client).read()
    address = 'const Endpoint = "https://openrouter.ai/api/v1/chat/completions"'
    if address not in source:
        sys.exit("internal/llm/openrouter.go no longer has: " + address)
    with open(client, "w") as f:
        f.write(source.replace(address, 'const Endpoint = "http://127.0.0.1:%d/"' % PORT))
    os.makedirs(os.path.join(base, "bin"))
    subprocess.run(["go", "build", "-buildvcs=false", "-o", os.path.join(base, "bin", "clingon"), "./cmd/clingon"], cwd=copy, check=True)

    state = {"waiting": False, "asked": 0, "done": False}

    class Relay(http.server.BaseHTTPRequestHandler):
        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            state["asked"] += 1
            turn = state["asked"]
            with open(os.path.join(relay, "req-%d.json" % turn), "wb") as f:
                f.write(body)
            state["waiting"] = True
            answer = os.path.join(relay, "resp-%d.json" % turn)
            while not os.path.exists(answer):
                time.sleep(0.05)
            time.sleep(0.05)
            move = json.load(open(answer))
            state["waiting"] = False
            if move["name"] == "done":
                state["done"] = True
            reply = {"choices": [{"message": {"role": "assistant", "content": "", "tool_calls": [{"id": "call_%d" % turn, "type": "function", "function": {"name": move["name"], "arguments": json.dumps(move["arguments"])}}]}, "finish_reason": "tool_calls"}],
                     "usage": {"prompt_tokens": 0, "completion_tokens": 0}}
            data = json.dumps(reply).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *args):
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", PORT), Relay)
    threading.Thread(target=server.serve_forever, daemon=True).start()

    box = film.Sandbox(base, "ai", model=model)
    samples = []  # (seconds in, the screen, its cursor, whether the model was being waited for)
    stop = threading.Event()
    began = time.time()

    def watch():
        while not stop.is_set():
            try:
                ansi = film.tmux("capture-pane", "-p", "-e", "-t", "film")
                x, y, shown = film.tmux("display-message", "-p", "-t", "film", "#{cursor_x} #{cursor_y} #{cursor_flag}").split()
            except Exception:
                break
            samples.append((time.time() - began, ansi, (int(x), int(y)) if shown == "1" else None, state["waiting"]))
            time.sleep(SAMPLE)

    threading.Thread(target=watch, daemon=True).start()
    try:
        # Whoever's at the keyboard gives the task: found by its name, and typed in.
        time.sleep(0.8)
        box.key("/")
        box.type("give a task", film=False)
        time.sleep(0.4)
        box.key("Down")
        box.key("Enter", beat=0.6)
        box.type(task, film=False)
        time.sleep(0.5)
        box.key("Enter")
        # And the model does it, a move at a time, for as long as it takes.
        limit = time.time() + float(os.environ.get("LIMIT", "1800"))
        while not state["done"] and time.time() < limit:
            time.sleep(0.2)
        time.sleep(3.5)
    finally:
        stop.set()
        time.sleep(0.2)
        with open(os.path.join(relay, "screen.txt"), "w") as f:
            f.write(box.text())
        box.close()
        server.shutdown()

    # The film: the waits for the model cut down to a moment, the same screen
    # twice running made one, no screen held too long, and the lot paced to fit.
    kept, since = [], None
    for index, (at, ansi, cursor, waiting) in enumerate(samples):
        if waiting:
            since = at if since is None else since
            if at - since > THINK:
                continue
        else:
            since = None
        kept.append((ansi, cursor, SAMPLE))
    shots = []
    for ansi, cursor, held in kept:
        if shots and shots[-1][0] == ansi and shots[-1][1] == cursor:
            shots[-1][2] += held
        else:
            shots.append([ansi, cursor, held])
    for shot in shots[:-1]:
        shot[2] = min(shot[2], HOLD)
    shots[-1][2] = LAST
    total = sum(shot[2] for shot in shots)
    pace = min(1.0, LONGEST / total)
    # (Paced by carrying what's left over from one screen to the next: where
    # the whole has to go faster than a frame a screen, a letter typed is
    # skipped now and then, in place of every one held a frame too long.)
    pictures, owed = [], 0.0
    for index, (ansi, cursor, held) in enumerate(shots):
        owed += held * pace * film.FPS
        frames = int(owed) if index < len(shots) - 1 else max(1, round(owed))
        if frames < 1:
            continue
        owed -= frames
        pictures.append((film.framed(film.render.draw(ansi, film.COLS, film.ROWS, 1, cursor)), frames))
    pictures.append((pictures[0][0], film.FADE))
    made = film.encode(pictures, out, name, float(os.environ.get("BUDGET", "1")))
    print("%s.gif.png  %.2f MB  lossy %d, %.1f s (%.1f s of screen, %d moves)  (mp4 %.2f MB)" % (name, made["mb"], made["lossy"], made["seconds"], total, state["asked"], made["mp4_mb"]), flush=True)
    # Listed with the rest, in films.json and on the page.
    listed = []
    if os.path.exists(os.path.join(out, "films.json")):
        listed = [entry for entry in json.load(open(os.path.join(out, "films.json"))) if entry["file"] != name + ".gif.png"]
    listed.append({"file": name + ".gif.png", "width": film.CANVAS[0], "height": film.CANVAS[1], "task": "AI (%s): %s" % (model.split("/")[-1], task), "moves": state["asked"], **made})
    film.index(out, listed)
    if not os.environ.get("RELAY"):
        shutil.rmtree(base, ignore_errors=True)


if __name__ == "__main__":
    main()
