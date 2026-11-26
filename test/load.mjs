// Pushes a large conversation through the plugin and reports what the process
// costs: resident memory and CPU time. This is the check that the plugin does
// not hang the proxy on real traffic.
import { copyFile, mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";
import { createServer } from "node:http";

const cpa = resolve(process.argv[2] ?? "");
const library = resolve(process.argv[3] ?? "");
const root = resolve(process.argv[4] ?? ".work/load");
const ROUNDS = Number(process.argv[5] ?? "40");

const MODEL = "claude-sonnet-4-5";
const KEY = "load-key";
const CLIENT = "load-client";

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
				usage: { prompt_tokens: 100000, completion_tokens: 50, total_tokens: 100050 },
			}));
		});
	});
	return new Promise(done => server.listen(0, "127.0.0.1", () => done({ server, port: server.address().port })));
}

// A conversation that grows like a real agent loop: every round adds a tool
// call, its result and a reply, and the body carries the entire history.
function turn(round) {
	const messages = [{ role: "system", content: "You are a careful engineer. ".repeat(80) }];
	messages.push({ role: "user", content: "Investigate the parser and fix the bug in the scheduler." });
	const filler = "The reasoning continues at length because the parser is subtle and the scheduler state is easy to miss. ".repeat(30);
	for (let i = 0; i < round; i++) {
		messages.push({
			role: "assistant", content: null,
			tool_calls: [{
				id: `c${i}`, type: "function",
				function: { name: "read", arguments: JSON.stringify({ file_path: `internal/pkg/file${i}.go` }) },
			}],
		});
		messages.push({ role: "tool", tool_call_id: `c${i}`, name: "read", content: `${filler} ${i}` });
		messages.push({ role: "assistant", content: `Step ${i} done. ${filler}` });
		messages.push({ role: "user", content: `next: continue with ${i}` });
	}
	return messages;
}

async function rss(pid) {
	try {
		const { execFile } = await import("node:child_process");
		return await new Promise(done => {
			execFile("pwsh", ["-NoProfile", "-Command",
				`$p=Get-Process -Id ${pid} -ErrorAction SilentlyContinue; if ($p) { "$($p.WorkingSet64) $($p.CPU)" } else { "0 0" }`],
				(error, stdout) => done(error ? null : stdout.trim()));
		});
	} catch {
		return null;
	}
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
const configPath = join(root, "config.yaml");
await writeFile(configPath, [
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
	`    - name: "load"`,
	`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
	`      keys:`,
	`        - api-key: "load-upstream"`,
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

const child = spawn(cpa, ["--config", configPath], { stdio: ["ignore", "pipe", "pipe"] });
child.stdout.on("data", () => { });
child.stderr.on("data", () => { });
const base = `http://127.0.0.1:${port}`;
for (let attempt = 0; attempt < 160; attempt++) {
	try {
		const res = await fetch(`${base}/v0/management/cpa-context/sessions`, { headers: { Authorization: `Bearer ${KEY}` } });
		if (res.ok) break;
	} catch { }
	await new Promise(r => setTimeout(r, 150));
}

const before = await rss(child.pid);
const started = Date.now();
let sent = 0;
for (let round = 0; round < ROUNDS; round++) {
	const body = JSON.stringify({ model: MODEL, max_tokens: 4096, messages: turn(round) });
	const res = await fetch(`${base}/v1/chat/completions`, {
		method: "POST",
		headers: { "content-type": "application/json", Authorization: `Bearer ${CLIENT}`, "x-session-id": "load-session" },
		body,
	});
	if (res.ok) sent++;
	const text = await res.text();
	if (res.ok && text.length < 2) console.error("empty response");
}
const elapsed = Date.now() - started;
await new Promise(r => setTimeout(r, 1500));
const after = await rss(child.pid);

function mib(value) {
	const n = Number(String(value ?? "0").split(" ")[0] ?? 0);
	return (n / 1024 / 1024).toFixed(0);
}
function cpu(value) {
	return String(value ?? "0").split(" ")[1] ?? "?";
}

console.log(`requests sent: ${sent} in ${elapsed} ms`);
console.log(`rss before: ${mib(before)} MiB, cpu ${cpu(before)} s`);
console.log(`rss after:  ${mib(after)} MiB, cpu ${cpu(after)} s`);

child.kill();
backend.server.close();
