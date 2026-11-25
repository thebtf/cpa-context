# cpa-context

Context composition and evolution for CLIProxyAPI. CPA already reports how many
tokens a request billed; this plugin reports what those tokens were.

A dashboard shows, per conversation, which part of the prompt is the system
instruction, which part is tool schemas, which is the user's own text, the
model's reasoning, tool calls and tool results, plus how the window grew from
request to request.

## What it shows

| View | Question it answers |
| --- | --- |
| Conversation list | Which conversations ran, on which model, and how big their last prompt was |
| Composition bar | Where the current window went, category by category |
| Window over time | How the prompt grew request by request, and where it compacted |
| What the newest request carried | Every block of the newest prompt, largest first, expandable to its text |
| Window events | When the window changed for a reason other than the reply |
| Agent family | Which conversations are children of which, and what each one carries |

## Agent families

CPA resolves session identity and the parent relationship from the client's own
session headers. The dashboard uses that to nest conversations: a subagent, a
fork, and a plain follow-up conversation all appear under the conversation that
spawned them, each with its own context size. A conversation whose parent is not
in the observed window appears as its own root rather than as a dangling edge.

## How the numbers are produced

CPA exposes no prompt body to a usage sink, and no usage counters to a request
interceptor. So the plugin takes both halves itself:

1. **Request interceptor.** It parses the request body the way the wire format
   defines it — OpenAI chat, OpenAI Responses, Claude messages, or Gemini — and
   splits it into addressable items: system blocks, each tool schema, each
   message block, each tool call and result.
2. **Exact tokenizer.** Every item is measured with the tokenizer for the
   request's model family (`tiktoken`), so the split reflects real token counts
   rather than a character estimate. Images are charged by area.
3. **Usage sink.** The provider's own counters arrive with the same trace id. The
   per-item estimate is then scaled so the categories sum exactly to the prompt
   tokens the provider billed. Cache accounting defers to CPA's own
   `EnsureTokenBreakdownForProvider`, so providers that report cache inside the
   input total and providers that report it beside the input total both land on
   the right prompt size.

The result is a split that adds up to the billed number, with the model's real
tokenizer deciding the proportions. The dashboard states which of the two it is
showing: a calibrated split, or an estimate when the provider sent no counters.

## Install

Build the shared library for the CPA host's OS and architecture:

```sh
go build -buildmode=c-shared -o cpa-context.dll .
```

Copy it into CPA's plugin directory, then enable it in the CPA configuration:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    cpa-context:
      enabled: true
      record_bodies: true
```

Open the dashboard at `/v0/resource/plugins/cpa-context/dashboard`. The page
itself is static and carries no data; it asks for the CPA management key and
reads `/v0/management/cpa-context/overview` with it.

## API

All routes require the CPA management key.

| Route | Purpose |
| --- | --- |
| `GET /v0/management/cpa-context/overview` | Every conversation, the newest requests, and recent events |
| `GET /v0/management/cpa-context/session?id=` | One conversation with its full timeline |
| `GET /v0/management/cpa-context/events?id=&limit=` | Window events, optionally for one conversation |
| `POST /v0/management/cpa-context/reset` | Drop everything the plugin collected |

## Limits this plugin cannot remove

- **Since start only.** CPA gives plugins no history API, so the plugin reports
  traffic it observed since the proxy started. It does not read CPA's own logs.
- **Bounded memory.** Conversations and timeline points are capped and the least
  recently active are evicted. `max_sessions` controls the session cap.
- **Prompt text is optional.** `record_bodies: false` keeps counters and the
  category split but drops the expandable text, which is the only part that
  stores request content.
- **Context window is not inferred.** CPA does not expose a model's window size
  to plugins, so the dashboard shows the client's requested output cap and the
  observed prompt size; it never claims a percentage of a window it cannot see.
- **Unrecognized formats are reported as such.** A request whose format the
  plugin does not model appears as one opaque item instead of an invented split.

## Development

```sh
go test ./...
```

The tests cover the four wire formats, the cache-shape normalization, the scaling
invariant (categories always sum to the target), the trace-id join between
interceptor and usage, and the guarantee that the interceptor never modifies a
request.
