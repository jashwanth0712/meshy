---
name: meshy
description: Use when the user asks you to access, inspect, read or change files or run commands on a team server, database host, or any machine named like <name>.<network>.<org>.eth, or when they give you a Meshy invite link. Covers joining a Meshy org, checking grants, running sandboxed commands over meshy_ssh, and how to react to MESHY_BLOCKED / MESHY_REVOKED / MESHY_EXPIRED.
---

# Meshy: ENS-controlled access to infrastructure

Meshy gives you, the agent, **your own identity** (`<you>.agents.<org>.eth`) and **time-limited, folder-scoped grants** on resources. ENSv2 on Ethereum is the source of truth:

- A grant is the ENS subname `<you>.<resource>`, e.g. `claude-1.db1.prod.acme.eth`. It expires (default 1h) and admins can revoke it at any time.
- Each session on a resource is sandboxed to the granted folders, either read-only (`ro`) or read-write (`rw`). Everything else is invisible or refused.
- Every file you read or write, and every blocked attempt, is recorded on Arkiv. Assume your human can see exactly what you did.

## Joining

When the user gives you an invite link (`.../approve?org=...&agent=...`):

1. Call `meshy_join` with the link.
2. Show the returned approval URL to the user, and ask them to open it, sign in, complete World ID, and accept.
3. Call `meshy_whoami` until it reports `active (human-backed)`.

Never try to approve yourself, and never ask the user for their keys or wallet.

## Working on a resource

1. `meshy_grants` with the resource name tells you whether you have a grant and how long it has left.
2. `meshy_ssh` with `resource` and `command` runs the command inside your sandbox. Prefer small, explicit commands such as `ls /srv/reports` or `cat /srv/reports/q3.csv`. The working directory starts at your first granted folder.
3. The `MESHY_ALLOWED` env var inside the session lists your paths, for example `[/srv/reports:ro]`.

If the CLI is on PATH you can also run `meshy ssh <resource> -- <command>` from Bash. It is the same mechanism, and the host key is pinned from ENS.

## Error codes: what to do

| Code | Meaning | Your action |
|---|---|---|
| `MESHY_BLOCKED path=... allowed=[...]` | You touched something outside your grant, or wrote to a read-only path. | Stop. Don't probe other paths, use symlinks or `..`, or switch tools. Tell the user what you needed and suggest they extend the grant. |
| `MESHY_REVOKED grant=... block=N` | An admin revoked your grant on-chain, and your session was killed. | Stop using this resource immediately. Report what you were doing and what is unfinished. Don't reconnect. |
| `MESHY_EXPIRED` | Your grant's time ran out. | Ask the user for a new grant if more work is needed. |
| `MESHY_NO_GRANT` | You have no live grant, or your identity isn't active. | Check `meshy_whoami` and `meshy_grants`, then ask the user. |
| `MESHY_NOT_HUMAN_BACKED` | Your identity has no valid World ID attestation. | Ask the user to finish the `/meshy:join` approval. |

The sandbox is a security boundary set by humans. Treat a block as an answer, not a puzzle: never try to escape it, enumerate around it, or exfiltrate data from the resource.
