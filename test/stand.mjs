// A live stand for the dashboard: boots CPA with the built plugin, feeds it a
// realistic conversation (growing history, tool calls with arguments, tool
// results, a cache hit), and leaves it running so the page can be opened in a
// browser.
import { copyFile, mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { basename, join, resolve } from "node:path";
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { tmpdir } from "node:os";

const cpa = resolve(process.argv[2] ?? "");
const library = resolve(process.argv[3] ?? "");
const keep = process.argv[4] ?? "";

const MODEL = "claude-sonnet-4-5";
const MANAGEMENT_KEY = "stand-management-key";
const CLIENT_KEY = "stand-client-key";

function freePort() {
	return new Promise(done => {
		const server = createServer();
		server.listen(0, "127.0.0.1", () => {
			const { port } = server.address();
			server.close(() => done(port));
		});
	});
}

// A backend that answers like a provider and reports usage counters, so the
// plugin has provider numbers to join with its own composition.
function startBackend() {
	const server = createServer((req, res) => {
		let body = "";
		req.on("data", chunk => { body += chunk; });
		req.on("end", () => {
			if (req.url?.includes("/chat/completions")) {
				res.writeHead(200, { "content-type": "application/json" });
				res.end(JSON.stringify({
					id: "chatcmpl-stand", object: "chat.completion", model: MODEL,
					choices: [{ index: 0, message: { role: "assistant", content: "Done." }, finish_reason: "stop" }],
					usage: { prompt_tokens: 1200, completion_tokens: 40, total_tokens: 1240, prompt_tokens_details: { cached_tokens: 900 } },
				}));
				return;
			}
			res.writeHead(404).end();
		});
	});
	return new Promise(done => {
		server.listen(0, "127.0.0.1", () => done({ server, port: server.address().port }));
	});
}

// One realistic agent turn: a user question, then repeated steps where the
// agent reads files, searches, edits, runs a command and reports back.
function conversation() {
	const messages = [{ role: "user", content: "Investigate why the chart bars are flat and fix the drawing code." }];
	const script = [
		{ tool: "read", args: { file_path: "D:/Dev/cliproxyapi-plugins/cpa-context/dashboard.go" }, out: "package main\n\nfunc dashboardHTML() string {\n\t...240 lines...\n}\n" },
		{ tool: "grep", args: { pattern: "drawChart", path: "cpa-context" }, out: "dashboard.go:349: function drawChart(s) {\ndashboard.go:415: ctx.fillText(\"hits\", 34, h - 6);\n" },
		{ tool: "edit", args: { file_path: "D:/Dev/cliproxyapi-plugins/cpa-context/dashboard.go", old_string: "const max = 1", new_string: "const max = 1\n  const bandTop = 2" }, out: "edited 1 file" },
		{ tool: "bash", args: { command: "go build ./... && go test ./..." }, out: "ok  github.com/thebtf/cpa-context  0.12s\n" },
	];

	// Chat format keeps a tool call and its result as separate messages, which is
	// what an agent loop actually sends.
	for (let round = 0; round < 6; round++) {
		for (const step of script) {
			const id = `call_${round}_${step.tool}`;
			messages.push({
				role: "assistant",
				content: null,
				tool_calls: [{ id, type: "function", function: { name: step.tool, arguments: JSON.stringify(step.args) } }],
			});
			messages.push({ role: "tool", tool_call_id: id, name: step.tool, content: step.out.repeat(6) });
		}
		messages.push({
			role: "assistant",
			content: "Round " + (round + 1) + ": the bars now carry a cache band, so a run of misses is visible.",
		});
	}
	return { messages };
}

async function main() {
	if (!process.argv[2] || !process.argv[3]) {
		throw new Error("usage: node test/stand.mjs <cpa-executable> <plugin-library>");
	}
	const root = await mkdtemp(join(tmpdir(), "cpa-context-stand-"));
	const plugins = join(root, "plugins");
	const auths = join(root, "auths");
	await mkdir(plugins, { recursive: true });
	await mkdir(auths, { recursive: true });
	await copyFile(library, join(plugins, basename(library)));

	const backend = await startBackend();
	const port = await freePort();
	const configPath = join(root, "config.yaml");
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
		`    - name: "stand"`,
		`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
		`      keys:`,
		`        - api-key: "stand-upstream-key"`,
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
		`      data_dir: "${join(root, "data").replaceAll("\\", "/")}"`,
		"",
	].join("\n"));

	const child = spawn(cpa, ["--config", configPath], { stdio: ["ignore", "pipe", "pipe"] });
	let log = "";
	child.stdout.on("data", c => { log += c; });
	child.stderr.on("data", c => { log += c; });

	const base = `http://127.0.0.1:${port}`;
	for (let attempt = 0; attempt < 120; attempt++) {
		try {
			const res = await fetch(`${base}/v0/management/cpa-context/overview`, {
				headers: { Authorization: `Bearer ${MANAGEMENT_KEY}` },
			});
			if (res.ok) break;
		} catch { }
		await new Promise(r => setTimeout(r, 250));
	}

	const { messages } = conversation();
	for (let i = 0; i < 12; i++) {
		const slice = messages.slice(0, Math.min(messages.length, 1 + i * 2));
		const res = await fetch(`${base}/v1/chat/completions`, {
			method: "POST",
			headers: {
				"content-type": "application/json",
				Authorization: `Bearer ${CLIENT_KEY}`,
				"x-session-id": "stand-session-1",
			},
			body: JSON.stringify({
				model: MODEL,
				max_tokens: 4096,
				stream: false,
				messages: [{ role: "system", content: "You are a careful engineer." }, ...slice],
			}),
		});
		if (!res.ok) {
			console.error(`request ${i} answered ${res.status}: ${(await res.text()).slice(0, 300)}`);
		}
	}

	const dashboard = `${base}/v0/resource/plugins/cpa-context/dashboard`;
	const page = await fetch(dashboard);
	console.log(`stand ready: ${base}`);
	console.log(`dashboard: ${dashboard}`);
	console.log(`management key: ${MANAGEMENT_KEY}`);
	console.log(`dashboard answered ${page.status}, ${(await page.text()).length} bytes`);

	if (keep === "keep") {
		console.log("running; press ctrl-c to stop");
		await new Promise(() => { });
	}
	child.kill();
	backend.server.close();
}

await main();
