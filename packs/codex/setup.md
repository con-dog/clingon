# Setup
Log in and out of Codex, and list, add or remove MCP servers

## Log in
Signs Codex in with your ChatGPT account in the browser.

```sh
codex login
```

## Login status
Shows whether Codex is signed in, and how.

```sh
codex login status
```

## Log out
> [!WARNING]
> Removes Codex's saved credentials from this machine.

```sh
codex logout
```

## MCP servers
Lists the MCP servers Codex is set up to use.

```sh
codex mcp list
```

## Add MCP server
> [!WARNING]
> Adds a local MCP server that Codex starts when it needs it.

```sh
codex mcp add <name> -- <command>
```

- `<name>` Name
- `<command>` Command

## Remove MCP server
> [!WARNING]
> Removes an MCP server from Codex's config.

```sh
codex mcp remove <name>
```

- `<name>` Name
