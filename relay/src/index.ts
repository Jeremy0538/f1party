// F1 Party relay: one Durable Object per party code. The host's app sends its MultiViewer world-feed
// position; the relay timestamps it and fans it out to the guests. It also answers clock pings, so every
// app can translate relay timestamps into its own clock, and keeps the member list for the UIs.
//
//   GET /f1party/ws?room=CODE&role=host|guest&name=NAME   (WebSocket)
//   GET /f1party/health
//   GET /f1party/join/CODE   (a page that opens f1party://join/CODE: the clickable link in Discord invites)
//   POST /f1party/post       (group Discord invites: {key, kind, ...}; see postToDiscord)
//   POST /f1party/group/invite {key} -> {url}   GET /f1party/group/TOKEN (page)   POST /f1party/group/redeem {token} -> {key}
//
// Messages (JSON):
//   any   -> relay  {t:"ping", c}                        relay -> same  {t:"pong", c, s}
//   host  -> relay  {t:"state", ...feed state}          relay -> guests {t:"state", ..., s}  (s = relay ms)
//   guest -> relay  {t:"report", drift, status}         relay -> all    {t:"members", members:[...]}
//   relay -> all    {t:"host", online}                   relay -> socket {t:"error", message} then close

import { DurableObject } from "cloudflare:workers";

export interface Env {
	ROOMS: DurableObjectNamespace<Room>;
	// Secrets (wrangler secret put): the friend group's Discord webhook and the key the apps send instead.
	DISCORD_WEBHOOK?: string;
	GROUP_KEY?: string;
}

const CODE_RE = /^[A-Z0-9]{4,8}$/;

export default {
	async fetch(req: Request, env: Env): Promise<Response> {
		const url = new URL(req.url);
		const path = url.pathname.replace(/^\/f1party/, "") || "/";
		if (path === "/health") return new Response("ok");
		const join = path.match(/^\/join\/([A-Za-z0-9]{4,8})$/);
		if (join) return joinPage(join[1].toUpperCase());
		if (path === "/post" && req.method === "POST") return postToDiscord(req, env, url.origin);
		if (path === "/group/invite" && req.method === "POST") return groupInvite(req, env, url.origin);
		if (path === "/group/redeem" && req.method === "POST") return groupRedeem(req, env);
		const groupLink = path.match(/^\/group\/([A-Za-z0-9]{12,32})$/);
		if (groupLink) return groupPage(groupLink[1]);
		if (path !== "/ws") return new Response("Not found", { status: 404 });
		if (req.headers.get("Upgrade") !== "websocket") return new Response("Expected WebSocket", { status: 426 });
		const room = (url.searchParams.get("room") || "").toUpperCase();
		if (!CODE_RE.test(room)) return new Response("Bad party code", { status: 400 });
		return env.ROOMS.get(env.ROOMS.idFromName(room)).fetch(req);
	},
};

type Attachment = { role: "host" | "guest"; name: string; drift?: number; status?: string };

export class Room extends DurableObject<Env> {
	async fetch(req: Request): Promise<Response> {
		const url = new URL(req.url);
		if (url.pathname === "/internal/group") return this.groupTokens(await req.json());
		if (url.pathname === "/internal/allow") return this.allowPost(url.searchParams.get("kind") || "", url.searchParams.get("needHost") === "1");
		const role = url.searchParams.get("role") === "host" ? "host" : "guest";
		const name = (url.searchParams.get("name") || "Guest").slice(0, 32);
		const pair = new WebSocketPair();
		const [client, server] = Object.values(pair);

		if (role === "host") {
			// A party has one host; a new host connection (e.g. after a reconnect) replaces the old one.
			for (const ws of this.ctx.getWebSockets("host")) ws.close(4001, "Replaced by a new host connection");
		} else if (this.ctx.getWebSockets().length >= 50) {
			server.accept();
			server.send(JSON.stringify({ t: "error", message: "This party is full" }));
			server.close(4002, "full");
			return new Response(null, { status: 101, webSocket: client });
		}
		this.ctx.acceptWebSocket(server, [role]);
		server.serializeAttachment({ role, name } satisfies Attachment);

		const last = await this.ctx.storage.get<object>("state");
		const hostOnline = this.ctx.getWebSockets("host").length > 0;
		if (role === "guest") {
			server.send(JSON.stringify({ t: "host", online: hostOnline }));
			if (last && hostOnline) server.send(JSON.stringify(last));
		} else {
			this.broadcast({ t: "host", online: true }, "guest");
		}
		this.sendMembers();
		return new Response(null, { status: 101, webSocket: client });
	}

