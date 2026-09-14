# The fault catalogue

A rule is what to match and what to do to it. This page is the "what to do":
every fault Faultline has, what it is for, and one example of each, written the
way `faultline.yaml`, the API and the MCP tools take it and the way the command
line takes it. [reference.md](reference.md) has every parameter with its
bounds; it is generated from the same catalogue the binary runs, so the two
never disagree.

```yaml
rules:
  - id: stripe-is-slow
    name: Stripe is slow
    match:
      host: api.stripe.com
      method: POST
      path: /v1/charges/*
    fault:
      type: delay
      ms: 2000
      jitter_ms: 500
    behavior:
      type: first_n
      n: 2
```

```
faultline rule add --name "Stripe is slow" \
  --host api.stripe.com --method POST --path '/v1/charges/*' \
  --fault delay --set ms=2000 --set jitter_ms=500 \
  --behavior first_n --behavior-set n=2
```

A fault is its `type` and that type's parameters, side by side. No other key is
accepted, and a wrong one is refused with the field named, so the fastest way to
learn a fault's parameters is to guess: `faultline rule add --fault status`
with nothing else answers with what `status` takes.

## Two tiers

**Connection faults** act on the connection and need nothing else, so they work
on every call, HTTPS that Faultline cannot read included. They are what an
unreachable, dying or saturated dependency looks like from outside.

**Response faults** rewrite a request or its response, so Faultline has to see
inside the call. On plain HTTP and on HTTPS the client trusts Faultline for,
they apply in full. On a host that stays at tier `encrypted` they cannot apply,
and Faultline says so: the rule is stored with a warning, the report repeats it
beside its counts, and [trust.md](trust.md) is the fix. A `faulted` of zero next
to that warning means the fault never fired, not that the application coped.

Every synthetic response - a `status`, a `rate_limit`, a `refuse` answered on
plain HTTP - carries a `Faultline-Fault` header holding the rule id, so a 503
Faultline produced is never mistaken for one the upstream sent.

## Connection faults

### `delay`

Hold the call back before letting it through. The response is the real one,
later. `jitter_ms` adds a random amount on top, so a client's timeout is hit
sometimes rather than always, which is how flaky latency looks.

```yaml
fault: { type: delay, ms: 2000, jitter_ms: 500 }
```

```
faultline rule add --host api.stripe.com --fault delay --set ms=2000 --set jitter_ms=500
```

Use it to find out whether a loading state exists, whether a timeout is set,
and what the user sees while they wait.

### `hang`

Accept the connection and never answer. The client waits until its own timeout
fires, which is exactly the thing worth rehearsing: a client with no timeout
waits forever. `max_ms` makes Faultline drop the connection after that long,
for a client you suspect has no timeout and do not want to leave hanging.

```yaml
fault: { type: hang, max_ms: 30000 }
```

```
faultline rule add --host api.stripe.com --fault hang --set max_ms=30000
```

To prove a configured timeout is the one in force, run twice: hold the call a
little under the configured value, then a little over. One reading proves only
that something, somewhere, eventually gave up.

### `refuse`

Turn the connection away, the way a host with nothing listening does. On plain
HTTP the client gets a `502` saying the connection was refused by the rule; on
HTTPS the tunnel is never opened, and the client sees a connection error before
any TLS handshake. Takes no parameters.

```yaml
fault: { type: refuse }
```

```
faultline rule add --host api.stripe.com --fault refuse
```

This is the fault for "the dependency is down": what an unreachable service
does, and it works whatever Faultline can see of the host.

### `reset`

Break the connection off. With no parameter, nothing is sent at all; with
`after_bytes`, that much of the real response reaches the client and then the
connection dies mid-body, which is how a dependency that crashes half way
through an answer looks. If the body is shorter than `after_bytes`, the
connection is reset where the body ends: a client reading a chunked body sees
the break, while one reading a `Content-Length` body has every byte by then and
only loses the connection.

```yaml
fault: { type: reset, after_bytes: 1024 }
```

```
faultline rule add --host api.stripe.com --fault reset --set after_bytes=1024
```

### `throttle`

Deliver the real response at a crawl. Every byte still arrives, and nothing is
changed but the speed, so a large response takes as long as `bytes_per_sec`
says it should.

```yaml
fault: { type: throttle, bytes_per_sec: 4096 }
```

```
faultline rule add --host cdn.example.com --fault throttle --set bytes_per_sec=4096
```

## Response faults

### `status`

Answer with a status code of your choosing instead of the upstream's. The
upstream is still called; its response is replaced. `body` is the response body
to send, and there is none without it.

```yaml
fault: { type: status, code: 503, body: '{"error":"service unavailable"}' }
```

```
faultline rule add --host api.stripe.com --fault status --set code=503 --set 'body={"error":"service unavailable"}'
```

`--set` reads a value as JSON when it is JSON and as text otherwise, so a body
that happens to look like a number wants quoting: `--set 'body="200"'`.

### `rate_limit`

Answer `429 Too Many Requests` with a `Retry-After` header holding
`retry_after_sec`. Narrower than `status` on purpose: it is for checking that
a client honours the hint rather than hammering on.

