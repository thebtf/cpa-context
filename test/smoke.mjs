// End-to-end check of the plugin against a real CLIProxyAPI binary.
//
// It starts a fake model backend on loopback, writes a throwaway CPA config that
// loads this plugin, runs one chat request, and then asserts that the dashboard
// payload reports a composition that sums to the prompt tokens the backend
// reported. Synthetic requests only: no provider accounts, no installed profile.
//
//   node test/smoke.mjs <cpa-executable> <plugin-library>

import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, mkdir, copyFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, basename, resolve } from "node:path";

const MANAGEMENT_KEY = "smoke-management-key";
const CLIENT_KEY = "smoke-client-key";
const MODEL = "smoke-model";

function fail(message) {
	console.error("FAIL: " + message);
	process.exitCode = 1;
	throw new Error(message);
}

function startBackend() {
	const server = createServer((req, res) => {
		let body = "";
		req.on("data", chunk => {
			body += chunk;
		});
		req.on("end", () => {
			if (req.url === "/v1/models") {
				res.writeHead(200, { "Content-Type": "application/json" });
				res.end(JSON.stringify({ data: [{ id: MODEL, object: "model" }] }));
				return;
			}
			// Prompt tokens track body size so the assertion has a known shape.
			const promptTokens = Math.max(64, Math.floor(body.length / 3));
			const outputTokens = 7;
			res.writeHead(200, { "Content-Type": "application/json" });
			res.end(
				JSON.stringify({
					id: "chatcmpl-smoke",
					object: "chat.completion",
					model: MODEL,
					choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: "ok" } }],
					usage: {
						prompt_tokens: promptTokens,
						completion_tokens: outputTokens,
						total_tokens: promptTokens + outputTokens,
					},
				}),
			);
		});
	});
	return new Promise(resolvePromise => {
		server.listen(0, "127.0.0.1", () => resolvePromise({ server, port: server.address().port }));
	});
}

function freePort() {
	return new Promise((resolvePromise, reject) => {
		const probe = createServer();
		probe.once("error", reject);
		probe.listen(0, "127.0.0.1", () => {
			const { port } = probe.address();
			probe.close(() => resolvePromise(port));
		});
	});
}

async function waitFor(url, options, deadlineMs) {
	const started = Date.now();
	while (Date.now() - started < deadlineMs) {
		try {
			const res = await fetch(url, options);
			if (res.ok) return res;
		} catch {
			// The server is still starting.
		}
		await new Promise(r => setTimeout(r, 250));
	}
	fail("timed out waiting for " + url);
}

