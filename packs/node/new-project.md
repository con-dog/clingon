# New project
Start a project with npm, pnpm or bun, or run a package without installing it

## Run a package without installing
Downloads a package's command if needed and runs it.

```sh needs=npx
npx <package>
```

- `<package>` Which package

## Start a bun project
Creates package.json, tsconfig.json and an index.ts here, set up for bun.

```sh needs=bun
bun init -y
```

## Start a pnpm project
Creates a package.json and lockfile here, for pnpm.

```sh needs=pnpm
pnpm init && pnpm install
```

## Start an npm project
Creates a package.json and lockfile here, with the usual defaults.

```sh needs=npm
npm init -y && npm install
```
