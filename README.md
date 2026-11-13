# clingon

**Stop googling commands. Pick one, fill in the blanks, run it. Or let an AI do it.**

```sh
brew install con-dog/tap/clingon
clingon
```

![clingon's AI given a task: it makes a pack, builds a recipe and runs it from the menu, in a real shell beside it](media/ai-driver.gif.png)

Needs macOS, with zsh or bash. Opens in its own window in Terminal or iTerm2 (Ghostty, kitty and Alacritty untested).

- **An AI drives it for you.** Give it a task and watch it use the menu as you would.
- **It's your real shell.** Your config, aliases and environment. clingon only types the command in.
- **It checks before risky commands.** ⚠ commands ask No/Yes. ☠ commands can't be undone, so you type a short code first.
- **Commands are just Markdown.** A pack is a folder of `.md` files that reads as docs on GitHub and runs in clingon. [How packs work →](docs/packs.md)
- **Chain commands into recipes.** Press `1`–`9` to line commands up, then run them in order. It stops if one fails. [How recipes work →](docs/recipes.md)

**20 packs built in**, from `git` and `docker` to `aws` and `postgres`.

## Why clingon?

<table>
<tr><th></th><th width="100%" align="center">clingon</th><th align="center">navi</th><th align="center">tldr</th><th align="center">fzf</th></tr>
<tr><td nowrap>Finds commands you've never run</td><td align="center">✓</td><td align="center">✓</td><td align="center">✓</td><td align="center">–</td></tr>
<tr><td nowrap>Fills in the blanks for you</td><td align="center">✓</td><td align="center">✓</td><td align="center">–</td><td align="center">–</td></tr>
<tr><td nowrap>Runs it in your shell</td><td align="center">✓</td><td align="center">✓</td><td align="center">–</td><td align="center">✓</td></tr>
<tr><td nowrap>Runs plain Markdown</td><td align="center">✓</td><td align="center">–</td><td align="center">–</td><td align="center">–</td></tr>
<tr><td nowrap>Asks before risky commands</td><td align="center">✓</td><td align="center">–</td><td align="center">–</td><td align="center">–</td></tr>
<tr><td nowrap>Chains commands into recipes</td><td align="center">✓</td><td align="center">–</td><td align="center">–</td><td align="center">–</td></tr>
<tr><td nowrap>Has an AI that does it for you</td><td align="center">✓</td><td align="center">–</td><td align="center">–</td><td align="center">–</td></tr>
<tr><td nowrap>Works in any terminal</td><td align="center">–</td><td align="center">✓</td><td align="center">✓</td><td align="center">✓</td></tr>
<tr><td nowrap>Runs on Linux</td><td align="center">–</td><td align="center">✓</td><td align="center">✓</td><td align="center">✓</td></tr>
</table>

## Good to know

- **It never changes your setup.** clingon reads your shell config and never writes to it.
- **What it adds:** a font, and a "clingon" profile in Terminal or iTerm2. macOS may ask once to let clingon control those apps, only to open and style the window.
- **Separate history.** Unless your config sets `HISTFILE`, zsh inside clingon keeps its own history.
- **Its files** are in `~/.clingon`: your packs and recipes, and its own files in `config`. Set `$CLINGON_CONFIG` to move it.
- **Something wrong?** **Clingon › General** in the menu checks your setup, shows the last crash and updates clingon.
- **Zero telemetry.** clingon collects nothing. It goes online only to check for a new release, once a day.
- **The AI** needs an OpenRouter key (add it under **Clingon › AI**, or set `$OPENROUTER_API_KEY`) and runs only when you give it a task. The task, the menu and the shell's screen then go to OpenRouter and the model's maker.

## Removing clingon

```sh
brew uninstall clingon
rm -rf ~/.clingon
rm -f ~/Library/Fonts/ClingonMono-*.otf
rm -f ~/Library/Application\ Support/iTerm2/DynamicProfiles/clingon.json # iTerm2
rm -rf ~/Library/Caches/clingon # Terminal
```

If you use Terminal, also delete any profiles named "clingon" in Settings › Profiles.

## Hacking on clingon

```sh
go build -o clingon ./cmd/clingon && ./clingon
go test ./...
```

## License

MIT
