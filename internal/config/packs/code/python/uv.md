---
needs: uv
---
# uv
Start a project, add packages, sync the environment and run things with uv

## Add a package
Adds a dependency to pyproject.toml and installs it.

```sh when=pyproject.toml
uv add <package>
```

- `<package>` Which package

## Run in the project
Runs a command inside the project's environment, syncing it first.

```sh when=pyproject.toml
uv run <command>
```

- `<command>` Command

## Start a project
Creates a pyproject.toml and starter files in this folder.

```sh
uv init
```

## Sync environment
> [!WARNING]
> Makes .venv match the lockfile exactly, installing and removing packages as needed.

```sh when=pyproject.toml
uv sync
```
