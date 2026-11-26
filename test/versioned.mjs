// Checks what CPA reports for a plugin whose file carries a version in its name.
//
// CPA's discovery strips "-v<version>" from the library filename and takes the
// rest as the plugin id, and the version it strips is what the host matches
// against the configured version. This runs the real binary and prints what the
// host resolved, so the install layout is confirmed rather than assumed.
//
//   node test/versioned.mjs <cpa-executable> <plugin-library> <file-name>

import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, mkdir, copyFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve, extname } from "node:path";

const KEY = "versioned-management-key";
const CLIENT = "versioned-client-key";

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
				id: "x", object: "chat.completion", model: "m",
				choices: [{ index: 0, message: { role: "assistant", content: "ok" }, finish_reason: "stop" }],
				usage: { prompt_tokens: 321, completion_tokens: 5, total_tokens: 326 },
			}));
		});
	});
	return new Promise(done => server.listen(0, "127.0.0.1", () => done({ server, port: server.address().port })));
}

const cpa = resolve(process.argv[2] ?? "");
const library = resolve(process.argv[3] ?? "");
const fileName = process.argv[4] ?? `cpa-context-v0.0.0${extname(library) || ".dll"}`;
const expectedVersion = process.argv[5] ?? "";

// CPA looks in plugins/<goos>/<goarch>/ first, so the probe installs there for
// the platform it runs on rather than assuming one.
const goos = process.platform === "win32" ? "windows" : process.platform === "darwin" ? "darwin" : "linux";
const goarch = process.arch === "arm64" ? "arm64" : "amd64";

const root = await mkdtemp(join(tmpdir(), "cpa-context-versioned-"));
const plugins = join(root, "plugins");
const auths = join(root, "auths");
await mkdir(join(plugins, goos, goarch), { recursive: true });
await mkdir(auths, { recursive: true });
await copyFile(library, join(plugins, goos, goarch, fileName));

const backend = await startBackend();
const port = await freePort();
const configPath = join(root, "config.yaml");
await writeFile(configPath, [
	`host: "127.0.0.1"`, `port: ${port}`,
	`remote-management:`, `  secret-key: "${KEY}"`,
	`auth-dir: "${auths.replaceAll("\\", "/")}"`,
	`access:`, `  api-keys:`, `    - "${CLIENT}"`,
	`api-keys:`, `  openai-compatibility:`, `    - name: "v"`,
	`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
	`      keys:`, `        - api-key: "up"`,
	`      models:`, `        - name: "m"`, `          alias: "m"`,
	`usage-statistics-enabled: true`,
	`plugins:`, `  enabled: true`, `  dir: "${plugins.replaceAll("\\", "/")}"`,
	`  configs:`, `    cpa-context:`, `      enabled: true`,
	"",
].join("\n"));

const child = spawn(cpa, ["--config", configPath], { stdio: ["ignore", "pipe", "pipe"] });
let log = "";
child.stdout.on("data", c => { log += c; });
child.stderr.on("data", c => { log += c; });

const base = `http://127.0.0.1:${port}`;
const headers = { Authorization: `Bearer ${KEY}` };
for (let i = 0; i < 200; i++) {
	try {
		const res = await fetch(`${base}/v0/management/plugins`, { headers });
		if (res.ok) break;
	} catch { }
	await new Promise(r => setTimeout(r, 150));
}

// A request makes the plugin itself answer, which is the point: a misresolved
// id would leave the routes unanswered.
await fetch(`${base}/v1/chat/completions`, {
	method: "POST",
	headers: { "content-type": "application/json", Authorization: `Bearer ${CLIENT}` },
	body: JSON.stringify({ model: "m", max_tokens: 8, messages: [{ role: "user", content: "hello there" }] }),
}).then(r => r.text());
await new Promise(r => setTimeout(r, 1200));

const list = await fetch(`${base}/v0/management/plugins`, { headers }).then(r => r.json());
const entry = (list.plugins ?? []).find(p => p.id === "cpa-context");
console.log("file on disk:", fileName);
console.log("resolved id:", entry?.id ?? "(none)");
console.log("resolved path:", entry?.path ?? "(none)");
console.log("metadata version:", entry?.metadata?.version ?? "(none)");
console.log("registered:", entry?.registered, "effective_enabled:", entry?.effective_enabled);

const sessions = await fetch(`${base}/v0/management/cpa-context/sessions`, { headers })
	.then(r => r.status)
	.catch(() => 0);
console.log("dashboard route status:", sessions);

const loaded = log.split("\n").filter(l => l.includes("pluginhost") && l.includes("cpa-context")).slice(0, 3);
for (const line of loaded) console.log("log:", line.trim());

child.kill();
backend.server.close();
// The host still holds its auth database for a moment after the kill, and a
// locked file must not hide the result the check already produced.
await new Promise(r => setTimeout(r, 500));
await rm(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 }).catch(() => { });

if (!entry || entry.id !== "cpa-context" || sessions !== 200) {
	console.error("FAIL: the versioned file name did not resolve to the plugin id");
	process.exit(1);
}
// The host reports two versions: the one in the file name, and the one the
// plugin declares about itself. A release where they disagree shows the operator
// one number in the list and another in the logs.
if (expectedVersion) {
	const fromName = fileName.match(/-v(.+)\.[^.]+$/)?.[1] ?? "";
	const fromMetadata = entry?.metadata?.version ?? "";
	if (fromName !== expectedVersion) {
		console.error(`FAIL: the file name says ${fromName}, the release is ${expectedVersion}`);
		process.exit(1);
	}
	if (fromMetadata !== expectedVersion) {
		console.error(`FAIL: the plugin declares ${fromMetadata}, the release is ${expectedVersion}`);
		process.exit(1);
	}
}
console.log("ok: a versioned file name resolves to the plugin id");