	async webSocketMessage(ws: WebSocket, raw: string | ArrayBuffer) {
		if (typeof raw !== "string" || raw.length > 4096) return;
		let msg: any;
		try { msg = JSON.parse(raw); } catch { return; }
		const me = ws.deserializeAttachment() as Attachment;
		switch (msg.t) {
			case "ping":
				ws.send(JSON.stringify({ t: "pong", c: msg.c, s: Date.now() }));
				break;
			case "state": {
				if (me.role !== "host") return;
				const out = { ...msg, t: "state", s: Date.now() };
				await this.ctx.storage.put("state", out);
				this.broadcast(out, "guest");
				break;
			}
			case "report":
				if (me.role !== "guest") return;
				me.drift = typeof msg.drift === "number" ? msg.drift : undefined;
				me.status = typeof msg.status === "string" ? msg.status.slice(0, 64) : undefined;
				ws.serializeAttachment(me);
				this.sendMembers();
				break;
		}
	}

	async webSocketClose(ws: WebSocket, code: number) {
		const me = ws.deserializeAttachment() as Attachment;
		try { ws.close(code === 1005 ? 1000 : code, "bye"); } catch {}
		if (me.role === "host" && this.ctx.getWebSockets("host").filter((w) => w !== ws).length === 0) {
			this.broadcast({ t: "host", online: false }, "guest");
		}
		this.sendMembers(ws);
	}

	async webSocketError(ws: WebSocket) {
		await this.webSocketClose(ws, 1011);
	}

	// Group Discord posts: at most one of each kind per party every few minutes; an invite only while the
	// party's host is connected. The "__global" room caps all group posts together.
	private async allowPost(kind: string, needHost: boolean): Promise<Response> {
		if (needHost && this.ctx.getWebSockets("host").length === 0) return new Response("This party isn't running", { status: 409 });
		const now = Date.now();
		if (kind === "global") {
			const recent = ((await this.ctx.storage.get<number[]>("posts")) || []).filter((t) => now - t < 3600_000);
			if (recent.length >= 30) return new Response("Too many Discord posts this hour, try again later", { status: 429 });
			recent.push(now);
			await this.ctx.storage.put("posts", recent);
			return new Response("ok");
		}
		const last = (await this.ctx.storage.get<number>("posted:" + kind)) || 0;
		if (now - last < 120_000) return new Response("Already posted to Discord a moment ago", { status: 429 });
		await this.ctx.storage.put("posted:" + kind, now);
		return new Response("ok");
	}

	// Group invite links (only in the "__group" object): token -> {exp, uses}. Creating is capped at 20 a
	// day, redeeming at 60 tries an hour, so tokens can't be guessed or farmed.
	private async groupTokens(cmd: { op: string; token?: string }): Promise<Response> {
		const now = Date.now();
		const cap = async (name: string, max: number, windowMs: number) => {
			const recent = ((await this.ctx.storage.get<number[]>(name)) || []).filter((t) => now - t < windowMs);
			if (recent.length >= max) return false;
			recent.push(now);
			await this.ctx.storage.put(name, recent);
			return true;
		};
		if (cmd.op === "create") {
			if (!(await cap("created", 20, 86400_000))) return new Response("Too many invite links today", { status: 429 });
			const bytes = crypto.getRandomValues(new Uint8Array(12));
			const abc = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789";
			const token = Array.from(bytes, (b) => abc[b % abc.length]).join("") + Array.from(crypto.getRandomValues(new Uint8Array(4)), (b) => abc[b % abc.length]).join("");
			await this.ctx.storage.put("t:" + token, { exp: now + 7 * 86400_000, uses: 5 });
			return Response.json({ token });
		}
		if (cmd.op === "redeem" && cmd.token) {
			if (!(await cap("tries", 60, 3600_000))) return new Response("Too many tries, try again later", { status: 429 });
			const t = await this.ctx.storage.get<{ exp: number; uses: number }>("t:" + cmd.token);
			if (!t || t.exp < now || t.uses <= 0) {
				if (t) await this.ctx.storage.delete("t:" + cmd.token);
				return new Response("This invite link has expired. Ask for a new one.", { status: 410 });
			}
			t.uses--;
			if (t.uses > 0) await this.ctx.storage.put("t:" + cmd.token, t);
			else await this.ctx.storage.delete("t:" + cmd.token);
			return new Response("ok");
		}
		return new Response("Bad request", { status: 400 });
	}

