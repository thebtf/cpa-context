// Checks the three things the operator reported as missing, against a real CPA:
// a session is named after what the person asked rather than after a harness
// wrapper, the fleet lists a subagent under its parent, and the context browser
// can load a header.
//
//   node test/panels.mjs <cpa-executable> <plugin-library>

import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, mkdir, copyFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const KEY = "panels-management-key";
const CLIENT = "panels-client-key";
const MODEL = "panels-model";

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
				usage: { prompt_tokens: 400, completion_tokens: 5, total_tokens: 405 },
			}));
		});
	});
	return new Promise(done => server.listen(0, "127.0.0.1", () => done({ server, port: server.address().port })));
}

const cpa = resolve(process.argv[2] ?? "");
const library = resolve(process.argv[3] ?? "");

const root = await mkdtemp(join(tmpdir(), "cpa-context-panels-"));
const plugins = join(root, "plugins");
const auths = join(root, "auths");
const data = join(root, "data");
await mkdir(join(plugins, "windows", "amd64"), { recursive: true });
await mkdir(auths, { recursive: true });
await mkdir(data, { recursive: true });
await copyFile(library, join(plugins, "windows", "amd64", "cpa-context-v0.0.0.dll"));

const backend = await startBackend();
const port = await freePort();
await writeFile(join(root, "config.yaml"), [
	`host: "127.0.0.1"`, `port: ${port}`,
	`remote-management:`, `  secret-key: "${KEY}"`,
	`auth-dir: "${auths.replaceAll("\\", "/")}"`,
	`access:`, `  api-keys:`, `    - "${CLIENT}"`,
	`api-keys:`, `  openai-compatibility:`, `    - name: "p"`,
	`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
	`      keys:`, `        - api-key: "up"`,
	`      models:`, `        - name: "${MODEL}"`, `          alias: "${MODEL}"`,
	`usage-statistics-enabled: true`,
	`plugins:`, `  enabled: true`, `  dir: "${plugins.replaceAll("\\", "/")}"`,
	`  configs:`, `    cpa-context:`, `      enabled: true`,
	`      data_dir: "${data.replaceAll("\\", "/")}"`,
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

async function ask(session, content) {
	return fetch(`${base}/v1/chat/completions`, {
		method: "POST",
		headers: { "content-type": "application/json", Authorization: `Bearer ${CLIENT}`, "X-Session-Id": session },
		body: JSON.stringify({
			model: MODEL, max_tokens: 8,
			messages: [
				{ role: "system", content: "You are a careful engineer. Follow the instructions exactly." },
				{ role: "user", content },
			],
			tools: [{
				type: "function",
				function: {
					name: "bash", description: "Run a shell command",
					parameters: { type: "object", properties: { command: { type: "string" } } },
				},
			}],
		}),
	}).then(r => r.text());
}

// A prompt that opens with a harness wrapper, which is what named the sessions
// after the tag in the operator's screenshot.
await ask("panels-lead", "<system-reminder>\nInternal note the person never wrote.\n</system-reminder>\n\nInvestigate why the chart bars are flat.");
await ask("panels-child", "<system-reminder>\nAnother internal note.\n</system-reminder>\n\nDo one slice of the work.");
await new Promise(r => setTimeout(r, 2000));

const list = await fetch(`${base}/v0/management/cpa-context/sessions`, { headers }).then(r => r.json());
const rows = list.sessions ?? [];
let failures = 0;

const lead = rows.find(r => String(r.id).includes("panels-lead"));
if (lead === undefined) {
	console.error("FAIL: the lead conversation is missing");
	failures++;
} else {
	console.log("title:", JSON.stringify(lead.title));
	if (String(lead.title ?? "").includes("system-reminder")) {
		console.error("FAIL: the conversation is still named after the harness wrapper");
		failures++;
	}
	if (!String(lead.title ?? "").includes("chart bars")) {
		console.error("FAIL: the title does not carry what the person asked");
		failures++;
	}
}

// The session detail must carry the fleet keys and a header the browser can read.
const detail = await fetch(`${base}/v0/management/cpa-context/session?session=${encodeURIComponent(lead?.id ?? "")}`, { headers })
	.then(r => r.json());
const team = detail.agentTeam;
if (team === null || !Array.isArray(team?.members) || team.members.length === 0) {
	console.error("FAIL: agentTeam is empty, the fleet would draw nothing");
	failures++;
} else {
	console.log("fleet members:", team.members.map(m => `${m.name}:${m.role}`).join(", "));
}

const headerRows = await fetch(`${base}/v0/management/cpa-context/api/dsh-context/history`, {
	method: "POST",
	headers: { ...headers, "Content-Type": "application/json" },
	body: JSON.stringify({ sessionId: lead?.id ?? "", throughSeq: 1 }),
}).then(r => r.json());
const events = headerRows.value?.rows ?? [];
const headerEvent = events.find(r => r.type === "request/header");
if (headerEvent === undefined) {
	console.error("FAIL: no request/header event, the panel will say it cannot load a header");
	failures++;
} else {
	console.log("header tools:", (headerEvent.data?.header?.tools ?? []).length, "systemTokens:", headerEvent.data?.header?.systemTokens);
}
const systemEvent = events.find(r => r.type === "system/message");
if (systemEvent === undefined) {
	console.error("FAIL: no system/message event, the system prompt stays unreadable");
	failures++;
} else {
	const text = systemEvent.data?.message?.content?.[0]?.text ?? "";
	console.log("system prompt:", JSON.stringify(text.slice(0, 40)));
	if (!text.includes("careful engineer")) {
		console.error("FAIL: the system prompt event carries the wrong text");
		failures++;
	}
}

child.kill();
backend.server.close();
await new Promise(r => setTimeout(r, 500));
await rm(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 }).catch(() => { });

if (failures > 0) {
	console.error(`${failures} check(s) failed`);
	process.exit(1);
}
console.log("ok: titles, fleet and header content all resolve");
