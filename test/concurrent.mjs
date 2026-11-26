// Sends many requests at once through a real CPA and checks that every one is
// accounted for. This is the check that the plugin survives the concurrency a
// proxy actually sees.
import { mkdir, writeFile, copyFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";
import { createServer } from "node:http";

const cpa = resolve(process.argv[2] ?? "");
const library = resolve(process.argv[3] ?? "");
const root = resolve(process.argv[4] ?? ".work/concurrent");
const TOTAL = Number(process.argv[5] ?? "200");
const CONCURRENCY = Number(process.argv[6] ?? "20");

const MODEL = "claude-sonnet-4-5";
const KEY = "concurrent-key";
const CLIENT = "concurrent-client";

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
				usage: { prompt_tokens: 500, completion_tokens: 10, total_tokens: 510 },
			}));
		});
	});
	return new Promise(done => server.listen(0, "127.0.0.1", () => done({ server, port: server.address().port })));
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
	`host: "127.0.0.1"`, `port: ${port}`,
	`remote-management:`, `  allow-remote: false`, `  secret-key: "${KEY}"`,
	`auth-dir: "${auths.replaceAll("\\", "/")}"`,
	`access:`, `  api-keys:`, `    - "${CLIENT}"`,
	`api-keys:`, `  openai-compatibility:`, `    - name: "conc"`,
	`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
	`      keys:`, `        - api-key: "conc-upstream"`,
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
for (let attempt = 0; attempt < 200; attempt++) {
	try {
		const res = await fetch(`${base}/v0/management/cpa-context/sessions`, { headers: { Authorization: `Bearer ${KEY}` } });
		if (res.ok) break;
	} catch { }
	await new Promise(r => setTimeout(r, 150));
}

let ok = 0;
let failed = 0;
const sessions = 8;
let next = 0;
const started = Date.now();

async function worker() {
	while (true) {
		const i = next++;
		if (i >= TOTAL) return;
		const session = `conc-${i % sessions}`;
		try {
			const res = await fetch(`${base}/v1/chat/completions`, {
				method: "POST",
				headers: { "content-type": "application/json", Authorization: `Bearer ${CLIENT}`, "x-session-id": session },
				body: JSON.stringify({
					model: MODEL, max_tokens: 64,
					messages: [{ role: "user", content: `concurrent request ${i}` }],
				}),
			});
			await res.text();
			if (res.ok) ok++; else failed++;
		} catch {
			failed++;
		}
	}
}

await Promise.all(Array.from({ length: CONCURRENCY }, worker));
const elapsed = Date.now() - started;

// Usage arrives on its own path, after the response the client already got, and
// reports back up when the proxy is busy. Poll until the count stops moving
// rather than guessing a delay.
const readSessions = () => fetch(`${base}/v0/management/cpa-context/sessions`, {
	headers: { Authorization: `Bearer ${KEY}` },
}).then(r => r.json());

let rows = [];
let accounted = 0;
let stable = 0;
for (let attempt = 0; attempt < 100; attempt++) {
	await new Promise(r => setTimeout(r, 250));
	const list = await readSessions();
	rows = list.sessions ?? [];
	const total = rows.reduce((sum, s) => sum + (s.requests ?? 0), 0);
	if (total === accounted && total >= ok) break;
	stable = total === accounted ? stable + 1 : 0;
	accounted = total;
}
const settleMs = 0;

let projectionFailures = 0;
for (const row of rows) {
	const res = await fetch(`${base}/v0/management/cpa-context/session?session=${encodeURIComponent(row.id)}`, {
		headers: { Authorization: `Bearer ${KEY}` },
	});
	if (!res.ok) {
		projectionFailures++;
		continue;
	}
	const body = await res.json();
	if (!body.contextTimeline?.ok) projectionFailures++;
}

console.log(`sent ${TOTAL} at concurrency ${CONCURRENCY} in ${elapsed} ms: ok=${ok} failed=${failed}`);
console.log(`accounted requests: ${accounted}, conversations: ${rows.length}`);
console.log(`projection failures: ${projectionFailures}`);

child.kill();
backend.server.close();

if (failed > 0 || accounted !== ok || projectionFailures > 0) {
	console.error("FAIL: requests were lost or a projection did not build");
	process.exit(1);
}
console.log("ok: every request accounted for under concurrency");
