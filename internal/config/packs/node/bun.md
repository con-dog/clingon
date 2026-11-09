---
needs: bun
when: bun.lock, bun.lockb
---
# Bun
Install, add, remove and update packages, run scripts and tests with bun

## Add a package
Adds a package to package.json and installs it.

```sh
bun add <flags> <package>
```

- `<package>` Which package
- `<flags>` Extra options
  - [ ] `-d` dev dependency

## Clean install (CI)
Installs exactly what bun.lock lists, and fails if package.json has moved on from it. Best for CI.

```sh
bun install --frozen-lockfile
```

## Install dependencies
Installs every dependency in package.json.

```sh
bun install
```

## Outdated packages
Lists dependencies with newer versions available.

```sh
bun outdated
```

## Remove a package
> [!WARNING]
> Takes a dependency out of package.json and node_modules.

```sh
bun remove <package>
```

- `<package>` Which package

## Run a script
Runs one of the scripts in package.json.

```sh
bun run <script>
```

- `<script>` Which script

## Run tests
Runs the tests with bun's test runner.

```sh
bun test
```

## Update dependencies
> [!WARNING]
> Updates dependencies to the newest versions package.json allows.

```sh
bun update
```
