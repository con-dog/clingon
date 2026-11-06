---
when: playwright.config.ts, playwright.config.js, playwright.config.mts, playwright.config.mjs
---
# Debug
Debug a test file, use UI mode, record traces and view traces and reports

## Debug a file
Runs a file's tests one step at a time in the Playwright Inspector.

```sh
npx playwright test --debug <file>
```

- `<file>` Which file

## Record traces
Runs the tests and records a trace of each, with screenshots, network and console, to open from the report.

```sh
npx playwright test --trace on
```

## Show a trace
Opens a recorded trace in the trace viewer, to step through what a test did.

```sh
npx playwright show-trace <file>
```

- `<file>` Which file

## Show report
Opens the HTML report from the last run in your browser.

```sh
npx playwright show-report
```

## UI mode
Opens a window to pick tests, run them, watch them and step back through each action.

```sh
npx playwright test --ui
```
