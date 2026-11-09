# Sessions
Start or resume Codex, run a one-off task, or start with a chosen model or sandbox

## Start Codex
Starts an interactive Codex session in this folder.

```sh
codex
```

## Resume last session
Picks up your most recent Codex session in this folder where you left off.

```sh
codex resume --last
```

## Resume a session
Lets you choose a past Codex session to pick back up.

```sh
codex resume
```

## Run a single task
> [!WARNING]
> Gives Codex a single task to work through without a session, printing its progress and final answer.

```sh
codex exec <flags> "<prompt>"
```

- `<prompt>` Prompt
- `<flags>` Extra options
  - [ ] `--sandbox workspace-write` let it edit files in this folder
  - [ ] `--json` print events as JSON lines
  - [ ] `--skip-git-repo-check` run outside a Git repo

## Start with a model
Starts an interactive Codex session using a model other than your default.

```sh
codex --model <model>
```

- `<model>` Model

## Start with a sandbox
> [!WARNING]
> Starts an interactive Codex session that can only touch what the sandbox allows.

```sh
codex --sandbox <sandbox>
```

- `<sandbox>` Sandbox
  - ( ) `read-only` read files only
  - ( ) `workspace-write` edit files in this folder
  - ( ) `danger-full-access` no sandbox at all
