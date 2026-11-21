// F1 Party relay: one Durable Object per party code. The host's app sends its MultiViewer world-feed
// position; the relay timestamps it and fans it out to the guests. It also answers clock pings, so every
// app can translate relay timestamps into its own clock, and keeps the member list for the UIs.
//
//   GET /f1party/ws?room=CODE&role=host|guest&name=NAME   (WebSocket)
//   GET /f1party/health
//   GET /f1party/join/CODE   (a page that opens f1party://join/CODE: the clickable link in Discord invites)
//
// Messages (JSON):
//   any   -> relay  {t:"ping", c}                        relay -> same  {t:"pong", c, s}
//   host  -> relay  {t:"state", ...feed state}          relay -> guests {t:"state", ..., s}  (s = relay ms)
//   guest -> relay  {t:"report", drift, status}         relay -> all    {t:"members", members:[...]}
//   relay -> all    {t:"host", online}                   relay -> socket {t:"error", message} then close

import { DurableObject } from "cloudflare:workers";

export interface Env {
	ROOMS: DurableObjectNamespace<Room>;
}

const CODE_RE = /^[A-Z0-9]{4,8}$/;

export default {
	async fetch(req: Request, env: Env): Promise<Response> {
		const url = new URL(req.url);
		const path = url.pathname.replace(/^\/f1party/, "") || "/";
		if (path === "/health") return new Response("ok");
		const join = path.match(/^\/join\/([A-Za-z0-9]{4,8})$/);
		if (join) return joinPage(join[1].toUpperCase());
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
