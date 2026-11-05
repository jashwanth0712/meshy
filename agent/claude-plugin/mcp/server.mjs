#!/usr/bin/env node
// Meshy MCP server for Claude Code — zero dependencies, stdio JSON-RPC.
// Wraps the `meshy` CLI (set MESHY_BIN to its path if not on PATH).
import { spawn } from "node:child_process";
import { mkdirSync, openSync, readFileSync, existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";

const BIN = process.env.MESHY_BIN || "meshy";
const HOME = process.env.MESHY_HOME || join(homedir(), ".meshy");
const PROTOCOL = "2025-06-18";

const TOOLS = [
  {
    name: "meshy_join",
    description:
      "Join a Meshy org with an invite link from your human. Generates this agent's keys and returns an approval URL the HUMAN must open (they sign in, complete World ID, accept). Approval is then detected in the background; check with meshy_whoami.",
    inputSchema: {
      type: "object",
      properties: { invite: { type: "string", description: "Invite link, e.g. https://<dashboard>/approve?org=acme.eth&agent=claude-1" } },
      required: ["invite"],
    },
  },
  {
    name: "meshy_whoami",
    description: "Show this agent's Meshy identity (ENS name, address, key) and whether it is active and human-backed.",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "meshy_grants",
    description:
      "Check what you may access: for each resource ENS name, whether you hold a live grant and when it expires. Grants are ENSv2 subnames <agent>.<resource>; admins can revoke them at any time.",
    inputSchema: {
      type: "object",
      properties: { resources: { type: "array", items: { type: "string" }, description: "Resource ENS names, e.g. db1.prod.acme.eth" } },
      required: ["resources"],
    },
  },
  {
    name: "meshy_ssh",
    description:
      "Run a shell command on a Meshy resource you have a grant for. The session is sandboxed to the granted folders (ro/rw); anything else returns MESHY_BLOCKED. If your grant is revoked or expires mid-command the session is killed (MESHY_REVOKED / MESHY_EXPIRED). Every file access is audited on Arkiv.",
    inputSchema: {
      type: "object",
      properties: {
        resource: { type: "string", description: "Resource ENS name, e.g. db1.prod.acme.eth" },
        command: { type: "string", description: "Shell command to run on the resource" },
        timeout_sec: { type: "number", description: "Kill locally after this many seconds (default 120)" },
      },
      required: ["resource", "command"],
    },
  },
];

function run(args, { timeoutSec = 120 } = {}) {
  return new Promise((resolve) => {
    const p = spawn(BIN, args, { env: process.env });
    let out = "", err = "";
    p.stdout.on("data", (d) => (out += d));
    p.stderr.on("data", (d) => (err += d));
    const t = setTimeout(() => p.kill("SIGKILL"), timeoutSec * 1000);
    p.on("error", (e) => { clearTimeout(t); resolve({ code: 127, out, err: `${err}${e.message} (is the meshy CLI installed? set MESHY_BIN)` }); });
    p.on("close", (code, sig) => { clearTimeout(t); resolve({ code: code ?? (sig ? 128 : 1), out, err, sig }); });
  });
}

function codesIn(text) {
  return [...new Set((text.match(/MESHY_[A-Z_]+/g) || []))];
}

function explain(codes) {
  const notes = {
    MESHY_BLOCKED: "A path or command outside your grant was refused. Do not retry or work around it; ask the human to extend the grant if needed.",
    MESHY_REVOKED: "Your grant was revoked on-chain and the session was terminated. Stop using this resource and tell the human.",
    MESHY_EXPIRED: "Your grant expired. Ask the human for a new grant.",
    MESHY_NO_GRANT: "You have no live grant for this resource (or your identity is not active).",
    MESHY_NOT_HUMAN_BACKED: "Your identity lacks a valid World ID attestation. Ask the human to complete /meshy:join approval.",
    MESHY_UNREACHABLE: "The resource could not be reached over the mesh.",
  };
  return codes.filter((c) => notes[c]).map((c) => `${c}: ${notes[c]}`).join("\n");
}

async function callTool(name, a) {
  switch (name) {
    case "meshy_join": {
      mkdirSync(HOME, { recursive: true });
      const log = join(HOME, "join.log");
      const fd = openSync(log, "w");
      const p = spawn(BIN, ["join", "--no-open", "--timeout", "30m", a.invite], { detached: true, stdio: ["ignore", fd, fd], env: process.env });
      p.unref();
      for (let i = 0; i < 40; i++) {
        await new Promise((r) => setTimeout(r, 250));
        const s = existsSync(log) ? readFileSync(log, "utf8") : "";
        const m = s.match(/MESHY_APPROVE_URL (\S+)/);
        if (m) {
          const ident = (s.match(/Agent identity: (\S+)/) || [])[1] || "";
          return text(
            `Agent identity: ${ident}\n\nAsk your human to open this link to approve you (sign in, World ID, accept):\n${m[1]}\n\nApproval is being watched in the background. Call meshy_whoami to confirm once they are done.`,
          );
        }
        if (/error|exists/i.test(s)) return text(s, true);
      }
      return text(`meshy join did not print an approval URL yet; see ${log}`, true);
    }
    case "meshy_whoami": {
      const r = await run(["whoami"], { timeoutSec: 30 });
      return text((r.out + r.err).trim(), r.code !== 0);
    }
    case "meshy_grants": {
      const r = await run(["grants", ...(a.resources || [])], { timeoutSec: 60 });
      return text((r.out + r.err).trim(), r.code !== 0);
    }
    case "meshy_ssh": {
      const r = await run(["ssh", a.resource, "--", a.command], { timeoutSec: a.timeout_sec || 120 });
      const codes = codesIn(r.err + r.out);
      const parts = [`exit_code: ${r.code}`];
      if (r.out) parts.push(`stdout:\n${r.out}`);
      if (r.err) parts.push(`stderr:\n${r.err}`);
      const ex = explain(codes);
      if (ex) parts.push(`meshy:\n${ex}`);
      return text(parts.join("\n\n"), r.code !== 0);
    }
    default:
      throw Object.assign(new Error(`unknown tool ${name}`), { code: -32602 });
  }
}

const text = (t, isError = false) => ({ content: [{ type: "text", text: t || "(no output)" }], isError });

function send(msg) {
  process.stdout.write(JSON.stringify(msg) + "\n");
}

const rl = createInterface({ input: process.stdin });
rl.on("line", async (line) => {
  let req;
  try { req = JSON.parse(line); } catch { return; }
  const { id, method, params } = req;
  if (id === undefined) return; // notification
  try {
    let result;
    if (method === "initialize") {
      result = { protocolVersion: params?.protocolVersion || PROTOCOL, capabilities: { tools: {} }, serverInfo: { name: "meshy", version: "0.1.0" } };
    } else if (method === "tools/list") {
      result = { tools: TOOLS };
    } else if (method === "tools/call") {
      result = await callTool(params.name, params.arguments || {});
    } else if (method === "ping") {
      result = {};
    } else {
      return send({ jsonrpc: "2.0", id, error: { code: -32601, message: `method not found: ${method}` } });
    }
    send({ jsonrpc: "2.0", id, result });
  } catch (e) {
    send({ jsonrpc: "2.0", id, error: { code: e.code || -32603, message: e.message } });
  }
});
