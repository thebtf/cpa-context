// Prints the exact agentTeam and subagentCatalog the plugin returns for a
// conversation, so a client-side crash can be traced to the payload rather than
// guessed at.
//
//   node test/fleetpayload.mjs <cpa-executable> <plugin-library>

import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, mkdir, copyFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const KEY = "fleet-management-key";
const CLIENT = "fleet-client-key";
const MODEL = "fleet-model";

function freePort() {
	return new Promise(done => {
		const server = createServer();
		server.listen(0, "127.0.0.1", () => {
			const { port } = server.address();
			server.close(() => done(port));
		});
	});
}

function startBackend() {
	const server = createServer((req, res) => {
		req.on("data", () => { });
		req.on("end", () => {
			res.writeHead(200, { "content-type": "application/json" });
			res.end(JSON.stringify({
				id: "x", object: "chat.completion", model: MODEL,
				choices: [{ index: 0, message: { role: "assistant", content: "ok" }, finish_reason: "stop" }],
				usage: { prompt_tokens: 200, completion_tokens: 3, total_tokens: 203 },
			}));
		});
	});
	return new Promise(done => server.listen(0, "127.0.0.1", () => done({ server, port: server.address().port })));
}

const cpa = resolve(process.argv[2] ?? "");
const library = resolve(process.argv[3] ?? "");
const root = await mkdtemp(join(tmpdir(), "cpa-context-fleetpayload-"));
const plugins = join(root, "plugins");
const auths = join(root, "auths");
await mkdir(join(plugins, "windows", "amd64"), { recursive: true });
await mkdir(auths, { recursive: true });
await copyFile(library, join(plugins, "windows", "amd64", "cpa-context-v0.0.0.dll"));

const backend = await startBackend();
const port = await freePort();
await writeFile(join(root, "config.yaml"), [
	`host: "127.0.0.1"`, `port: ${port}`,
	`remote-management:`, `  secret-key: "${KEY}"`,
	`auth-dir: "${auths.replaceAll("\\", "/")}"`,
	`access:`, `  api-keys:`, `    - "${CLIENT}"`,
	`api-keys:`, `  openai-compatibility:`, `    - name: "f"`,
	`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
	`      keys:`, `        - api-key: "up"`,
	`      models:`, `        - name: "${MODEL}"`, `          alias: "${MODEL}"`,
	`usage-statistics-enabled: true`,
	`plugins:`, `  enabled: true`, `  dir: "${plugins.replaceAll("\\", "/")}"`,
	`  configs:`, `    cpa-context:`, `      enabled: true`,
	"",
].join("\n"));

const child = spawn(cpa, ["--config", join(root, "config.yaml")], { stdio: ["ignore", "pipe", "pipe"] });
child.stdout.on("data", () => { });
child.stderr.on("data", () => { });
const base = `http://127.0.0.1:${port}`;
const headers = { Authorization: `Bearer ${KEY}` };
for (let i = 0; i < 200; i++) {
	try {
		if ((await fetch(`${base}/v0/management/cpa-context/sessions`, { headers })).ok) break;
	} catch { }
	await new Promise(r => setTimeout(r, 150));
}

async function ask(session, content, parent) {
	const extra = { "X-Session-Id": session };
	if (parent !== undefined) {
		extra["X-Parent-Session-ID"] = parent;
		extra["X-Agent-Id"] = session;
	}
	const res = await fetch(`${base}/v1/chat/completions`, {
		method: "POST",
		headers: { "content-type": "application/json", Authorization: `Bearer ${CLIENT}`, ...extra },
		body: JSON.stringify({ model: MODEL, max_tokens: 8, messages: [{ role: "user", content }] }),
	});
	await res.text();
}

// One root, one child that names it, and one conversation with no session
// header at all — the shape the operator's fleet actually has.
await ask("fleet-lead", "plan the work");
await ask("fleet-child", "do one slice", "fleet-lead");
await fetch(`${base}/v1/chat/completions`, {
	method: "POST",
	headers: { "content-type": "application/json", Authorization: `Bearer ${CLIENT}` },
	body: JSON.stringify({ model: MODEL, max_tokens: 8, messages: [{ role: "user", content: "no session header" }] }),
}).then(r => r.text());
await new Promise(r => setTimeout(r, 2000));

const list = await fetch(`${base}/v0/management/cpa-context/sessions`, { headers }).then(r => r.json());
let failures = 0;
for (const row of list.sessions ?? []) {
	const detail = await fetch(`${base}/v0/management/cpa-context/session?session=${encodeURIComponent(row.id)}`, { headers })
		.then(r => r.json());
	const team = detail.agentTeam;
	const catalog = detail.subagentCatalog;
	console.log(`--- ${row.id}`);
	console.log("  agentTeam:", JSON.stringify(team));
	console.log("  subagentCatalog:", JSON.stringify(catalog));
	if (team === null || team === undefined) {
		console.error("  FAIL: agentTeam is null, the fleet crashes on members.length");
		failures++;
		continue;
	}
	if (!Array.isArray(team.members)) {
		console.error("  FAIL: agentTeam.members is not an array");
		failures++;
	}
	if (!Array.isArray(team.tasks)) {
		console.error("  FAIL: agentTeam.tasks is not an array, the graph reads tasks.length");
		failures++;
	}
	if (catalog !== null && catalog !== undefined && !Array.isArray(catalog)) {
		console.error("  FAIL: subagentCatalog is not an array");
		failures++;
	}

	// The fleet detail route carries the roster the client sizes. The client sums
	// name.length and description.length over every member without checking, so a
	// member without both fields crashes the view with "Cannot read properties of
	// undefined (reading 'length')".
	const member = await fetch(`${base}/v0/management/cpa-context/api/dsh-context/fleet`, {
		method: "POST",
		headers: { ...headers, "Content-Type": "application/json" },
		body: JSON.stringify({ sessionId: row.id }),
	}).then(r => r.json());
	const roster = member?.value?.roster;
	if (!Array.isArray(roster)) {
		console.error(`  FAIL: roster is not an array for ${row.id}: ${JSON.stringify(roster)}`);
		failures++;
	} else {
		console.log(`  roster: ${JSON.stringify(roster)}`);
		for (const entry of roster) {
			if (typeof entry?.name !== "string") {
				console.error("  FAIL: a roster member has no name string");
				failures++;
			}
			if (typeof entry?.description !== "string") {
				console.error("  FAIL: a roster member has no description string");
				failures++;
			}
		}
	}
}

child.kill();
backend.server.close();
await new Promise(r => setTimeout(r, 500));
await rm(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 }).catch(() => { });

if (failures > 0) {
	console.error(`${failures} payload check(s) failed`);
	process.exit(1);
}
console.log("ok: every session carries an array roster and an array task list");
