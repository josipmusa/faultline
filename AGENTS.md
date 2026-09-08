# Faultline — Guide for coding agents

Read this first. Then read SPEC.md for what the product does and ROADMAP.md for what to build next.

## What this project is

Faultline is a single-binary HTTP and HTTPS fault-injection proxy for development and testing. It sits between an application and its dependencies, shows every outbound call, and can degrade those calls on purpose. Its front door is `faultline run -- <start command>`. It is written in Go with an embedded Next.js UI, and it exposes the same capabilities through a web UI, a CLI, an HTTP API, and an MCP server.

Predecessor code lives in `../resilience-proxy` (reverse proxy, embedded UI) and `../resilience-proxy-service` (forward proxy). Port ideas and small pieces from them when a task says so. Do not copy their architecture: no database, no source-service concept, no JWT, no rule push over WebSocket.

## How to work

- Work on exactly one ROADMAP task at a time. Say which one before starting.
- Do not start the next task until the current one's Verify block passes. If verification is manual, stop, describe the steps to the human, and wait.
- Tick the checkbox in ROADMAP.md when done and add a one-line note under the task if anything surprising happened.
- If a task is unclear, conflicts with this file, or seems to need more than it says, stop and ask. Do not widen scope silently.
- Never perform git actions (commit, branch, push, tag) unless the human asks explicitly.
- Prefer the smallest change that satisfies the task. No speculative abstractions, no configurability that was not asked for.

## Fixed decisions

These are settled. Do not reopen them in code; raise them in conversation if you believe one is wrong.

- Language: Go 1.25, standard library `net/http` for all proxying. Cobra for the CLI. Official Go MCP SDK for the agent interface. No web framework.
- State: in memory, optionally persisted to `faultline.yaml`. No database, ever.
- Real traffic only. Faultline never fabricates a response for an unreachable upstream. The `status` fault is a fault on real traffic, not a mock.
- Ports: admin `9000` (UI, API, WebSocket, MCP), forward proxy `9001`, explicit routes from `9100`. Admin binds to localhost by default.
- Per-user state in the OS config dir (`~/.config/faultline` on Linux). CA key is owner-readable only.
- Tiers: `plain`, `intercepted`, `encrypted`. Every event carries one. Response-tier faults never silently no-op; the tier explains why.
- Synthetic responses always carry `Faultline-Fault: <rule id>`.
- The WebSocket stream is read-only. All mutations go through the REST API.
- HTTP and HTTPS only for v0.1.

## Repository layout

```
cmd/faultline/        CLI entry and subcommands
internal/config/      faultline.yaml schema, loading, hot reload
internal/rules/       Rule, Match, store
internal/faults/      one file per fault, registry, RoundTripper pipeline
internal/events/      Event, ring buffer, subscribers, report
internal/proxy/reverse/   explicit routes
internal/proxy/forward/   forward proxy, CONNECT, bypass
internal/tlsmitm/     CA, leaf certs, interception
internal/runner/      `faultline run` wrapper and runtime env injection
internal/admin/       REST API, WebSocket, embedded UI serving
internal/mcp/         MCP tools over the same service layer as the API
web/                  Next.js UI, static export embedded at build
clients/go, clients/ts    thin API clients for tests
examples/             sample apps used in verification
skills/faultline/     Claude Code skill
docs/                 user documentation
schema/               JSON Schema for faultline.yaml
```

Files stay under about 400 lines. Split by feature, not by type.

## API shape

JSON, snake_case. A rule looks like:

```json
{
  "id": "slow-stripe",
  "name": "Stripe is slow",
  "enabled": true,
  "match": { "host": "api.stripe.com", "method": "POST", "path": "/v1/charges/*", "header": { "X-Test": "1" } },
  "fault": { "type": "delay", "ms": 2000, "jitter_ms": 500 },
  "behavior": { "type": "first_n", "n": 2 }
}
```

Errors are `{ "error": "human readable", "field": "fault.ms" }` with 400 for validation, 404 for unknown ids, 409 for conflicts.

## Code conventions

- Domain values are immutable. Updates return new copies. Stores are the only mutable things and are guarded by mutexes.
- Every network operation takes a `context.Context` and respects cancellation. No bare `time.Sleep` in request paths.
- Validate at the boundary: API handlers, config loader, CLI flags. Fail fast with a message that names the field.
- Errors are wrapped with context and never swallowed. Log with `log/slog`.
- Tests live next to code. Use `httptest` for upstreams. Run with `-race`. Target 80 percent coverage on `internal/`.
- Write the test first when the task is a behavior change. Run it, see it fail, then implement.
- UI: TypeScript strict, functional components, data fetched through one API module, no rule state pushed over the socket.

## Commands

```
make build      # ./bin/faultline (embeds web/out if present)
make test       # go test -race ./...
make lint       # golangci-lint run
make ui         # builds web/ static export
make run        # faultline serve with examples config
```

## Definition of done for a task

1. The Verify block passed and the output or the human's confirmation is recorded.
2. Tests added or updated, `make test` and `make lint` clean.
3. No new dependency without a sentence justifying it in the conversation.
4. ROADMAP.md checkbox ticked.
5. Docs touched if user-visible behavior changed.

## Things that look tempting and are wrong here

- Adding a mock mode "just for convenience."
- Adding a database or a cache service.
- Pushing full rule lists over the WebSocket.
- Installing the CA into the OS trust store without an explicit confirmation.
- Proxying the admin port through itself.
- Applying a response-tier fault to encrypted traffic and returning nothing.
