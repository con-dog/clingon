---
needs: pnpm
when: pnpm-lock.yaml
---
# pnpm
Install, add, remove, update and audit packages, and run scripts and tests with pnpm

## Add a package
Adds a package to package.json and installs it.

```sh
pnpm add <flags> <package>
```

- `<package>` Which package
- `<flags>` Extra options
  - [ ] `-D` dev dependency

## Clean install (CI)
Installs exactly what pnpm-lock.yaml lists, and fails if package.json has moved on from it. Best for CI.

```sh
pnpm install --frozen-lockfile
```

## Install dependencies
Installs every dependency in package.json.

```sh
pnpm install
```

## Outdated packages
Lists dependencies with newer versions available.

```sh
pnpm outdated
```

## Remove a package
> [!WARNING]
> Takes a dependency out of package.json and node_modules.

```sh
pnpm remove <package>
```

- `<package>` Which package

## Run a package without installing
Downloads a package's command if needed and runs it.

```sh
pnpm dlx <package>
```

- `<package>` Which package

## Run a script
Runs one of the scripts in package.json.

```sh
pnpm run <script>
```

- `<script>` Which script

## Run tests
Runs the project's test script.

```sh
pnpm test
```

## Security audit
Checks your dependencies for known security problems.

```sh
pnpm audit
```

## Top level dependencies
Lists the project's direct dependencies and the versions installed.

```sh
pnpm list --depth=0
```

## Update dependencies
> [!WARNING]
> Updates dependencies to the newest versions package.json allows.

```sh
pnpm update
```
