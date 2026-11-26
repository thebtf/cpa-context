// Sends traffic over time and reports the journal size and the memory the
// process holds, so an unbounded file or a growing heap shows up as a number.
import { mkdir, stat, writeFile, copyFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";
import { createServer } from "node:http";

const cpa = resolve(process.argv[2] ?? "");
const library = resolve(process.argv[3] ?? "");
const root = resolve(process.argv[4] ?? ".work/journal");
const ROUNDS = Number(process.argv[5] ?? "200");

const MODEL = "claude-sonnet-4-5";
const KEY = "journal-key";
const CLIENT = "journal-client";

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
				usage: { prompt_tokens: 5000, completion_tokens: 20, total_tokens: 5020 },
			}));
		});
	});
	return new Promise(done => server.listen(0, "127.0.0.1", () => done({ server, port: server.address().port })));
}

async function fileSize(path) {
	try {
		return (await stat(path)).size;
	} catch {
		return -1;
	}
}

async function rss(pid) {
	const { execFile } = await import("node:child_process");
	return await new Promise(done => {
		execFile("pwsh", ["-NoProfile", "-Command",
			`$p=Get-Process -Id ${pid} -ErrorAction SilentlyContinue; if ($p) { "$($p.WorkingSet64) $($p.CPU)" } else { "0 0" }`],
			(error, stdout) => done(error ? "0 0" : stdout.trim()));
	});
}

const backend = await startBackend();
const plugins = join(root, "plugins");
const auths = join(root, "auths");
const data = join(root, "data");
await mkdir(plugins, { recursive: true });
await mkdir(auths, { recursive: true });
await mkdir(data, { recursive: true });
await copyFile(library, join(plugins, "cpa-context.dll"));

const port = await freePort();
await writeFile(join(root, "config.yaml"), [
	`host: "127.0.0.1"`,
	`port: ${port}`,
	`remote-management:`,
	`  allow-remote: false`,
	`  secret-key: "${KEY}"`,
	`auth-dir: "${auths.replaceAll("\\", "/")}"`,
	`access:`,
	`  api-keys:`,
	`    - "${CLIENT}"`,
	`api-keys:`,
	`  openai-compatibility:`,
	`    - name: "journal"`,
	`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
	`      keys:`,
	`        - api-key: "journal-upstream"`,
	`      models:`,
	`        - name: "${MODEL}"`,
	`          alias: "${MODEL}"`,
	`usage-statistics-enabled: true`,
	`plugins:`,
	`  enabled: true`,
	`  dir: "${plugins.replaceAll("\\", "/")}"`,
	`  configs:`,
	`    cpa-context:`,
	`      enabled: true`,
	`      data_dir: "${data.replaceAll("\\", "/")}"`,
	"",
].join("\n"));

const child = spawn(cpa, ["--config", join(root, "config.yaml")], { stdio: ["ignore", "pipe", "pipe"] });
child.stdout.on("data", () => { });
child.stderr.on("data", () => { });
const base = `http://127.0.0.1:${port}`;
for (let attempt = 0; attempt < 200; attempt++) {
	try {
		const res = await fetch(`${base}/v0/management/cpa-context/sessions`, { headers: { Authorization: `Bearer ${KEY}` } });
		if (res.ok) break;
	} catch { }
	await new Promise(r => setTimeout(r, 150));
}

const dbPath = join(data, "context.db");
const before = { size: await fileSize(dbPath), rss: await rss(child.pid) };

// Many conversations, each a handful of requests, so both the session cap and
// the snapshot cap get exercised.
for (let i = 0; i < ROUNDS; i++) {
	const session = `journal-${i % 40}`;
	await fetch(`${base}/v1/chat/completions`, {
		method: "POST",
		headers: { "content-type": "application/json", Authorization: `Bearer ${CLIENT}`, "x-session-id": session },
		body: JSON.stringify({
			model: MODEL, max_tokens: 1024,
			messages: [{ role: "user", content: `request ${i} for ${session}` }],
		}),
	}).then(r => r.text());
}

await new Promise(r => setTimeout(r, 1500));
const after = { size: await fileSize(dbPath), rss: await rss(child.pid) };

const sessions = await fetch(`${base}/v0/management/cpa-context/sessions`, { headers: { Authorization: `Bearer ${KEY}` } })
	.then(r => r.json());

console.log(`requests sent: ${ROUNDS} across 40 conversations`);
console.log(`journal bytes: ${before.size} -> ${after.size}`);
console.log(`process rss/cpu: ${before.rss} -> ${after.rss}`);
console.log(`conversations reported: ${sessions.sessions?.length ?? 0}`);

child.kill();
backend.server.close();
