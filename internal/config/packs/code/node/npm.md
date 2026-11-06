---
needs: npm
when: package-lock.json, npm-shrinkwrap.json
---
# npm
Install, add, remove, update and audit packages, and run scripts and tests with npm

## Add a package
Adds a package to package.json and installs it.

```sh
npm install <flags> <package>
```

- `<package>` Which package
- `<flags>` Extra options
  - [ ] `-D` dev dependency

## Clean install (CI)
> [!WARNING]
> Deletes node_modules and installs exactly what package-lock.json lists. Best for CI and clean checkouts.

```sh
npm ci
```

## Clean reinstall
> [!WARNING]
> Deletes node_modules and installs every dependency again from scratch.

```sh
rm -rf node_modules && npm install
```

## Install dependencies
Installs every dependency in package.json.

```sh
npm install
```

## Outdated packages
Lists dependencies with newer versions available.

```sh
npm outdated
```

## Remove a package
> [!WARNING]
> Takes a dependency out of package.json and node_modules.

```sh
npm uninstall <package>
```

- `<package>` Which package

## Run a script
Runs one of the scripts in package.json.

```sh
npm run <script>
```

- `<script>` Which script

## Run tests
Runs the project's test script.

```sh
npm test
```

## Security audit
Checks your dependencies for known security problems.

```sh
npm audit
```

## Top level dependencies
Lists the project's direct dependencies and the versions installed.

```sh
npm ls --depth=0
```

## Update dependencies
> [!WARNING]
> Updates dependencies to the newest versions package.json allows.

```sh
npm update
```