```yaml
fault: { type: rate_limit, retry_after_sec: 5 }
```

```
faultline rule add --host api.stripe.com --fault rate_limit --set retry_after_sec=5
```

### `headers`

Set or strip response headers on an otherwise untouched response. `set` is a
map of names to values, `remove` a list of names, and at least one of the two
has to be there.

```yaml
fault:
  type: headers
  set: { Cache-Control: no-store }
  remove: [ETag]
```

```
faultline rule add --host api.stripe.com --fault headers --set 'set={"Cache-Control":"no-store"}' --set 'remove=["ETag"]'
```

### `truncate`

Cut the response body off. `after_bytes` cuts after that many bytes, `percent`
after that share of the body; write one or the other, not both. The status and
the headers are the real ones, so the client believes it is reading a good
response until the body ends early. If the body is shorter than `after_bytes`,
it is delivered whole and the event is still recorded as faulted. On a chunked
body the client sees a clean short body rather than a broken one, since there
is no length for it to check against.

```yaml
fault: { type: truncate, percent: 50 }
```

```
faultline rule add --host api.stripe.com --fault truncate --set percent=50
```

### `corrupt`

Flip bits in the real response body. The body keeps its length and the
transfer stays valid, so nothing below the application notices: the fault lands
on whatever parses the bytes, which is where a mangled payload has to be
handled. `percent` is how much of the body to touch.

```yaml
fault: { type: corrupt, percent: 10 }
```

```
faultline rule add --host api.stripe.com --fault corrupt --set percent=10
```

### `slow_body`

Deliver a correct response over `ms` milliseconds, trickling the body rather
than holding the whole call back. Nothing about the response is wrong, which is
the point: it is the fault for a client whose read timeout, or lack of one,
only shows when the bytes come slowly.

`slow_body`, and `truncate` with `percent`, need the body's length to pace or
divide it. When the upstream sends no `Content-Length`, Faultline reads the
whole body into memory before sending the first byte, so a streaming or
long-poll response (server-sent events, say) never starts and memory grows with
the body. Use `throttle`, or `truncate` with `after_bytes`, for those.

```yaml
fault: { type: slow_body, ms: 5000 }
```

```
faultline rule add --host api.stripe.com --fault slow_body --set ms=5000
```

## Behaviors

A behavior decides when a rule's fault applies. Without one the fault applies
to every matching call, which is an outage; with one it has a shape. Enabling a
rule, or activating a scenario that names it, starts its behavior over, so a
rehearsal always begins from the beginning.

### `first_n`

Apply the fault to the first `n` matching calls, then let the rest through.
This is the shape for "does it retry": break the dependency for two calls, and
read whether the application tried again and succeeded on the third.

```yaml
behavior: { type: first_n, n: 2 }
```

```
faultline rule add --host api.stripe.com --fault status --set code=503 --behavior first_n --behavior-set n=2
```

### `percent`

Apply the fault to that share of matching calls, chosen at random. The shape
for a flaky dependency over many calls.

```yaml
behavior: { type: percent, percent: 10 }
```

```
faultline rule add --host api.stripe.com --fault status --set code=503 --behavior percent --behavior-set percent=10
```

### `for_duration`

Apply the fault for `sec` seconds, then recover. The clock starts at the first
call the rule matches rather than when the rule was switched on, so an outage
nobody has driven traffic through yet has not started; switching the rule off
and on, or resetting the session, starts it over. The shape for an outage with
a length: does the application come back on its own once the dependency does,
or does a circuit breaker stay open?

```yaml
behavior: { type: for_duration, sec: 30 }
```

```
faultline rule add --host api.stripe.com --fault refuse --behavior for_duration --behavior-set sec=30
```

### `pattern`

Repeat a cycle, one letter per matching call: `F` applies the fault, `P` lets
the call through. `FFP` fails two, passes one, and starts again.

```yaml
behavior: { type: pattern, pattern: FFP }
```

```
faultline rule add --host api.stripe.com --fault status --set code=500 --behavior pattern --behavior-set pattern=FFP
```

## How rules combine

A call is decided by the first enabled rule, in order, that matches it and
whose behavior accepts it. A rule with no behavior always accepts, so a `delay`
written above a `status` on the same host means the `status` never fires. A
rule whose behavior declines hands the call to the next matching rule, so a
`first_n: 2` `status` above a `delay` gives two failures and then slow
responses. Faultline warns when a rule can never fire because of the one above
it, when it is added and on the report.

The order is the file's order in `faultline.yaml` and the list's order in the
UI and `faultline rule list`. Rules added through the API go to the end.

## Reading the result

The session report says what the application did under the faults it was
given: how many calls went out, how many a rule broke, how many looked like
retries, the longest gap between an attempt and its repeat, and how many
failures nothing followed up on. `faultline session report` prints it,
`faultline run` prints it when the child exits, and `GET
/api/sessions/current/report` returns it. [cli.md](cli.md#sessions-and-reports)
says what each number means and, more usefully, what it does not.
