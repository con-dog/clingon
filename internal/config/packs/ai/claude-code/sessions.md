# Sessions
Start, continue or resume Claude Code, or ask it a one-off question

## Start Claude Code
Starts an interactive Claude Code session in this folder.

```sh
claude
```

## Continue last conversation
Picks up your most recent Claude Code conversation in this folder where you left off.

```sh
claude --continue
```

## Resume a conversation
Lets you choose a past Claude Code conversation to pick back up.

```sh
claude --resume
```

## Ask a single question
Asks Claude a single question and prints the answer, without starting a session. It skips the check that you trust this folder.

```sh
claude -p "<prompt>"
```

- `<prompt>` Prompt