	private broadcast(msg: object, tag?: string, skip?: WebSocket) {
		const data = JSON.stringify(msg);
		for (const ws of this.ctx.getWebSockets(tag)) {
			if (ws === skip) continue;
			try { ws.send(data); } catch {}
		}
	}

	// Member list goes out on joins/leaves and guest reports (reports come every few seconds per guest).
	private sendMembers(closing?: WebSocket) {
		const members = this.ctx.getWebSockets()
			.filter((ws) => ws !== closing)
			.map((ws) => ws.deserializeAttachment() as Attachment)
			.map(({ role, name, drift, status }) => ({ role, name, drift, status }));
		this.broadcast({ t: "members", members }, undefined, closing);
	}
}

// The page behind an invite link: opens the F1 Party app on this party (f1party://join/CODE), with a
// download link for people who don't have it yet. Discord only makes https links clickable.
function joinPage(code: string): Response {
	const app = `f1party://join/${code}`;
	const html = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Join F1 Party ${code}</title>
<meta property="og:title" content="Join my F1 Party: ${code}">
<meta property="og:description" content="Watch F1 in sync with friends in MultiViewer.">
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0c0d10;color:#eef0f3;font:15px/1.5 system-ui,sans-serif}
main{max-width:380px;padding:24px;text-align:center}h1{font-size:22px;margin:0 0 6px}.code{font:700 34px/1.2 system-ui;letter-spacing:.18em;margin:14px 0}
a.btn{display:inline-block;background:#e10600;color:#fff;text-decoration:none;font-weight:600;padding:11px 18px;border-radius:7px}
p{color:#8a909c}a{color:#ff7a72}</style></head><body><main>
<h1>Join this F1 Party</h1><div class="code">${code}</div>
<a class="btn" href="${app}">Open in F1 Party</a>
<p>Nothing happens? Get F1 Party from <a href="https://github.com/Jeremy0538/f1party/releases/latest">GitHub</a>, then click the button again, or enter the code in the app.</p>
</main><script>location.href=${JSON.stringify(app)};</script></body></html>`;
	return new Response(html, { headers: { "content-type": "text/html; charset=utf-8", "cache-control": "no-store" } });
}

// Group Discord invites: the apps send the group key and the plain facts (name, code, session, time), never
// text; the relay checks the key and the limits and writes the message itself, so a leaked key can only post
// ordinary invites for real parties, and the webhook URL itself never leaves the relay.
const KINDS = new Set(["invite", "plan", "remind"]);
// Names and titles go into Discord markdown: keep letters, digits, spaces and plain punctuation only.
const clean = (v: unknown, max: number) =>
	String(v ?? "").replace(/[^\p{L}\p{N} .,:;'!?&+\-·()]/gu, " ").replace(/[()]/g, "").replace(/\s+/g, " ").trim().slice(0, max);

function sameKey(a: string, b: string): boolean {
	const x = new TextEncoder().encode(a), y = new TextEncoder().encode(b);
	let diff = x.length ^ y.length;
	for (let i = 0; i < Math.max(x.length, y.length); i++) diff |= (x[i] ?? 0) ^ (y[i] ?? 0);
	return diff === 0;
}

async function postToDiscord(req: Request, env: Env, origin: string): Promise<Response> {
	if (!env.GROUP_KEY || !env.DISCORD_WEBHOOK) return new Response("Teams aren't set up on this relay", { status: 503 });
	let body: any;
	try { body = await req.json(); } catch { return new Response("Bad request", { status: 400 }); }
	if (typeof body?.key !== "string" || !sameKey(body.key, env.GROUP_KEY)) return new Response("Wrong group key", { status: 403 });
	const kind = String(body.kind);
	const code = String(body.code || "").toUpperCase();
	if (!KINDS.has(kind) || !CODE_RE.test(code)) return new Response("Bad request", { status: 400 });
	const name = clean(body.name, 32) || "Someone";
	const title = clean(body.title, 80);
	const at = Math.floor(Number(body.at) || 0);
	const now = Date.now() / 1000;
	if (kind !== "invite" && (!title || at < now - 600 || at > now + 60 * 86400)) return new Response("Bad request", { status: 400 });

	const check = async (room: string, k: string, needHost = false) => {
		const res = await env.ROOMS.get(env.ROOMS.idFromName(room)).fetch(`https://room/internal/allow?kind=${k}&needHost=${needHost ? 1 : 0}`);
		return res.ok ? null : new Response(await res.text(), { status: res.status });
	};
	const refused = (await check(code, kind, kind === "invite")) || (await check("__global", "global"));
	if (refused) return refused;

	const join = `[Join the party](<${origin}/f1party/join/${code}>)`;
	const download = "\nNo F1 Party yet? [Download it](<https://github.com/Jeremy0538/f1party/releases/latest>)";
	let text: string;
	if (kind === "invite") {
		text = `🏁 **${name}** is hosting an F1 Party` + (title ? `\n📺 **${title}**` : "") + `\nCode: **${code}** · ${join}` + download;
	} else if (kind === "plan") {
		text = `🗓️ **${name}** plans an F1 Party\n📺 **${title}**\n⏰ <t:${at}:F> (<t:${at}:R>)\nCode: **${code}** · ${join} (from the start time)` + download;
	} else {
		text = `⏰ **${title}** starts <t:${at}:R>: ${name}'s F1 Party\nCode: **${code}** · ${join}`;
	}
	const res = await fetch(env.DISCORD_WEBHOOK + "?wait=true", {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify({ username: "F1 Party", content: text, allowed_mentions: { parse: [] } }),
	});
	if (!res.ok) return new Response("Discord: " + res.status, { status: 502 });
	return new Response("ok");
}

// Group invite links: someone who has the group key makes a link (valid 7 days, 5 uses); opening it starts
// F1 Party (f1party://group/TOKEN), which trades the token here for the key. The link never holds the key.
const groupRoom = (env: Env) => env.ROOMS.get(env.ROOMS.idFromName("__group"));

async function groupInvite(req: Request, env: Env, origin: string): Promise<Response> {
	if (!env.GROUP_KEY) return new Response("Teams aren't set up on this relay", { status: 503 });
	let body: any;
	try { body = await req.json(); } catch { return new Response("Bad request", { status: 400 }); }
	if (typeof body?.key !== "string" || !sameKey(body.key, env.GROUP_KEY)) return new Response("Wrong group key", { status: 403 });
	const res = await groupRoom(env).fetch("https://room/internal/group", { method: "POST", body: JSON.stringify({ op: "create" }) });
	if (!res.ok) return new Response(await res.text(), { status: res.status });
	const { token } = (await res.json()) as { token: string };
	return Response.json({ url: `${origin}/f1party/group/${token}` });
}

async function groupRedeem(req: Request, env: Env): Promise<Response> {
	if (!env.GROUP_KEY) return new Response("Teams aren't set up on this relay", { status: 503 });
	let body: any;
	try { body = await req.json(); } catch { return new Response("Bad request", { status: 400 }); }
	const token = String(body?.token || "");
	if (!/^[A-Za-z0-9]{12,32}$/.test(token)) return new Response("Bad request", { status: 400 });
	const res = await groupRoom(env).fetch("https://room/internal/group", { method: "POST", body: JSON.stringify({ op: "redeem", token }) });
	if (!res.ok) return new Response(await res.text(), { status: res.status });
	return Response.json({ key: env.GROUP_KEY }, { headers: { "cache-control": "no-store" } });
}

function groupPage(token: string): Response {
	const app = `f1party://group/${token}`;
	const html = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Join our F1 Party team</title>
<meta property="og:title" content="Join our F1 Party team">
<meta property="og:description" content="One click puts you in our team: party invites go to our Discord channel.">
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0c0d10;color:#eef0f3;font:15px/1.5 system-ui,sans-serif}
main{max-width:400px;padding:24px;text-align:center}h1{font-size:22px;margin:0 0 10px}
a.btn{display:inline-block;background:#e10600;color:#fff;text-decoration:none;font-weight:600;padding:11px 18px;border-radius:7px;margin:8px 0}
p{color:#8a909c}a{color:#ff7a72}</style></head><body><main>
<h1>Join our F1 Party team</h1>
<p>Your team is our group of friends: every party invite goes to our Discord channel. One click and you're in, nothing to paste.</p>
<a class="btn" href="${app}">Open in F1 Party</a>
<p>Nothing happens? Get F1 Party from <a href="https://github.com/Jeremy0538/f1party/releases/latest">GitHub</a>, start it once, then click the button again.</p>
<p>The link works for 7 days.</p>
</main><script>location.href=${JSON.stringify(app)};</script></body></html>`;
	return new Response(html, { headers: { "content-type": "text/html; charset=utf-8", "cache-control": "no-store", "x-robots-tag": "noindex" } });
}
