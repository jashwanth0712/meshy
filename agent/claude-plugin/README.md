# Meshy plugin for Claude Code

This plugin gives Claude Code its own Meshy identity: an ENSv2 name with World ID-backed approval. With it, Claude can run commands on granted resources inside a folder sandbox.

## Install
1. Build the CLI and put it on your PATH, or point the `MESHY_BIN` setting in `.mcp.json` at it:
   ```bash
   cd meshy && go build -o /usr/local/bin/meshy ./cmd/meshy
   ```
2. Load the plugin:
   ```bash
   claude --plugin-dir agent/claude-plugin
   ```
3. In Claude Code, run `/meshy:join <invite-link>`, using the link your org admin sent you.

## What you get
- **MCP tools:** `meshy_join`, `meshy_whoami`, `meshy_grants`, `meshy_ssh`
- **Skill:** `meshy`. It covers how grants work, and what to do on `MESHY_BLOCKED`, `MESHY_REVOKED` and `MESHY_EXPIRED`.
- **Commands:** `/meshy:join`, `/meshy:status`

The MCP server (`mcp/server.mjs`) has no dependencies. It speaks MCP over stdio and calls the `meshy` CLI.
