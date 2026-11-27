# cpa-context

Context composition and evolution for CLIProxyAPI. CPA already reports how many
tokens a request billed; this plugin reports what those tokens were carrying.

The dashboard is a port of [dsh-context](https://github.com/bowenliang123/dsh-context)
to CPA. The client is that project's own: its components, styles, dictionaries
and charts are carried over unchanged, and only the host is replaced. CPA is a
proxy, so it cannot read a harness session log; it observes the wire instead. The
plugin therefore supplies the projection the client expects, computed from the
request bodies and the provider's usage counters.

## What it shows

| View | Question it answers |
| --- | --- |
| Context tab | Composition, token split, timing, current window, timeline, events, file activity, command executions |
| Fleet tab | The agent family: who spawned whom, what each member carries, and the delegations the prompt shows |
| Overview | Every conversation with usage, cache hit rate, cost estimate, daily series and per-project totals |

Each conversation is named from its own first user message, because CPA resolves
session ids but not titles.

## How the numbers are produced

CPA exposes no prompt body to a usage sink and no usage counters to an
interceptor, so the plugin takes both halves itself:

1. **Request interceptor.** It parses the body by wire format — OpenAI chat,
   OpenAI Responses, Claude messages, Gemini — and splits it into addressable
   items: system blocks, each tool schema, each message block, each tool call and
   its result.
2. **Estimate.** Each item is measured with a character heuristic, then the
   categories are scaled so they sum exactly to the prompt tokens the provider
   billed. The heuristic is deliberate: an exact BPE pass costs about a hundred
   times as much per request and the scaling removes its absolute error, so a
   bargraph built on the exact tokenizer would look the same and cost more.
3. **Usage sink.** The provider's counters arrive with the same trace id. Cache
   accounting defers to CPA's own `EnsureTokenBreakdownForProvider`, so providers
   that report cache inside the input total and providers that report it beside
   the input total both land on the right prompt size.
4. **Cost.** Estimated from the public `models.dev` catalog, which is fetched
   once, cached beside the journal and refreshed on a long interval. A model the
   catalog does not list is reported as unpriced, never as free.

A tool result is labelled with the tool that produced it, resolved from the call
it answers, and a call's target is read from its arguments. That is what gives
file activity its paths and line deltas and the command panel its programs.

## Install

Download the archive for the host's platform from
[Releases](https://github.com/thebtf/cpa-context/releases) and extract it into
CPA's plugin directory.

The archive holds one file named `cpa-context-v<version><ext>`. Keep that name:
CPA reads the plugin id and the plugin version from it, stripping `-v<version>`
and taking the remainder as the id. A file named `cpa-context.so` still loads,
but the host cannot report a version for it, and a store install into
`plugins/<goos>/<goarch>/` writes the versioned name for the same reason.

Then enable it:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    cpa-context:
      enabled: true
      record_bodies: true
      # Where observations are stored. In a container this is the mounted plugins
      # directory, which is the one that survives a restart.
      data_dir: "/CLIProxyAPI/plugins/cpa-context"
```

Open `/v0/resource/plugins/cpa-context/dashboard` and enter the management key.
The page polls while it is visible, so it stays current without a reload.

### Link oh-my-pi tasks to their parent

Copy [`extensions/omp-task-lineage.ts`](extensions/omp-task-lineage.ts) into the active omp profile's `extensions/` directory. No environment variables, address list or management key are needed. The extension checks HTTP bases already configured in the active profile and the selected task model. It supports OpenAI Chat, OpenAI Responses and Anthropic Messages. It makes a short unauthenticated `GET` to the CPA root, preserving any reverse-proxy path prefix. If the root is blocked, redirects or does not identify itself as CLIProxyAPI, model traffic continues without the extra parent identity.

Each CPA stores its own traffic. A child on a second instance can identify its parent even when that instance has no row for the parent. The dashboards do not combine databases.

## Storage

Observations are written to SQLite in `data_dir` and replayed at startup, so a
restart keeps the history. The journal is bounded twice: by a thirty-day
retention window and by the same row ceiling memory keeps, applied at startup and
on a slow ticker. A file that is unreadable, or whose tables have another shape,
is rebuilt rather than trusted; a file that is not a database at all falls back
to memory and the dashboard says the history is not durable.

Without `data_dir` the plugin works from memory only and reports that.

## API

All routes require the management key.

| Route | Purpose |
| --- | --- |
| `GET /v0/management/cpa-context/sessions` | Conversations the dashboard can open |
| `GET /v0/management/cpa-context/session?session=` | One conversation's projection |
| `GET /v0/management/cpa-context/overview` | Totals, newest requests, events, journal status |
| `GET /v0/management/cpa-context/events?id=&limit=` | Window events |
| `POST /v0/management/cpa-context/reset` | Drop everything collected |
| `POST /v0/management/cpa-context/api/dsh-context/*` | The ported client's own detail, fleet and overview routes |

## Limits this plugin cannot remove

- **Only observed traffic.** CPA gives plugins no history API, so the plugin
  reports what it saw. The journal extends that across restarts, not backwards.
- **Bounded memory.** Conversations, timeline points and held prompt text are
  capped; the least recently active conversation is evicted with everything that
  hangs off it.
- **Prompt text is optional.** `record_bodies: false` keeps counters and the
  category split but drops the expandable text.
- **Context window is not inferred.** CPA does not expose a model's window size,
  so the dashboard shows the observed prompt size and the client's output cap,
  never a percentage of a window it cannot see.
- **One step's own nodes are not kept.** The projection describes the newest
  prompt, so comparing an arbitrary step answers with a refusal rather than a
  fabricated difference; the client then falls back to its own local comparison
  and labels it as one.
- **Unrecognized formats are reported as such** rather than given an invented
  split.

## Development

```sh
go test ./...
go test -race ./...
```

The Go tests cover the four wire formats, cache-shape normalization, the scaling
invariant, the trace-id join, agent families including a parent cycle, eviction,
journal durability and schema upgrades, cost arithmetic, and the route table.

The dashboard client lives in `web/` and is built before the plugin is compiled,
because the Go build embeds its output:

```sh
cd web && npm ci && npm run build
```

### End-to-end checks

Each loads the built library into a real CLIProxyAPI binary on loopback:

```sh
go build -buildmode=c-shared -o cpa-context.dll .
node test/smoke.mjs  path/to/cli-proxy-api path/to/cpa-context.dll   # composition sums to the billed prompt
node test/restart.mjs path/to/cli-proxy-api path/to/cpa-context.dll  # history survives a restart
node test/load.mjs   path/to/cli-proxy-api path/to/cpa-context.dll  # resident memory and CPU under load
node test/versioned.mjs path/to/cli-proxy-api path/to/cpa-context.dll cpa-context-v0.0.0.dll  # the host resolves id and version from a versioned name
node test/panels.mjs path/to/cli-proxy-api path/to/cpa-context.dll  # titles, the fleet roster and readable header content
node test/concurrent.mjs path/to/cli-proxy-api path/to/cpa-context.dll  # every request accounted for under parallel load
```

No provider accounts and no installed profile are used.

### Releases

`.github/workflows/build.yml` builds the dashboard, then the plugin, for Windows
amd64, Linux amd64 and macOS arm64, downloads the pinned CLIProxyAPI release,
verifies its archive checksum, and runs the smoke test against that exact binary
before packaging. The library is packed as `cpa-context-v<version><ext>`, because
that is the name the host reads the id and the version from. Pushing a tag
publishes three archives and a `checksums.txt`.
