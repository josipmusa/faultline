# The HTTP API

Everything the UI, the command line and the MCP server do goes through this
API, on the admin port, `http://localhost:9000` by default. JSON in and out,
`snake_case` keys. There is no authentication: [security.md](security.md) says
what that means and what the server checks instead.

Errors share one shape, with `field` present when one input caused the
failure:

```json
{ "error": "ms must be at least 1", "field": "fault.ms" }
```

| status | meaning |
| --- | --- |
| 400 | the request is invalid; `field` names what |
| 403 | the request came from a web page on another site, or names a host that is not this machine |
| 404 | no such rule, scenario, event or endpoint |
| 409 | the id or name is taken, or the configuration file cannot be read so the change was not made |
| 500 | the change was made in memory but could not be written to the file, and the message says why |
| 503 | the server is shutting down |

## Rules

A rule is the shape [faults.md](faults.md) describes:

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

| | |
| --- | --- |
| `GET /api/rules` | every rule, in the order they apply |
| `POST /api/rules` | create one; `id` is derived from `name` when left out; 201 with the rule as stored |
| `GET /api/rules/{id}` | one rule |
| `PUT /api/rules/{id}` | replace one; the id in the path wins |
| `DELETE /api/rules/{id}` | remove one; 204 |
| `POST /api/rules/{id}/enable` | switch it on; its behavior starts over |
| `POST /api/rules/{id}/disable` | switch it off |

The single-rule responses carry a `warnings` list when the rule is in force
but cannot fire, most often a response-tier fault on a host Faultline can only
tunnel. The list does not, so a cached list is re-read from here.

## Scenarios

A scenario is a named group of rule ids. Activating one enables its rules and
deactivates whichever scenario was active.

| | |
| --- | --- |
| `GET /api/scenarios` | each with `name`, `rules` and `active` |
| `POST /api/scenarios` | `{ "name": "...", "rules": ["id", ...] }`; every id must exist; 201 |
| `POST /api/scenarios/{name}/activate` | the scenario, active |
| `POST /api/scenarios/{name}/deactivate` | the scenario, inactive; its rules are disabled |

## Events

An event is one call the proxy saw:

```json
{
  "id": "17", "timestamp": "2026-09-14T17:02:00Z",
  "host": "httpbin.org", "method": "GET", "path": "/get",
  "status": 503, "duration_ms": 12, "bytes_in": 0, "bytes_out": 37,
  "faulted": true, "rule_id": "fail-httpbin", "retry_of": "16",
  "tier": "intercepted"
}
```

| | |
| --- | --- |
| `GET /api/events` | the most recent thousand, oldest first. `?host=` keeps one host, `?faulted=true` or `false` one side of the split, `?limit=N` the most recent N |
| `DELETE /api/events` | forget them all, captures included; 204 |
| `GET /api/events/{id}/capture` | the headers and, unless `--no-bodies`, the bodies of one exchange: `event_id`, `bodies`, `request` and `response`, each side with `headers`, `body` (base64) and `truncated` |
| `GET /api/events/stream` | the WebSocket, below |
| `GET /api/upstreams` | one row per host: `host`, `tier`, `requests`, `faulted`, `errors`, `last_seen`, `bypassed`, `bypass_entry` and a `hint` when something is in the way |

A capture exists only for a call Faultline could read, so a host at tier
`encrypted` has none, and the endpoint says so with a 404.

### The stream

`GET /api/events/stream` upgrades to a WebSocket that only ever sends. Every
message is a tagged envelope, so a client switches on `type`:

```json
{ "type": "event", "event": { "id": "18", "host": "httpbin.org", "method": "GET", "faulted": false, "tier": "plain" } }
{ "type": "rules_changed" }
```

`rules_changed` says a cached rule list is stale and should be re-read from
`GET /api/rules`. Rules never travel over the socket, and no ordering is
promised between an event and a notice.

## The session

| | |
| --- | --- |
| `GET /api/sessions/current/report` | `total`, `faulted`, `retries`, `max_retry_wait_ms`, `abandoned`, and `warnings` for every rule in force that cannot fire |
| `POST /api/sessions/current/reset` | forget the events and captures and re-arm every rule's behavior, leaving the rules and the active scenario alone; 204 |

[cli.md](cli.md#sessions-and-reports) says what the report's numbers measure
and what they do not.

## Bypass

The hosts the forward proxy passes through untouched and never records.
Loopback is always on the list and cannot be taken off.

| | |
| --- | --- |
| `POST /api/bypass` | `{ "host": "metrics.internal" }`; a host, a `host:port`, or a suffix written `*.internal`; 201 |
| `DELETE /api/bypass/{host}` | take it off; 204 |

## About this instance

| | |
| --- | --- |
| `GET /api/health` | `{ "status": "ok" }` |
| `GET /api/config` | `persisted` and `path` say whether changes outlive the process and where they go; `proxy_url`, `no_proxy` and `intercepting` say how to send a child through this instance; `routes` lists each explicit route as `name`, `addr` and `upstream`; `bypass` is the list as configured |
| `GET /api/catalogue` | every fault and behavior with a `description` and its parameters: `name`, `kind`, `description`, `required`, `min`, `max`, `chars`. The UI's rule editor is drawn from this |
| `/mcp` | the MCP server over streamable HTTP, in [agents.md](agents.md) |

## Clients

[clients/go](../clients/go) and [clients/ts](../clients/ts) are thin typed
clients over these endpoints, written for use inside tests; the command line
is built on the Go one.
