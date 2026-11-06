// Email an invite link via Resend (https://resend.com). Server-side only: the API key
// never reaches the browser, and Meshy stores nothing (not even the address).
// Env: RESEND_API_KEY, optional INVITE_FROM (default "Meshy <invites@meshy.live>").
import { NextResponse } from "next/server";

const hits = new Map<string, number[]>(); // ip -> timestamps (best effort, per instance)

function limited(ip: string) {
  const now = Date.now();
  const recent = (hits.get(ip) ?? []).filter((t) => now - t < 3_600_000);
  recent.push(now);
  hits.set(ip, recent);
  return recent.length > 20; // 20 emails per hour per IP
}

const esc = (s: string) => s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

export async function POST(req: Request) {
  const key = process.env.RESEND_API_KEY;
  if (!key) return NextResponse.json({ error: "Email invites aren't configured yet (RESEND_API_KEY). Copy the link instead." }, { status: 503 });
  const ip = req.headers.get("x-forwarded-for")?.split(",")[0]?.trim() ?? "unknown";
  if (limited(ip)) return NextResponse.json({ error: "Too many invites from here. Try again later." }, { status: 429 });

  const { to, link, org, role } = (await req.json().catch(() => ({}))) as { to?: string; link?: string; org?: string; role?: string };
  if (!to || !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(to)) return NextResponse.json({ error: "Invalid email address" }, { status: 400 });
  let u: URL;
  try {
    u = new URL(link ?? "");
  } catch {
    return NextResponse.json({ error: "Invalid invite link" }, { status: 400 });
  }
  const okHost = ["meshy.live", "www.meshy.live", "localhost:3000", new URL(req.url).host].includes(u.host);
  if (!okHost || !u.pathname.startsWith("/join/")) return NextResponse.json({ error: "Only Meshy invite links can be emailed" }, { status: 400 });
  if (!org || !/^([a-z0-9-]+\.)+eth$/.test(org)) return NextResponse.json({ error: "Invalid org" }, { status: 400 });

  const who = role === "agent" ? "an agent" : "a member";
  const html = `<div style="font-family:-apple-system,Segoe UI,Inter,sans-serif;max-width:520px;margin:0 auto;padding:32px;color:#0b1020">
  <div style="font-weight:700;font-size:18px;letter-spacing:-.02em">meshy</div>
  <h1 style="font-size:24px;letter-spacing:-.03em;margin:28px 0 8px">You're invited to ${esc(org)}</h1>
  <p style="font-size:15px;line-height:1.55;color:#4a5578">You've been invited to join <b>${esc(org)}</b> on Meshy as ${who}. Accept with your wallet: you'll claim your own name in the org with one Sepolia transaction.</p>
  <p style="margin:28px 0"><a href="${esc(u.toString())}" style="background:#3b5bfd;color:#fff;text-decoration:none;padding:12px 20px;border-radius:999px;font-weight:600;font-size:14px">Accept invite</a></p>
  <p style="font-size:12px;color:#8a93b2">Or open: ${esc(u.toString())}</p>
  <p style="font-size:12px;color:#8a93b2;margin-top:28px">Meshy: network access for agents and people, as ENS names. If you weren't expecting this, ignore it.</p>
</div>`;

  const r = await fetch("https://api.resend.com/emails", {
    method: "POST",
    headers: { Authorization: `Bearer ${key}`, "Content-Type": "application/json" },
    body: JSON.stringify({
      from: process.env.INVITE_FROM ?? "Meshy <invites@meshy.live>",
      to: [to],
      subject: `You're invited to ${org} on Meshy`,
      html,
      text: `You've been invited to join ${org} on Meshy as ${who}.\n\nAccept: ${u.toString()}\n`,
    }),
  });
  if (!r.ok) {
    const j = (await r.json().catch(() => ({}))) as { message?: string };
    return NextResponse.json({ error: j.message ?? `Resend error ${r.status}` }, { status: 502 });
  }
  return NextResponse.json({ ok: true });
}
