# Configuration reference

<!-- Generated from the fault catalogue by `make docs`. Edit internal/config/reference.go, not this file. -->

Every key `faultline.yaml` accepts, and every fault and behavior a rule can
name, with the bounds Faultline checks them against. [config.md](config.md)
explains how the file is read and written; [faults.md](faults.md) shows each
fault in use. The same faults and parameters are what the API, the CLI and
the MCP tools accept, in the same shape.

## Top level

| Key | Type | Description |
| --- | --- | --- |
| `routes` | list of `route` | Explicit routes: an upstream reachable on a local port, for clients that ignore proxy settings. |
| `bypass` | list of string | Hosts the forward proxy passes through untouched: no rules, no events, no interception. |
| `rules` | list of `rule` | Rules applied to matching traffic. A rule at the top level is on unless it says otherwise. |
| `scenarios` | list of `scenario` | Named situations: sets of rules that go on and off together. |

## `route`

| Key | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | What this route is called. |
| `upstream` | string, matching `^https?://` | yes | The upstream, as an absolute URL: https://api.stripe.com. |
| `port` | integer, 1 to 65535 | no | The local port to listen on. Left out, Faultline assigns one from 9100 up. |

## `rule`

| Key | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string, matching `^[A-Za-z0-9._-]+$` | yes | How everything else names this rule. |
| `name` | string | yes | What this rule is for, in a few words. |
| `enabled` | boolean | no | Whether the rule applies. Rules written inside a scenario default to false. |
| `match` | `match` | no | Which requests the rule applies to. Everything left out matches everything. |
| `fault` | `fault` | yes | The fault and its parameters. The type says which parameters there are. |
| `behavior` | `behavior` | no | The behavior and its parameters. The type says which parameters there are. |

## `match`

Which requests the rule applies to. Everything left out matches everything.

| Key | Type | Required | Description |
| --- | --- | --- | --- |
| `host` | string | no | A hostname like api.stripe.com, not a URL. |
| `method` | string, matching `^[A-Za-z]+$` | no | An HTTP method like GET or POST. |
| `path` | string, matching `^[/*]` | no | A path, with * standing in for the rest: /v1/charges/*. |
| `header` | map of string to string | no | Header names and the values they must have. |

## `scenario`

| Key | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | How the scenario is turned on and off. |
| `rules` | list of string or `rule` | no | The rules this scenario turns on, named by id or written out in full. |

## Faults

A fault is written as its `type` and that type's parameters, side by side.
No other key is accepted. A connection tier fault works on every request,
encrypted traffic included; a response tier fault needs Faultline to see the
request, so it does nothing to a host that stays at tier `encrypted`.

### `corrupt`

A response tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `percent` | integer, 1 to 100 | yes | How much of the body to mangle, as a percentage of its bytes |

### `delay`

A connection tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `ms` | integer, 1 or more | yes | How long to hold the request back, in milliseconds |
| `jitter_ms` | integer, 0 or more | no | Random extra delay on top, up to this many milliseconds |

### `hang`

A connection tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `max_ms` | integer, 1 or more | no | Give up and drop the connection after this long. Left out, the request hangs until the client does |

### `headers`

A response tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `remove` | list of string | no | Response headers to strip, by name |
| `set` | map of string to string | no | Response headers to set, by name |

Write at least one of `remove` and `set`.

### `rate_limit`

A response tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `retry_after_sec` | integer, 0 or more | yes | How many seconds the Retry-After header tells the client to wait |

### `refuse`

A connection tier fault.

Takes no parameters.

### `reset`

A connection tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `after_bytes` | integer, 0 or more | no | Deliver this much of the real response before breaking the connection. Left out, nothing is sent at all |

### `slow_body`

A response tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `ms` | integer, 1 or more | yes | How long the whole body should take to arrive, in milliseconds |

### `status`

A response tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `code` | integer, 100 to 599 | yes | The status code to answer with instead of the upstream's |
| `body` | string | no | The body to answer with. Left out, the response has none |

### `throttle`

A connection tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `bytes_per_sec` | integer, 1 or more | yes | How fast the response is allowed to arrive, in bytes per second |

### `truncate`

A response tier fault.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `after_bytes` | integer, 0 or more | no | Cut the body off after this many bytes |
| `percent` | integer, 1 to 99 | no | Cut the body off after this share of it, as a percentage |

Write `after_bytes` or `percent`, not both.

## Behaviors

A behavior decides when a rule's fault applies. It is written the same way, as
a `type` and its parameters, and a rule with no behavior applies its fault to
every matching request. Enabling a rule, or activating a scenario that names
it, starts its behavior over.

### `first_n`

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `n` | integer, 1 or more | yes | How many matching requests the fault applies to before the rule lets the rest through |

### `for_duration`

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `sec` | integer, 1 or more | yes | How many seconds after the rule is switched on the fault keeps applying |

### `pattern`

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `pattern` | string, matching `^[FfPp]+$` | yes | The cycle to repeat, one letter per matching request: F applies the fault, P lets it through |

### `percent`

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `percent` | integer, 1 to 100 | yes | What share of matching requests the fault applies to, as a percentage |