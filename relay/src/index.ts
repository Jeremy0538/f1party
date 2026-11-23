// F1 Party relay: one Durable Object per party code. The host's app sends its MultiViewer world-feed
// position; the relay timestamps it and fans it out to the guests. It also answers clock pings, so every
// app can translate relay timestamps into its own clock, and keeps the member list for the UIs.
//
//   GET /f1party/ws?room=CODE&role=host|guest&name=NAME   (WebSocket)
//   GET /f1party/health
//   GET /f1party/join/CODE   (a page that opens f1party://join/CODE: the clickable link in Discord invites)
//   POST /f1party/post       (group Discord invites: {key, kind, ...}; see postToDiscord)
//   POST /f1party/team/create {name, webhook} -> {key, name}   POST /f1party/team/info {key} -> {name}
//   POST /f1party/group/invite {key} -> {url}   GET /f1party/group/TOKEN (page)   POST /f1party/group/redeem {token, id} -> {key}
//   POST /f1party/team/rename|leave|invitefriend|kick {id, secret, key, ...}  (kick: the owner removes {member})
//   POST /f1party/friend/ask|accept|decline {id, secret, name, to}   friend requests between teammates
//   POST /f1party/friend/remove {id, secret, name, to}   unfriend: gone on both sides
//   POST /f1party/me/pic {id, secret, pic}   POST /f1party/team/logo {id, secret, key, logo}   (a small image as a
//        data URL, "" removes it)   GET /f1party/pic/u/UID  GET /f1party/pic/t/TEAMID   (the image; ?v= for caching)
//
// Messages (JSON):
//   any   -> relay  {t:"ping", c}                        relay -> same  {t:"pong", c, s}
//   host  -> relay  {t:"state", ...feed state}          relay -> guests {t:"state", ..., s}  (s = relay ms)
//   guest -> relay  {t:"report", drift, status}         relay -> all    {t:"members", members:[...]}
//   relay -> all    {t:"host", online}                   relay -> socket {t:"error", message} then close

import { DurableObject } from "cloudflare:workers";

export interface Env {
	ROOMS: DurableObjectNamespace<Room>;
	SKIP_WEBHOOK_CHECK?: string; // local tests only (wrangler dev --var SKIP_WEBHOOK_CHECK:1), never set live
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
		if (req.method === "POST") {
			switch (path) {
				case "/people": return people(req, env);
				case "/team/create": return teamCreate(req, env);
				case "/team/info": return teamInfo(req, env);
				case "/team/rename": return teamAction(req, env, "teamRename");
				case "/team/leave": return teamAction(req, env, "teamLeave");
				case "/team/invitefriend": return teamAction(req, env, "teamInviteFriend");
				case "/team/kick": return teamAction(req, env, "teamKick");
				case "/team/logo": return teamAction(req, env, "teamLogo");
				case "/me/pic": return setPic(req, env);
				case "/friend/link": return friendLink(req, env, url.origin);
				case "/friend/redeem": return friendRedeem(req, env);
				case "/friend/ask": return friendOp(req, env, "friendAsk");
				case "/friend/accept": return friendOp(req, env, "friendAccept");
				case "/friend/decline": return friendOp(req, env, "friendDecline");
				case "/friend/remove": return friendOp(req, env, "unfriend");
				case "/invite/send": return sendInvites(req, env);
			}
		}
		if (path === "/group/invite" && req.method === "POST") return groupInvite(req, env, url.origin);
		if (path === "/group/redeem" && req.method === "POST") return groupRedeem(req, env);
		const groupLink = path.match(/^\/group\/([A-Za-z0-9]{12,32})$/);
		if (groupLink) return groupPage(groupLink[1]);
		const pic = path.match(/^\/pic\/(u|t)\/([A-Za-z0-9]{8,12})$/);
		if (pic && req.method === "GET") return getPic(env, pic[1], pic[2], url.searchParams.has("v"));
		const friendLinkPath = path.match(/^\/friend\/([A-Za-z0-9]{12,32})$/);
		if (friendLinkPath) return friendPage(friendLinkPath[1]);
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
		if (url.pathname === "/internal/group") return this.people(await req.json());
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

