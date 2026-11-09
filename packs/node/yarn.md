---
needs: yarn
when: yarn.lock
---
# Yarn
Install, add and remove packages, run scripts and tests, and see why one is installed with yarn

## Add a package
Adds a package to package.json and installs it.

```sh
yarn add <flags> <package>
```

- `<package>` Which package
- `<flags>` Extra options
  - [ ] `-D` dev dependency

## Install dependencies
Installs every dependency in package.json.

```sh
yarn install
```

## Remove a package
> [!WARNING]
> Takes a dependency out of package.json and node_modules.

```sh
yarn remove <package>
```

- `<package>` Which package

## Run a script
Runs one of the scripts in package.json.

```sh
yarn run <script>
```

- `<script>` Which script

## Run tests
Runs the project's test script.

```sh
yarn test
```

## Why is this installed
Shows which of your dependencies pulls a package in.

```sh
yarn why <package>
```

- `<package>` Which package
