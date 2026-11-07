---
when: playwright.config.ts, playwright.config.js, playwright.config.mts, playwright.config.mjs
---
# Run tests
Run Playwright tests by file, title or project, headed, failed or changed only, and update snapshots

## Changed files only
Runs only the test files changed since the last commit, and those that import changed files.

```sh
npx playwright test --only-changed
```

## Headed
Runs the tests with the browser windows showing.

```sh
npx playwright test --headed
```

## Last failed
Runs again only the tests that failed last time.

```sh
npx playwright test --last-failed
```

## One project
Runs the tests for one project from the config, like one browser or device.

```sh
npx playwright test --project <project>
```

- `<project>` Which project

## Repeat to find flaky tests
Runs every test several times over, to catch ones that only fail sometimes.

```sh
npx playwright test --repeat-each <times>
```

- `<times>` Times
  - (x) `10`

## Test
Runs every test, headless, in every project in the config.

```sh
npx playwright test
```

## Test a file
Runs the tests in one file.

```sh
npx playwright test <file>
```

- `<file>` Which file

## Tests matching a title
Runs only the tests whose title matches.

```sh
npx playwright test -g "<title>"
```

- `<title>` Title

## Update snapshots
> [!WARNING]
> Runs the tests and replaces the saved screenshots and snapshots with what they show now.

```sh
npx playwright test --update-snapshots
```