async function main() {
	const cpa = resolve(process.argv[2] ?? "");
	const library = resolve(process.argv[3] ?? "");
	if (!process.argv[2] || !process.argv[3]) {
		fail("usage: node test/smoke.mjs <cpa-executable> <plugin-library>");
	}

	const root = await mkdtemp(join(tmpdir(), "cpa-context-smoke-"));
	const plugins = join(root, "plugins");
	const auths = join(root, "auths");
	await mkdir(plugins, { recursive: true });
	await mkdir(auths, { recursive: true });
	await copyFile(library, join(plugins, basename(library)));

	const backend = await startBackend();
	const port = await freePort();

	const configPath = join(root, "config.yaml");
	await writeFile(
		configPath,
		[
			`host: "127.0.0.1"`,
			`port: ${port}`,
			`remote-management:`,
			`  allow-remote: false`,
			`  secret-key: "${MANAGEMENT_KEY}"`,
			`auth-dir: "${auths.replaceAll("\\", "/")}"`,
			// Two roots share the name api-keys in CPA v8: client authentication
			// lives under access.api-keys, upstream API-key providers under
			// api-keys. The released layout nests compatibility providers there.
			`access:`,
			`  api-keys:`,
			`    - "${CLIENT_KEY}"`,
			`api-keys:`,
			`  openai-compatibility:`,
			`    - name: "smoke"`,
			`      base-url: "http://127.0.0.1:${backend.port}/v1"`,
			`      keys:`,
			`        - api-key: "smoke-upstream-key"`,
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
			"",
		].join("\n"),
	);

	const child = spawn(cpa, ["--config", configPath], { stdio: ["ignore", "pipe", "pipe"] });
	let log = "";
	child.stdout.on("data", chunk => {
		log += chunk;
	});
	child.stderr.on("data", chunk => {
		log += chunk;
	});

	const cleanup = async () => {
		const exited = new Promise(done => child.once("exit", done));
		child.kill();
		await Promise.race([exited, new Promise(r => setTimeout(r, 10_000))]);
		backend.server.close();
		// CPA holds its sqlite handle briefly past exit on Windows; retry the
		// removal instead of failing the run on a busy file.
		for (let attempt = 0; attempt < 20; attempt++) {
			try {
				await rm(root, { recursive: true, force: true });
				return;
			} catch (error) {
				if (attempt === 19) {
					console.warn("could not remove the temporary directory: " + error.message);
					return;
				}
				await new Promise(r => setTimeout(r, 250));
			}
		}
	};

	try {
		const base = `http://127.0.0.1:${port}`;
		await waitFor(
			`${base}/v1/models`,
			{ headers: { Authorization: `Bearer ${CLIENT_KEY}` } },
			60_000,
		);

		if (!log.includes("plugin registered")) {
			fail("the plugin did not register with CPA:\n" + log);
		}

		const request = {
			model: MODEL,
			messages: [
				{ role: "system", content: "You are a smoke test harness." },
				{ role: "user", content: "Describe the module layout in one sentence." },
				{
					role: "assistant",
					content: null,
					tool_calls: [
						{ id: "c1", type: "function", function: { name: "bash", arguments: '{"command":"ls"}' } },
					],
				},
				{ role: "tool", tool_call_id: "c1", name: "bash", content: "main.go go.mod" },
			],
			tools: [
				{
					type: "function",
					function: {
						name: "bash",
						description: "Run a shell command",
						parameters: { type: "object", properties: { command: { type: "string" } } },
					},
				},
			],
		};

		const chat = await fetch(`${base}/v1/chat/completions`, {
			method: "POST",
			headers: {
				Authorization: `Bearer ${CLIENT_KEY}`,
				"Content-Type": "application/json",
				"X-Session-Id": "smoke-session",
			},
			body: JSON.stringify(request),
		});
		if (!chat.ok) {
			let detail = "";
			try {
				detail = await chat.text();
			} catch {
				detail = "<no readable body>";
			}
			fail(`chat completion answered ${chat.status}: ${detail}`);
		}

		const overview = await waitFor(
			`${base}/v0/management/cpa-context/overview`,
			{ headers: { Authorization: `Bearer ${MANAGEMENT_KEY}` } },
			20_000,
		).then(res => res.json());

		const sessions = overview.sessions ?? [];
		if (sessions.length !== 1) {
			fail(`expected one conversation, saw ${sessions.length}`);
		}
		const session = sessions[0];
		if (!session.calibrated) {
			fail("the composition was not calibrated against provider counters");
		}
		const sum = Object.values(session.composition ?? {}).reduce((a, b) => a + b, 0);
		if (sum !== session.inputTokens) {
			fail(`composition sums to ${sum}, but the provider billed ${session.inputTokens}`);
		}
		for (const category of ["system", "tools", "user", "toolCall", "toolResult"]) {
			if (!session.composition?.[category]) {
				fail(`category ${category} is missing from the composition: ${JSON.stringify(session.composition)}`);
			}
		}
		if (!Array.isArray(session.lastItems) || session.lastItems.length === 0) {
			fail("the newest request carried no expandable items");
		}

		const dashboard = await fetch(`${base}/v0/resource/plugins/cpa-context/dashboard`);
		if (!dashboard.ok) {
			fail(`the dashboard resource answered ${dashboard.status}`);
		}
		const html = await dashboard.text();
		if (!html.includes("CPA context")) {
			fail("the dashboard resource did not return the page");
		}

		console.log(
			`ok: ${session.requests} request, ${session.inputTokens} prompt tokens, ` +
			`${Object.keys(session.composition).length} categories`,
		);
	} catch (error) {
		console.error(log);
		throw error;
	} finally {
		await cleanup();
	}
}

await main();
