import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { YAML } from 'bun';
import type { ExtensionAPI, ExtensionContext, ProviderConfig } from '@oh-my-pi/pi-coding-agent';
import { AgentRegistry } from '@oh-my-pi/pi-coding-agent/registry/agent-registry';
import type { Model, StreamOptions } from '@oh-my-pi/pi-ai';
import { streamAnthropic, streamOpenAICompletions, streamOpenAIResponses } from '@oh-my-pi/pi-ai/providers/register-builtins';

const supported = ['openai-completions', 'openai-responses', 'anthropic-messages'];

function cpaRoot(baseUrl: string): string | undefined {
  try {
    const url = new URL(baseUrl);
    if ((url.protocol !== 'http:' && url.protocol !== 'https:') || url.username || url.password || url.search || url.hash) return;
    // Keep the reverse proxy's mount point. Two paths on one host can be different servers.
    url.pathname = url.pathname.replace(/\/+$/, '').replace(/\/v1$/, '') + '/';
    return url.href;
  } catch { return; }
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

async function advertisesCPA(root: string): Promise<boolean> {
  try {
    const response = await fetch(root, {
      headers: { Accept: 'application/json' }, credentials: 'omit', redirect: 'error', signal: AbortSignal.timeout(1500),
    });
    const reader = response.body?.getReader();
    if (!reader) return false;
    try {
      if (response.status !== 200 || response.headers.get('content-type')?.split(';')[0].trim() !== 'application/json') return false;
      const decoder = new TextDecoder();
      let text = '', bytes = 0;
      for (; ;) {
        const { done, value } = await reader.read();
        if (done) break;
        bytes += value.byteLength;
        if (bytes > 4096) return false;
        text += decoder.decode(value, { stream: true });
      }
      const info: unknown = JSON.parse(text + decoder.decode());
      return record(info) && info.message === 'CLI Proxy API Server' && Array.isArray(info.endpoints) &&
        info.endpoints.includes('POST /v1/chat/completions') && info.endpoints.includes('GET /v1/models');
    } finally { await reader.cancel(); }
  } catch { return false; }
}

// Only probe the endpoint of a selected model. Coalesce simultaneous task starts
// and let both a temporary outage and a reused URL change the answer over time.
interface Discovery {
  until: number;
  confirmed: boolean;
  ready: Promise<boolean>;
}

const discoveries = new Map<string, Discovery>();
function discover(root: string): Promise<boolean> {
  const now = Date.now();
  const cached = discoveries.get(root);
  if (cached && cached.until > now) return cached.ready;
  for (const [key, entry] of discoveries) if (entry.until <= now) discoveries.delete(key);
  const entry: Discovery = {
    until: now + 60_000,
    confirmed: false,
    ready: advertisesCPA(root).then(ok => { entry.confirmed = ok; return ok; }),
  };
  discoveries.set(root, entry);
  return entry.ready;
}

function parentOf(sessionId: string): string | undefined {
  const registry = AgentRegistry.global();
  const children = registry.list().filter(ref => ref.kind === 'sub' && ref.session?.sessionManager.getSessionId() === sessionId);
  if (children.length !== 1) return;
  const child = children[0];
  if (!child.parentId || !child.parentSessionKey) return;
  const parent = registry.get(child.parentId)?.session;
  if (!parent || parent.instanceKey !== child.parentSessionKey) return;
  const id = parent.sessionManager.getSessionId();
  return id === sessionId ? undefined : id;
}

// CPA's Claude branch reads the parent from the body. Leave the caller's
// account/device fields and opaque or already attributed user identities alone.
function claudeParent(payload: unknown, sessionId: string, parent: string): unknown {
  if (!record(payload)) return payload;
  const meta = payload.metadata ?? {};
  if (!record(meta)) return payload;
  let identity: unknown = { session_id: sessionId };
  if (meta.user_id != null) {
    if (typeof meta.user_id !== 'string') return payload;
    try { identity = JSON.parse(meta.user_id); } catch { return payload; }
  }
  if (!record(identity) || identity.session_id !== sessionId || identity.parent_session_id || identity.parent_agent_id || identity.parent_id) return payload;
  return { ...payload, metadata: { ...meta, user_id: JSON.stringify({ ...identity, parent_session_id: parent }) } };
}

async function configuredRoutes(dir: string): Promise<[string, string[]][]> {
  for (const name of ['models.yml', 'models.yaml']) {
    let text: string;
    try { text = await readFile(join(dir, name), 'utf8'); } catch { continue; }
    let file: unknown;
    try { file = YAML.parse(text); } catch { return []; }
    if (!record(file) || !record(file.providers)) return [];
    const routes: [string, string[]][] = [];
    for (const [provider, config] of Object.entries(file.providers)) {
      if (!record(config)) continue;
      const models = Array.isArray(config.models) ? config.models.filter(record) : [];
      if (models.length === 0 && !supported.includes(String(config.api))) continue;
      if (models.some(m => !supported.includes(String(m.api ?? config.api)))) continue;
      const urls = [config.baseUrl, ...models.map(m => m.baseUrl)];
      const roots = urls.flatMap(u => typeof u === 'string' ? cpaRoot(u) ?? [] : []);
      if (roots.length) routes.push([provider, [...new Set(roots)]]);
    }
    return routes;
  }
  return [];
}

export default async function lineage(pi: ExtensionAPI) {
  const registered = new Set<string>();
  const stream: NonNullable<ProviderConfig['stream']> = (model, context, options) => {
    let request: StreamOptions = options;
    const id = options.sessionId;
    const route = discoveries.get(cpaRoot(model.baseUrl) ?? '');
    const existing = [model.headers, options.headers].some(headers => headers && Object.keys(headers).some(key => key.toLowerCase() === 'x-parent-session-id'));
    if (id && !existing && !options.parentSessionId && route?.confirmed && route.until > Date.now()) {
      const parent = parentOf(id);
      if (parent) {
        request = { ...options, headers: { ...options.headers, 'X-Parent-Session-Id': parent } };
        if (model.api === 'anthropic-messages') {
          request.onPayload = async (payload, requestModel, signal) => {
            const previous = await options.onPayload?.(payload, requestModel, signal);
            return claudeParent(previous ?? payload, id, parent);
          };
        }
      }
    }
    switch (model.api) {
      case 'anthropic-messages': return streamAnthropic(model as Model<'anthropic-messages'>, context, { isOAuth: model.isOAuth, ...request });
      case 'openai-completions': return streamOpenAICompletions(model as Model<'openai-completions'>, context, request);
      case 'openai-responses': return streamOpenAIResponses(model as Model<'openai-responses'>, context, request);
      default: throw new Error(`Unsupported CPA transport: ${model.api}`);
    }
  };

  async function bind(provider: string, roots: string[]) {
    const confirmations = await Promise.all(roots.map(discover));
    if (!confirmations.includes(true) || registered.has(provider)) return;
    pi.registerProvider(provider, { stream });
    registered.add(provider);
  }

  // omp clears this source's previous provider hooks when it binds a new task.
  // Queue the complete configured set here, before that clear-and-replace step.
  const routes = await configuredRoutes(pi.pi.getAgentDir());
  await Promise.all(routes.map(([provider, roots]) => bind(provider, roots)));

  async function prepare(_event: unknown, ctx: ExtensionContext) {
    const model = ctx.model;
    if (ctx.agent?.kind !== 'sub' || !model || !supported.includes(model.api)) return;
    const root = cpaRoot(model.baseUrl);
    if (!root) return;
    if (!registered.has(model.provider) && ctx.modelRegistry.getProviderModels(model.provider).some(m => !supported.includes(m.api))) return;
    await bind(model.provider, [root]);
  }
  pi.on('session_start', prepare);
  pi.on('turn_start', prepare);
}