	// People, teams and links, all in the one "__group" object (a friend group's worth of data):
	//   u:<uid>     {secret (sha256), name, status, seen, friends[], teams[]}   the app's fixed id + its secret
	//   team:<id>   {name, webhook, key, created, owner, members{uid: seen}, banned{uid: when}}
	//   t:<token>   team link {team, exp, uses}      fl:<token> friend link {from, exp, uses}
	//   inbox:<uid> [events]  friend made, party invite, team invite; handed over on the next presence call
	//   fq:<to>:<from> <when>  a friend request to a teammate, until it's accepted or declined (30 days)
	//   pic:u:<uid>, pic:t:<team>  {data} a profile picture / team logo (data URL); u.pic / team.logo hold its version
	// Caps keep keys and links from being guessed or farmed.
	private async people(cmd: any): Promise<Response> {
		const now = Date.now();
		const st = this.ctx.storage;
		const cap = async (name: string, max: number, windowMs: number) => {
			const recent = ((await st.get<number[]>(name)) || []).filter((t) => now - t < windowMs);
			if (recent.length >= max) return false;
			recent.push(now);
			await st.put(name, recent);
			return true;
		};
		const abc = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789";
		const random = (n: number) => Array.from(crypto.getRandomValues(new Uint8Array(n)), (b) => abc[b % abc.length]).join("");
		const err = (msg: string, status = 400) => new Response(msg, { status });

		// The app's id + secret: the first call claims the id, later calls must bring the same secret.
		let me: User | undefined;
		const auth = async (): Promise<Response | null> => {
			if (!/^[0-9a-f]{12}$/.test(cmd.uid || "") || typeof cmd.secret !== "string" || cmd.secret.length < 20) return err("Bad request");
			const hash = await sha256(cmd.secret);
			me = await st.get<User>("u:" + cmd.uid);
			if (!me) {
				if (!(await cap("newusers", 200, 86400_000))) return err("Too many new users today", 429);
				me = { secret: hash, name: cmd.name || "Someone", seen: now, friends: [], teams: [] };
				await st.put("u:" + cmd.uid, me);
			} else if (!sameKey(hash, me.secret)) return err("This app's id belongs to someone else", 403);
			if (cmd.name) me.name = cmd.name;
			return null;
		};
		const saveMe = () => st.put("u:" + cmd.uid, me);
		const inbox = async (to: string, ev: object) => {
			const list = ((await st.get<object[]>("inbox:" + to)) || []).slice(-19);
			list.push({ ...ev, at: now });
			await st.put("inbox:" + to, list);
		};
		const user = (id: string) => st.get<User>("u:" + id);
		const mutual = async (a: User, aid: string, bid: string) => a.friends.includes(bid) && !!(await user(bid))?.friends.includes(aid);
		const sharesTeam = async (a: User, bid: string) => {
			for (const tid of a.teams) if ((await st.get<TeamRec>("team:" + tid))?.members?.[bid]) return true;
			return false;
		};
		const teamLink = async (team: string, uses: number) => {
			const token = random(16);
			await st.put("t:" + token, { team, exp: now + 7 * 86400_000, uses });
			return token;
		};

		switch (cmd.op) {
			case "picGet": { // public: a picture by owner, for the apps that show it
				const p = await st.get<{ data: string }>(`pic:${cmd.kind}:${cmd.id}`);
				return p ? Response.json(p) : err("No picture", 404);
			}
			case "teamGet": {
				const t = await st.get("team:" + cmd.team);
				return t ? Response.json(t) : err("No such team", 404);
			}
			case "create": { // team link, for someone the worker checked has the team key
				if (!(await cap("created:" + cmd.team, 20, 86400_000))) return err("Too many team links today", 429);
				return Response.json({ token: await teamLink(cmd.team, 5) });
			}
			case "redeem": { // team link -> team id (a new link lets someone the owner removed back in)
				if (!cmd.token) break;
				if (!(await cap("tries", 60, 3600_000))) return err("Too many tries, try again later", 429);
				const t = await st.get<{ team?: string; exp: number; uses: number }>("t:" + cmd.token);
				if (!t || !t.team || t.exp < now || t.uses <= 0) {
					if (t) await st.delete("t:" + cmd.token);
					return err("This team link has expired. Ask for a new one.", 410);
				}
				t.uses--;
				if (t.uses > 0) await st.put("t:" + cmd.token, t);
				else await st.delete("t:" + cmd.token);
				if (/^[0-9a-f]{12}$/.test(cmd.uid || "")) {
					const team = await st.get<TeamRec>("team:" + t.team);
					if (team?.banned?.[cmd.uid] !== undefined) { delete team.banned[cmd.uid]; await st.put("team:" + t.team, team); }
				}
				return Response.json({ team: t.team });
			}
		}

		if (cmd.op === "teamCreate") { // 1.3.0 apps send no id: such a team has no owner (nobody can rename it)
			if (cmd.uid) { const d = await auth(); if (d) return d; }
			if (!(await cap("teams:" + (cmd.ip || "?"), 3, 86400_000)) || !(await cap("teams", 10, 86400_000)))
				return err("Too many new teams today, try again tomorrow", 429);
			const id = random(8);
			const key = `f1p_${id}${random(24)}`;
			await st.put("team:" + id, { name: cmd.teamName, webhook: cmd.webhook, key, created: now, owner: me ? cmd.uid : undefined, members: me ? { [cmd.uid]: now } : {} });
			if (me) { if (!me.teams.includes(id)) me.teams.push(id); await saveMe(); }
			return Response.json({ id, key });
		}

		const denied = await auth();
		if (denied) return denied;
		const m = me!;

		switch (cmd.op) {
			case "presence": {
				m.status = cmd.status || null;
				m.seen = now;
				// friends made through a friend link the app hasn't heard about yet (still in its inbox) stay
				const pending = ((await st.get<any[]>("inbox:" + cmd.uid)) || []).filter((e) => e.type === "friend").map((e) => e.id);
				m.friends = [...new Set([...(cmd.friends as string[]), ...pending])].slice(0, 300);
				let teams: string[] = cmd.teams.slice(0, 20);
				const removed: string[] = []; // teams whose owner removed this app: it drops their key
				for (const tid of teams) if ((await st.get<TeamRec>("team:" + tid))?.banned?.[cmd.uid] !== undefined) removed.push(tid);
				teams = teams.filter((tid) => !removed.includes(tid));
				for (const tid of new Set([...m.teams, ...teams])) {
					const t = await st.get<TeamRec>("team:" + tid);
					if (!t) continue;
					t.members ||= {};
					const was = t.members[cmd.uid];
					if (teams.includes(tid)) t.members[cmd.uid] = now;
					else delete t.members[cmd.uid];
					if (was === undefined || !teams.includes(tid) || now - was > 3600_000) await st.put("team:" + tid, t);
				}
				m.teams = teams;
				await saveMe();

				const people = new Map<string, any>();
				const person = (id: string, u: User) => {
					let p = people.get(id);
					if (!p) {
						p = { id, name: u.name, pic: u.pic, seen: u.seen, status: now - u.seen < 90_000 ? u.status || null : null, friend: false, teams: [] };
						people.set(id, p);
					}
					return p;
				};
				for (const fid of m.friends) {
					const f = await user(fid);
					if (f?.friends.includes(cmd.uid)) person(fid, f).friend = true;
				}
				const teamsOut = [];
				for (const tid of m.teams) {
					const t = await st.get<TeamRec>("team:" + tid);
					if (!t) continue;
					const members = Object.entries(t.members || {}).filter(([id, seen]) => id !== cmd.uid && now - seen < 30 * 86400_000);
					for (const [id] of members) {
						const u = await user(id);
						if (u) person(id, u).teams.push(tid);
					}
					teamsOut.push({ id: tid, name: t.name, logo: t.logo, owner: t.owner === cmd.uid, ownerId: t.owner, members: members.length + 1 });
				}
				// friend requests: the ones to you (with who sent them), and which teammates you asked
				const requests = [];
				for (const [k, at] of await st.list<number>({ prefix: "fq:" + cmd.uid + ":" })) {
					const from = k.split(":")[2];
					const u = await user(from);
					if (!u || now - at > 30 * 86400_000 || (m.friends.includes(from) && u.friends.includes(cmd.uid))) { await st.delete(k); continue; }
					requests.push({ id: from, name: u.name, pic: u.pic, at });
				}
				for (const p of people.values()) if (!p.friend && (await st.get("fq:" + p.id + ":" + cmd.uid)) !== undefined) p.asked = true;
				const events = (await st.get<object[]>("inbox:" + cmd.uid)) || [];
				if (events.length) await st.delete("inbox:" + cmd.uid);
				return Response.json({ people: [...people.values()], teams: teamsOut, inbox: events, removed, requests, me: { pic: m.pic } });
			}
			case "setPic": { // your own profile picture ("" removes it)
				if (!(await cap("pic:" + cmd.uid, 30, 86400_000))) return err("Too many picture changes today", 429);
				if (cmd.pic) { await st.put("pic:u:" + cmd.uid, { data: cmd.pic }); m.pic = await picVersion(cmd.pic); }
				else { await st.delete("pic:u:" + cmd.uid); delete m.pic; }
				await saveMe();
				return Response.json({ pic: m.pic || "" });
			}
			case "teamLogo": { // the team's logo, set by the one who started the team ("" removes it)
				const t = await st.get<TeamRec>("team:" + cmd.team);
				if (!t) return err("No such team", 404);
				if (t.owner !== cmd.uid) return err("Only the one who started the team can change its logo", 403);
				if (!(await cap("logo:" + cmd.team, 30, 86400_000))) return err("Too many logo changes today", 429);
				if (cmd.logo) { await st.put("pic:t:" + cmd.team, { data: cmd.logo }); t.logo = await picVersion(cmd.logo); }
				else { await st.delete("pic:t:" + cmd.team); delete t.logo; }
				await st.put("team:" + cmd.team, t);
				return Response.json({ logo: t.logo || "" });
			}
			case "teamRename": {
				const t = await st.get<TeamRec>("team:" + cmd.team);
				if (!t) return err("No such team", 404);
				if (t.owner !== cmd.uid) return err("Only the one who started the team can rename it", 403);
				t.name = cmd.teamName;
				await st.put("team:" + cmd.team, t);
				return new Response("ok");
			}
			case "teamKick": { // the owner removes someone; they can come back only with a new team link
				const t = await st.get<TeamRec>("team:" + cmd.team);
				if (!t) return err("No such team", 404);
				if (t.owner !== cmd.uid) return err("Only the one who started the team can remove people", 403);
				if (!/^[0-9a-f]{12}$/.test(cmd.member) || cmd.member === cmd.uid) return err("Bad request");
				if (t.members) delete t.members[cmd.member];
				(t.banned ||= {})[cmd.member] = now;
				await st.put("team:" + cmd.team, t);
				await inbox(cmd.member, { type: "kicked", team: t.name, name: m.name });
				return new Response("ok");
			}
			case "teamLeave": {
				const t = await st.get<TeamRec>("team:" + cmd.team);
				if (t?.members?.[cmd.uid] !== undefined) { delete t.members[cmd.uid]; await st.put("team:" + cmd.team, t); }
				m.teams = m.teams.filter((x) => x !== cmd.team);
				await saveMe();
				return new Response("ok");
			}
			case "teamInviteFriend": {
				const t = await st.get<TeamRec>("team:" + cmd.team);
				if (!t?.members?.[cmd.uid]) return err("You're not in that team", 403);
				if (!(await mutual(m, cmd.uid, cmd.to))) return err("You can only invite friends", 403);
				if (!(await cap("teaminv:" + cmd.uid, 30, 86400_000))) return err("Too many invites today", 429);
				await inbox(cmd.to, { type: "team", token: await teamLink(cmd.team, 1), team: t.name, from: cmd.uid, name: m.name });
				return new Response("ok");
			}
			case "friendLink": {
				if (!(await cap("fl:" + cmd.uid, 20, 86400_000))) return err("Too many friend links today", 429);
				const token = random(16);
				await st.put("fl:" + token, { from: cmd.uid, exp: now + 7 * 86400_000, uses: 10 });
				await saveMe();
				return Response.json({ token });
			}
			case "friendRedeem": {
				if (!(await cap("tries", 60, 3600_000))) return err("Too many tries, try again later", 429);
				const l = await st.get<{ from: string; exp: number; uses: number }>("fl:" + cmd.token);
				if (!l || l.exp < now || l.uses <= 0) return err("This friend link has expired. Ask for a new one.", 410);
				if (l.from === cmd.uid) return err("That's your own friend link: send it to a friend", 400);
				const other = await user(l.from);
				if (!other) return err("This friend link has expired. Ask for a new one.", 410);
				l.uses--;
				await st.put("fl:" + cmd.token, l);
				if (!m.friends.includes(l.from)) m.friends.push(l.from);
				if (!other.friends.includes(cmd.uid)) { other.friends.push(cmd.uid); await st.put("u:" + l.from, other); }
				await saveMe();
				await inbox(l.from, { type: "friend", id: cmd.uid, name: m.name });
				return Response.json({ id: l.from, name: other.name });
			}
			case "friendAsk": { // a friend request to a teammate (they asked you already: friends now)
				if (cmd.to === cmd.uid || !/^[0-9a-f]{12}$/.test(cmd.to || "")) return err("Bad request");
				if (await mutual(m, cmd.uid, cmd.to)) return Response.json({ friends: true, already: true });
				if (!(await sharesTeam(m, cmd.to))) return err("You can send a friend request to people in your teams", 403);
				if ((await st.get("fq:" + cmd.uid + ":" + cmd.to)) !== undefined) return this.people({ ...cmd, op: "friendAccept" });
				if (!(await cap("fq:" + cmd.uid, 30, 86400_000))) return err("Too many friend requests today", 429);
				await st.put("fq:" + cmd.to + ":" + cmd.uid, now);
				return Response.json({ sent: true });
			}
			case "friendAccept": { // only a request that was really sent makes friends
				const k = "fq:" + cmd.uid + ":" + cmd.to;
				const other = await user(cmd.to);
				if (other && (await mutual(m, cmd.uid, cmd.to))) { await st.delete(k); return Response.json({ friends: true, already: true, id: cmd.to, name: other.name }); }
				if ((await st.get(k)) === undefined || !other) return err("That friend request isn't there anymore", 410);
				await st.delete(k);
				if (!m.friends.includes(cmd.to)) m.friends.push(cmd.to);
				if (!other.friends.includes(cmd.uid)) { other.friends.push(cmd.uid); await st.put("u:" + cmd.to, other); }
				await saveMe();
				await inbox(cmd.to, { type: "friend", id: cmd.uid, name: m.name, accepted: true });
				return Response.json({ friends: true, id: cmd.to, name: other.name });
			}
			case "unfriend": { // both sides: the other app drops you too (an inbox event), and open requests go
				if (!/^[0-9a-f]{12}$/.test(cmd.to || "")) return err("Bad request");
				m.friends = m.friends.filter((f) => f !== cmd.to);
				await saveMe();
				const mine = (await st.get<any[]>("inbox:" + cmd.uid)) || [];
				if (mine.some((e) => e.type === "friend" && e.id === cmd.to)) await st.put("inbox:" + cmd.uid, mine.filter((e) => !(e.type === "friend" && e.id === cmd.to)));
				const other = await user(cmd.to);
				if (other) {
					other.friends = other.friends.filter((f) => f !== cmd.uid);
					await st.put("u:" + cmd.to, other);
					// a friend link they used but haven't heard about yet must not make you friends again
					const box = ((await st.get<any[]>("inbox:" + cmd.to)) || []).filter((e) => !(e.type === "friend" && e.id === cmd.uid));
					await st.put("inbox:" + cmd.to, box);
					await inbox(cmd.to, { type: "unfriend", id: cmd.uid, name: m.name });
				}
				await st.delete("fq:" + cmd.uid + ":" + cmd.to);
				await st.delete("fq:" + cmd.to + ":" + cmd.uid);
				return new Response("ok");
			}
			case "friendDecline": {
				await st.delete("fq:" + cmd.uid + ":" + cmd.to);
				return new Response("ok");
			}
			case "send": { // party invite to friends / teammates, in their app
				if (!(await cap("send:" + cmd.uid, 60, 3600_000))) return err("Too many invites this hour", 429);
				let sent = 0;
				for (const to of (cmd.to as string[]).slice(0, 50)) {
					if (to === cmd.uid || !((await mutual(m, cmd.uid, to)) || (await sharesTeam(m, to)))) continue;
					await inbox(to, { type: "party", from: cmd.uid, name: m.name, code: cmd.code, title: cmd.title || "" });
					sent++;
				}
				await saveMe();
				return Response.json({ sent });
			}
		}
		return err("Bad request");
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

type Team = { id: string; name: string; webhook: string; key: string };
type TeamRec = { name: string; webhook: string; key: string; created: number; owner?: string; members?: Record<string, number>; banned?: Record<string, number>; logo?: string };
type User = { secret: string; name: string; status?: any; seen: number; friends: string[]; teams: string[]; pic?: string };
const groupRoom = (env: Env) => env.ROOMS.get(env.ROOMS.idFromName("__group"));
const groupCmd = (env: Env, cmd: object) => groupRoom(env).fetch("https://room/internal/group", { method: "POST", body: JSON.stringify(cmd) });

async function sha256(s: string): Promise<string> {
	const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
	return [...new Uint8Array(d)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// Pictures: small square images the apps make themselves (128 px), as a data URL of at most ~48 KB. The
// version is the start of its sha256: the apps fetch a picture again only when it changed.
const PIC_RE = /^data:image\/(png|jpeg|webp);base64,[A-Za-z0-9+/]+={0,2}$/;
const picOK = (v: unknown) => v === "" || (typeof v === "string" && v.length <= 64_000 && PIC_RE.test(v));
const picVersion = async (data: string) => (await sha256(data)).slice(0, 10);

async function setPic(req: Request, env: Env): Promise<Response> {
	const body = await readJSON(req);
	if (!body || !picOK(body.pic ?? "")) return new Response("That picture isn't a small PNG, JPEG or WebP", { status: 400 });
	return forward(await groupCmd(env, { op: "setPic", ...who(body), pic: body.pic || "" }));
}

async function getPic(env: Env, kind: string, id: string, versioned: boolean): Promise<Response> {
	const res = await groupCmd(env, { op: "picGet", kind, id });
	if (!res.ok) return new Response("No picture", { status: 404, headers: { "cache-control": "public, max-age=300" } });
	const { data } = (await res.json()) as { data: string };
	const m = /^data:(image\/[a-z]+);base64,(.*)$/.exec(data);
	if (!m) return new Response("No picture", { status: 404 });
	const bytes = Uint8Array.from(atob(m[2]), (c) => c.charCodeAt(0));
	return new Response(bytes, { headers: { "content-type": m[1], "cache-control": versioned ? "public, max-age=31536000, immutable" : "public, max-age=300" } });
}

async function teamById(env: Env, id: string): Promise<Team | null> {
	if (!/^[A-Za-z0-9]{8}$/.test(id)) return null;
	const res = await groupCmd(env, { op: "teamGet", team: id });
	return res.ok ? { id, ...((await res.json()) as Omit<Team, "id">) } : null;
}

async function teamByKey(env: Env, key: unknown): Promise<Team | null> {
	if (typeof key !== "string" || key.length > 200) return null;
	const m = /^f1p_([A-Za-z0-9]{8})[A-Za-z0-9]{24}$/.exec(key);
	if (!m) return null;
	const t = await teamById(env, m[1]);
	return t && sameKey(key, t.key) ? t : null;
}

const readJSON = async (req: Request): Promise<any> => { try { return await req.json(); } catch { return null; } };
const forward = async (res: Response) => (res.ok ? res : new Response(await res.text(), { status: res.status }));

// The fields every signed-in call carries: the app's fixed id, its secret and its display name.
const who = (body: any) => ({ uid: String(body?.id || ""), secret: String(body?.secret || ""), name: clean(body?.name, 24) });

// What the app reports it's doing, kept to known fields.
function cleanStatus(s: any) {
	if (!s || (s.mode !== "host" && s.mode !== "guest")) return null;
	const code = String(s.code || "").toUpperCase();
	return { mode: s.mode, code: CODE_RE.test(code) ? code : "", title: clean(s.title, 80), live: !!s.live };
}

// Presence: report yourself, get your friends' and teammates' status, your teams and your inbox.
async function people(req: Request, env: Env): Promise<Response> {
	const body = await readJSON(req);
	if (!body) return new Response("Bad request", { status: 400 });
	const friends = (Array.isArray(body.friends) ? body.friends : []).map(String).filter((f: string) => /^[0-9a-f]{12}$/.test(f));
	const teams: string[] = [];
	const keyOf = new Map<string, string>();
	const lost: string[] = []; // keys that no longer work (team gone, or the owner removed you): the app drops them
	for (const key of (Array.isArray(body.teams) ? body.teams : []).slice(0, 20)) {
		const t = await teamByKey(env, key);
		if (t) { teams.push(t.id); keyOf.set(t.id, String(key)); } else lost.push(String(key));
	}
	const res = await groupCmd(env, { op: "presence", ...who(body), status: cleanStatus(body.status), friends, teams });
	if (!res.ok) return forward(res);
	const { removed, ...out } = (await res.json()) as { removed?: string[] };
	for (const id of removed || []) if (keyOf.has(id)) lost.push(keyOf.get(id)!);
	return Response.json({ ...out, lost }, { headers: { "cache-control": "no-store" } });
}

// A new team: name + the team's Discord webhook (checked with Discord first). Answers the team key.
async function teamCreate(req: Request, env: Env): Promise<Response> {
	const body = await readJSON(req);
	const teamName = clean(body?.teamName ?? body?.name, 32);
	const webhook = String(body?.webhook || "").trim();
	if (!teamName) return new Response("Give your team a name", { status: 400 });
	if (!/^https:\/\/(discord|discordapp|canary\.discord|ptb\.discord)\.com\/api\/webhooks\/\d+\/[A-Za-z0-9_-]+$/.test(webhook))
		return new Response("That isn't a Discord webhook URL", { status: 400 });
	if (env.SKIP_WEBHOOK_CHECK !== "1") {
		const check = await fetch(webhook);
		if (!check.ok) return new Response("Discord doesn't know that webhook (deleted?)", { status: 400 });
	}
	const res = await groupCmd(env, {
		op: "teamCreate", ...who(body), teamName, ip: req.headers.get("cf-connecting-ip") || "",
		webhook: webhook.replace(/^https:\/\/(discordapp|canary\.discord|ptb\.discord)\.com/, "https://discord.com"),
	});
	if (!res.ok) return forward(res);
	const { key } = (await res.json()) as { key: string };
	return Response.json({ key, name: teamName });
}

// Team actions that need both the team key (proof you're in it) and who you are.
async function teamAction(req: Request, env: Env, op: string): Promise<Response> {
	const body = await readJSON(req);
	const team = await teamByKey(env, body?.key);
	if (!team) return new Response("Unknown team key", { status: 403 });
	const extra: any = {};
	if (op === "teamRename") { extra.teamName = clean(body.teamName, 32); if (!extra.teamName) return new Response("Give your team a name", { status: 400 }); }
	if (op === "teamInviteFriend") extra.to = String(body.to || "");
	if (op === "teamKick") extra.member = String(body.member || "");
	if (op === "teamLogo") { if (!picOK(body.logo ?? "")) return new Response("That logo isn't a small PNG, JPEG or WebP", { status: 400 }); extra.logo = body.logo || ""; }
	return forward(await groupCmd(env, { op, ...who(body), team: team.id, ...extra }));
}

async function teamInfo(req: Request, env: Env): Promise<Response> {
	const team = await teamByKey(env, (await readJSON(req))?.key);
	return team ? Response.json({ name: team.name }) : new Response("Unknown team key", { status: 403 });
}

async function friendLink(req: Request, env: Env, origin: string): Promise<Response> {
	const res = await groupCmd(env, { op: "friendLink", ...who(await readJSON(req)) });
	if (!res.ok) return forward(res);
	return Response.json({ url: `${origin}/f1party/friend/${((await res.json()) as { token: string }).token}` });
}

async function friendRedeem(req: Request, env: Env): Promise<Response> {
	const body = await readJSON(req);
	const token = String(body?.token || "");
	if (!/^[A-Za-z0-9]{12,32}$/.test(token)) return new Response("Bad request", { status: 400 });
	return forward(await groupCmd(env, { op: "friendRedeem", ...who(body), token }));
}

// Friend requests between teammates: ask, accept, decline (body: id, secret, name, to).
async function friendOp(req: Request, env: Env, op: string): Promise<Response> {
	const body = await readJSON(req);
	const to = String(body?.to || "");
	if (!/^[0-9a-f]{12}$/.test(to)) return new Response("Bad request", { status: 400 });
	return forward(await groupCmd(env, { op, ...who(body), to }));
}

async function sendInvites(req: Request, env: Env): Promise<Response> {
	const body = await readJSON(req);
	const code = String(body?.code || "").toUpperCase();
	if (!CODE_RE.test(code)) return new Response("Bad request", { status: 400 });
	const to = (Array.isArray(body.to) ? body.to : []).map(String).filter((f: string) => /^[0-9a-f]{12}$/.test(f));
	return forward(await groupCmd(env, { op: "send", ...who(body), to, code, title: clean(body.title, 80) }));
}

async function postToDiscord(req: Request, env: Env, origin: string): Promise<Response> {
	const body = await readJSON(req);
	const team = await teamByKey(env, body?.key);
	if (!team) return new Response("Unknown team key", { status: 403 });
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
	const refused = (await check(code, kind, kind === "invite")) || (await check("__global:" + team.id, "global"));
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
	const res = await fetch(team.webhook + "?wait=true", {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify({ username: "F1 Party", content: text, allowed_mentions: { parse: [] } }),
	});
	if (!res.ok) return new Response("Discord: " + res.status, { status: 502 });
	return new Response("ok");
}

// Team links: anyone in a team makes a link (valid 7 days, 5 uses); opening it starts F1 Party
// (f1party://group/TOKEN), which trades the token here for the team key. The link never holds the key.
async function groupInvite(req: Request, env: Env, origin: string): Promise<Response> {
	const team = await teamByKey(env, (await readJSON(req))?.key);
	if (!team) return new Response("Unknown team key", { status: 403 });
	const res = await groupCmd(env, { op: "create", team: team.id });
	if (!res.ok) return new Response(await res.text(), { status: res.status });
	const { token } = (await res.json()) as { token: string };
	return Response.json({ url: `${origin}/f1party/group/${token}` });
}

async function groupRedeem(req: Request, env: Env): Promise<Response> {
	const body = await readJSON(req);
	const token = String(body?.token || "");
	if (!/^[A-Za-z0-9]{12,32}$/.test(token)) return new Response("Bad request", { status: 400 });
	const res = await groupCmd(env, { op: "redeem", token, uid: String(body?.id || "") });
	if (!res.ok) return new Response(await res.text(), { status: res.status });
	const team = await teamById(env, ((await res.json()) as { team: string }).team);
	if (!team) return new Response("That team doesn't exist any more", { status: 410 });
	return Response.json({ key: team.key, name: team.name }, { headers: { "cache-control": "no-store" } });
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

function friendPage(token: string): Response {
	const app = `f1party://friend/${token}`;
	const html = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Watch F1 with me</title>
<meta property="og:title" content="Watch F1 with me in F1 Party">
<meta property="og:description" content="Add me as a friend: see what I'm watching and join in one click.">
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0c0d10;color:#eef0f3;font:15px/1.5 system-ui,sans-serif}
main{max-width:400px;padding:24px;text-align:center}h1{font-size:22px;margin:0 0 10px}
a.btn{display:inline-block;background:#e10600;color:#fff;text-decoration:none;font-weight:600;padding:11px 18px;border-radius:7px;margin:8px 0}
p{color:#8a909c}a{color:#ff7a72}</style></head><body><main>
<h1>Watch F1 with me</h1>
<p>F1 Party lets us watch F1 in sync in MultiViewer. This link adds me as your friend, so you see what I'm watching and can join in one click.</p>
<a class="btn" href="${app}">Open in F1 Party</a>
<p>No F1 Party yet? <a href="https://github.com/Jeremy0538/f1party/releases/latest">Download it</a>, start it once, then click the button again.</p>
<p>The link works for 7 days.</p>
</main><script>location.href=${JSON.stringify(app)};</script></body></html>`;
	return new Response(html, { headers: { "content-type": "text/html; charset=utf-8", "cache-control": "no-store", "x-robots-tag": "noindex" } });
}
