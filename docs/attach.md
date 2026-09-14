# Attach modes

Faultline is a proxy. An application has to send its traffic through it before
anything can be seen or broken, and the attach mode is how that happens. There
are four, and everything else about Faultline - the UI, the rules, the report -
is the same whichever one carries the traffic.

| Mode | For | What changes in the application | HTTPS |
| --- | --- | --- | --- |
| [Wrap](#wrap) | a laptop, a CI job | nothing | intercepted for the runtimes `run` knows |
| [Container companion](#container-companion) | Docker Compose | environment variables in the Compose file, or nothing in transparent mode | intercepted once the image trusts the CA |
| [Explicit route](#explicit-route) | frontends, clients that ignore proxy settings | one base URL in dev configuration | terminated by the route |
| Shared instance | a staging environment for a team | one deployment setting | planned, not in v0.1 |

Start with wrap. Reach for the companion when the application already runs in
Compose, and for a route when the traffic starts in a browser or in a client
that ignores `HTTP_PROXY`.

## The words

- **Upstream.** A dependency the application talks to, named by host:
  `api.stripe.com`, `orders-service`, `login.microsoftonline.com`.
- **Rule.** Which traffic to affect - a host, optionally a method, a path
  pattern and headers - and the fault to apply to it.
- **Fault.** One thing that goes wrong. Connection faults act on the connection
  and work on any traffic; response faults rewrite a request or its response
  and need Faultline to see inside it. [faults.md](faults.md) has every one.
- **Behavior.** When a rule's fault applies: the first N matching calls, a
  percentage, a duration, a pattern.
- **Scenario.** A named set of rules that go on and off together, committed in
  `faultline.yaml` so a whole team rehearses the same outage.
- **Session.** One observed run and the report of what the application did
  under it.
- **Tier.** How much of a call Faultline could see. Every event carries one:
  `plain` for HTTP, `intercepted` for HTTPS Faultline terminated, `encrypted`
  for HTTPS it could only tunnel.

## Wrap

```
faultline run -- <the command you already use to start your app>
```

Faultline starts the admin server and the forward proxy, then starts your
command as a child with `HTTP_PROXY` and `HTTPS_PROXY` pointing at itself. The
child owns the terminal and the exit code; Faultline's own output goes to
stderr, and one run is one session, so the report is printed when the child
exits.

HTTPS is intercepted from the first run. Faultline creates a local certificate
authority if there is none and hands the child the variables each runtime reads
to trust it: `SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`,
`CURL_CA_BUNDLE`, `GIT_SSL_CAINFO`, and for a JVM `JAVA_TOOL_OPTIONS` with a
trust store built from the JDK's own. Nothing is installed anywhere: the
variables live and die with the child. [trust.md](trust.md) covers the runtimes
that need more than that, Go on macOS among them.

Wrap works for a test suite as well as a server. `--scenario` turns a scenario
on for the length of the command, `--report` writes the report as JSON, and the
exit code is the suite's own:

```
faultline run --scenario payments-down --report report.json -- go test ./...
```

What wrap does not catch is traffic that does not start in the child. A
browser talking to a wrapped dev server is the usual case; see
[Explicit route](#explicit-route).

## Container companion

For an application that runs in Docker Compose, Faultline runs as one more
service and the application's services send their traffic to it over the
Compose network. The application image is untouched. There are two ways to
point the services at it.

**With environment variables.** `faultline compose inject` reads the Compose
file already there and writes an override beside it that adds the Faultline
service, a one-shot service that creates the CA, the CA volume, and, for each
named service, the proxy variables and the CA path under every name a runtime
reads it by:

```
faultline compose inject --service api --service worker
docker compose -f compose.yaml -f docker-compose.faultline.yml up
```

A Go, Node, Python or curl-based service needs nothing else. A JVM reads none
of those variables and needs the `faultline trust java` entrypoint helper
instead; `inject` says so when it finishes, and [docker.md](docker.md) shows
the two lines it takes.

**In transparent mode.** For an application that cannot be told about a proxy
at all, Faultline runs with `--transparent` and `NET_ADMIN`, redirects outbound
80 and 443 in its own network namespace into itself with iptables, and the
application shares that namespace:

```yaml
  faultline:
    image: ghcr.io/josipmusa/faultline:transparent
    cap_add: [NET_ADMIN]
    command: ["serve", "--bind", "0.0.0.0", "--transparent"]
  app:
    image: your-app
    network_mode: "service:faultline"
```

No variables anywhere. Transparent mode moves the traffic but does not decrypt
it: without a CA the HTTPS calls are seen at tier `encrypted`, and with one the
application still has to trust it. It is Linux only and needs the
`transparent` image; [docker.md](docker.md) has the constraints.

[examples/compose](../examples/compose) and
[examples/compose-transparent](../examples/compose-transparent) are both
arrangements, working.

## Explicit route

```
faultline run --route api=https://api.stripe.com -- npm run dev
route api: http://127.0.0.1:9100 -> https://api.stripe.com
```

A route is a local port that forwards to one upstream. It is for clients that
ignore proxy settings, and above all for frontends: a browser is not a child of
`faultline run` and never sees the proxy variables, but the dev server that
forwards the page's API calls is. Point the dev server's proxy target at the
route and every call the page makes goes through Faultline.

Routes come from `--route` or from `routes` in `faultline.yaml`, start at port
9100 and are numbered in the order they are declared, so the address stays the
same across runs. Because a route terminates TLS itself and speaks to the
client over plain HTTP, there is nothing to trust and every fault applies.

[frontends.md](frontends.md) explains the arrangement and shows it for Vite,
Next.js, Create React App and Angular.

## Shared instance

A Faultline deployed once in a staging environment, which any service joins by
being deployed with one setting naming it as the proxy and identifying the
service, so rules can target one service without touching another team's run.
This is planned and not in v0.1. `faultline serve --bind 0.0.0.0` on a VM is
the shape of it today, without the per-service identity or any protection on
the control surface, so it belongs on a network you trust.

## What HTTPS looks like in each mode

Whatever the mode, an HTTPS call ends up at one of two tiers.

At `intercepted`, Faultline terminated the TLS connection with a certificate it
signed, so it saw the method, the path, the headers and the body, and every
fault applies. This needs the client to trust the Faultline CA.

At `encrypted`, the client did not trust the CA or Faultline has none, so the
connection was tunnelled: Faultline knows the host and nothing else. Connection
faults still apply in full, because they need nothing but the connection. A
response fault cannot, and Faultline says so rather than doing nothing quietly:
the rule is accepted with a warning, the report repeats it, and the upstream
carries a hint naming the fix. [trust.md](trust.md) is that fix, runtime by
runtime.

## What Faultline does not do

- It does not mock. An upstream that is unreachable is reported as unreachable,
  with a `502` that says so; Faultline never stands in for a dependency that is
  not there. Every synthetic response it does produce carries a
  `Faultline-Fault` header naming the rule.
- It handles HTTP and HTTPS only. gRPC, WebSocket and raw TCP are not in v0.1.
- It is a development and testing tool, not a way to run experiments on
  production traffic.
- It needs no database, no account and no hosted service.
