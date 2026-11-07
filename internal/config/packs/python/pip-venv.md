# pip and venv
Make and use virtualenvs, install packages and requirements with pip, and save requirements

## Activate virtualenv
Switches this shell to the project's .venv, so python and pip use it.

```sh when=./.venv
. .venv/bin/activate
```

## Deactivate virtualenv
Switches this shell back to your system Python.

```sh
deactivate
```

## Install a package
Installs a package into the active Python. Outside a virtualenv, Homebrew's Python refuses.

```sh
python3 -m pip install <package>
```

- `<package>` Which package

## Install requirements
Installs every package a requirements file lists. Outside a virtualenv, Homebrew's Python refuses.

```sh when=./requirements*.txt
python3 -m pip install -r <file>
```

- `<file>` Which file

## Make a virtualenv
Creates a private Python environment in .venv and switches this shell to it.

```sh
python3 -m venv .venv && . .venv/bin/activate
```

## Outdated packages
Lists installed packages that have newer versions available.

```sh
python3 -m pip list --outdated
```

## Save requirements
> [!WARNING]
> Writes every installed package and its exact version to requirements.txt, replacing what's there.

```sh
python3 -m pip freeze > requirements.txt
```
