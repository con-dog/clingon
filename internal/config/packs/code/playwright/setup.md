# Setup
Start a project, install browsers, record a test and check the version

## Install browsers
> [!WARNING]
> Downloads the browsers Playwright uses.

```sh
npx playwright install <browsers>
```

- `<browsers>` Browsers
  - [ ] `chromium`
  - [ ] `firefox`
  - [ ] `webkit`

## Record a test
Opens a browser and writes test code as you click around.

```sh
npx playwright codegen <url>
```

- `<url>` Url

## Start a project
Adds Playwright to this project: the config, an example test and the browsers.

```sh needs=npm
npm init playwright@latest
```

## Version
Shows which version of Playwright this project uses.

```sh when=playwright.config.ts,playwright.config.js,playwright.config.mts,playwright.config.mjs
npx playwright --version
```
