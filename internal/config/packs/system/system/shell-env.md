# Shell and env
Search history, find, set and unset env vars, change PATH, time a command and reload the shell

## Search shell history
Searches the commands you've run before in this shell.

```sh
fc -l 1 | grep -i -- "<text>" || echo "No matches."
```

- `<text>` Text

## Find env var
Shows environment variables whose name or value contains some text.

```sh
env | grep -i -- <name> || echo "No matches."
```

- `<name>` Name

## Set env var
Sets an environment variable for this shell and everything you run from it.

```sh
export <name>=<value>
```

- `<name>` Name
- `<value>` Value

## Unset env var
Removes an environment variable from this shell.

```sh
unset <name>
```

- `<name>` Name

## Add folder to PATH
Puts a folder first on your PATH for this shell, so its programs run by name.

```sh
export PATH=<dir>:"$PATH"
```

- `<dir>` Which folder
  - (x) `$PWD`

## Where is a command
Shows everything a command name could run: programs, aliases and functions.

```sh
type -a <name>
```

- `<name>` Name

## PATH (one per line)
Lists each folder your shell searches for commands, in order.

```sh
echo "$PATH" | tr ':' '\n'
```

## Time a command
Runs a command and reports how long it took.

```sh
time <command>
```

- `<command>` Command

## Reload shell
Restarts your shell in place so it picks up changes to your config files.

```sh
if [ -n "$CLINGON" ]; then exec clingon _shell; else exec "$SHELL" -l; fi
```
