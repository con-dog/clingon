# llm-harness

Watch an LLM use clingon the way a person would: it reads the screen and presses keys, nothing else.

```sh
cd tools/llm-harness
export OPENROUTER_API_KEY=...       # or keep it in the Keychain with `clingon ai key`
go run .                            # builds clingon from this checkout, then waits for you to watch
```

It opens a Terminal window to watch in, in clingon's own profile (font and colours), and starts once that window is attached. The screen is always `-width`×`-height` (150×42), whatever window watches, so every run's model sees the same thing and runs compare fairly. With `-open=false`, attach yourself: `tmux -L clingon-llm attach -r`. The line under clingon says what the model is doing and why. The harness's own terminal shows each step and the model's thinking.

The default task has six phases: **First look**, **Add a pack**, **Run commands**, **Recipe**, **Create a pack** (its own, in the pack form) and **Use your pack**. The model rates each phase as it ends, objective and subjective out of 10 with notes, then the whole run. Each run is saved to `runs/` (ratings, notes, actions per phase, tokens, cost, time, and why it stopped if it didn't finish), and `go run . -history` lines them all up, so you can see whether a change helped. `transcript.md` in the sandbox has every screen the model saw.

## How it works

- **Sandbox.** A temp folder holds a scratch `HOME`, a small git repo to work in, and a fresh build of clingon. Your `~/.clingon`, shell config and history are never read or written, and nor is the `clingon` binary in the repo.
- **Screen.** clingon runs in a tmux server of its own (`-L clingon-llm`, no config), sized to your watch window. The harness types through a hidden tmux client of its own, so your window can stay read-only. That client also hears tmux's `%output` events: after each key the harness waits until the screen has gone quiet (no polling), so the next key, and the guards, always act on the screen as it is. Then it sends the screen to the model as text, with the menu's cursor marked `▶` (or `▷` while the shell has focus).
- **Context.** A screen is a few thousand tokens. All but the latest few are left out of the chat, 16 at a time, so a long run never outgrows the model's context and prompt caching still works between trims.
- **Tools.** `press` (key names or single characters), `type` (text), `wait` and `finish`. Each takes a `why` for the watcher.
- **Guards.** The harness refuses to pick "Yes, run it" on risky commands (unless you pass `-allow-risky`). It never types the code for a ☠ command, and it refuses to type text that could reach outside the sandbox (`rm`, `sudo`, `kill`, `curl`, redirects, …).

## Flags

| flag | default | |
|---|---|---|
| `-task "…"` | first-run tour | what to do; the default adds the git pack, runs commands, builds and runs a recipe |
| `-model` | `anthropic/claude-opus-5.5` | any OpenRouter model, e.g. `anthropic/claude-sonnet-5.5` or `deepseek/deepseek-v4-pro`: it goes through OpenRouter's chat API, the same client clingon's own AI uses (`internal/llm`) |
| `-effort` | `medium` | reasoning effort, for models that reason: `low` is faster, `high` is more careful |
| `-delay` | `900ms` | pause between narrating and acting, so you can follow |
| `-max-steps` | `250` | |
| `-width`, `-height` | `150`, `42` | the screen's size |
| `-history` | | print every saved run's ratings, phase by phase, and stop |
| `-runs dir` | `runs` | where runs are saved |
| `-open=false` | | don't open a window to watch in |
| `-wait=false` | | start without waiting for a viewer |
| `-keep=false` | | close the session at the end (by default it stays open to look around) |
| `-clingon path` | | test a binary you built instead |
| `-screen` | | open the sandbox, print what the model would see, stop (no API calls) |
| `-peek` | | print the open session's screen as the model sees it |

End a session with `tmux -L clingon-llm kill-server`. Starting a new run ends the last one.
