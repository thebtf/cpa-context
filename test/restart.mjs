// Proves the journal survives a real restart of the proxy: it starts CPA on a
// fixed data directory, sends traffic, stops CPA, starts it again on the same
// directory, and reads the sessions back.
import { copyFile, mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { existsSync } from "node:fs";

const cpa = resolve(process.argv[2] ?? "");
const library = resolve(process.argv[3] ?? "");
const root = resolve(process.argv[4] ?? ".work/restart");

const MODEL = "claude-sonnet-4-5";
const MANAGEMENT_KEY = "restart-key";
const CLIENT_KEY = "restart-client";

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
				id: "chatcmpl-restart", object: "chat.completion", model: MODEL,
				choices: [{ index: 0, message: { role: "assistant", content: "Done." }, finish_reason: "stop" }],
				usage: { prompt_tokens: 800, completion_tokens: 30, total_tokens: 830 },
			}));
		});
	});
	return new Promise(done => {
		server.listen(0, "127.0.0.1", () => done({ server, port: server.address().port }));
	});
}

async function boot(backend, dataDir) {
	const plugins = join(root, "plugins");
	const auths = join(root, "auths");
	await mkdir(plugins, { recursive: true });
	await mkdir(auths, { recursive: true });
	await copyFile(library, join(plugins, "cpa-context.dll"));
	const port = await freePort();
	const configPath = join(root, `config-${port}.yaml`);
	await writeFile(configPath, [
		`host: "127.0.0.1"`,
		`port: ${port}`,
		`remote-management:`,
		`  allow-remote: false`,
		`  secret-key: "${MANAGEMENT_KEY}"`,
		`auth-dir: "${auths.replaceAll("\\", "/")}"`,
		`access:`,
		`  api-keys:`,
		`    - "${CLIENT_KEY}"`,
		`api-keys:`,
		`  openai-compatibility:`,
		`    - name: "restart"`,
		`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
		`      keys:`,
		`        - api-key: "restart-upstream"`,
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
		`      data_dir: "${dataDir.replaceAll("\\", "/")}"`,
		"",
	].join("\n"));
	const child = spawn(cpa, ["--config", configPath], { stdio: ["ignore", "pipe", "pipe"] });
	child.stdout.on("data", () => { });
	child.stderr.on("data", () => { });
	const base = `http://127.0.0.1:${port}`;
	for (let attempt = 0; attempt < 160; attempt++) {
		try {
			const res = await fetch(`${base}/v0/management/cpa-context/sessions`, {
				headers: { Authorization: `Bearer ${MANAGEMENT_KEY}` },
			});
			if (res.ok) return { child, base };
		} catch { }
		await new Promise(r => setTimeout(r, 150));
	}
	throw new Error("CPA did not start");
}

async function sessions(base) {
	const res = await fetch(`${base}/v0/management/cpa-context/sessions`, {
		headers: { Authorization: `Bearer ${MANAGEMENT_KEY}` },
	});
	const body = await res.json();
	return body.sessions ?? [];
}

async function send(base, text) {
	const res = await fetch(`${base}/v1/chat/completions`, {
		method: "POST",
		headers: {
			"content-type": "application/json",
			Authorization: `Bearer ${CLIENT_KEY}`,
			"x-session-id": "restart-session",
		},
		body: JSON.stringify({ model: MODEL, max_tokens: 2048, messages: [{ role: "user", content: text }] }),
	});
	return res.status;
}

const backend = await startBackend();
const dataDir = join(root, "data");
await mkdir(dataDir, { recursive: true });

const first = await boot(backend, dataDir);
await send(first.base, "first conversation before the restart");
await new Promise(r => setTimeout(r, 600));
const before = await sessions(first.base);
first.child.kill();
await new Promise(r => setTimeout(r, 1500));

const dbFile = join(dataDir, "context.db");
console.log(`database: ${dbFile} exists=${existsSync(dbFile)}`);

const second = await boot(backend, dataDir);
const after = await sessions(second.base);
console.log(`before restart: ${before.length} conversation(s)`);
console.log(`after restart:  ${after.length} conversation(s)`);
if (after.length > 0) console.log(`first after restart: ${JSON.stringify(after[0]).slice(0, 240)}`);
second.child.kill();
backend.server.close();

if (after.length === 0 || before.length === 0) {
	console.error("FAIL: the conversation did not survive the restart");
	process.exit(1);
}
console.log("ok: the conversation survived the restart");
