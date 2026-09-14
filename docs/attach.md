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
exits. HTTPS is intercepted from the first run: Faultline creates a local
certificate authority if there is none and hands the child the variables each
runtime reads to trust it, and nothing is installed anywhere. [trust.md](trust.md)
has the variables, runtime by runtime, and the runtimes that need more.

Wrap works for a test suite as well as a server: `--scenario` turns a scenario
on for the length of the command, `--report` writes the report as JSON, and the
exit code is the suite's own. [cli.md](cli.md#sessions-and-reports) covers the
run and its report.

What wrap does not catch is traffic that does not start in the child. A
browser talking to a wrapped dev server is the usual case; see
[Browsers and frontends](#browsers-and-frontends).

## Container companion

For an application that runs in Docker Compose, Faultline runs as one more
service and the application's services send their traffic to it over the
Compose network. The application image is untouched. `faultline compose inject`
writes the override that adds the Faultline service and points the named
services at it through the proxy and CA variables; a JVM reads none of those
and needs the `faultline trust java` entrypoint helper instead. For an
application that cannot be told about a proxy at all, transparent mode redirects
its outbound 80 and 443 with iptables, Linux only, and moves the traffic
without decrypting it. [docker.md](docker.md) has the override, the helper, the
transparent-mode constraints, and links to the two working Compose examples.

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
same across runs. `--route` is repeatable, once per upstream. Because a route
terminates TLS itself and speaks to the client over plain HTTP, there is
nothing to trust and every fault applies.

## Browsers and frontends

`faultline run -- npm run dev` wraps a dev server, and the calls the dev server
makes go through Faultline. The calls the *page* makes do not.

`faultline run` works by starting your command as a child process with
`HTTP_PROXY`, `HTTPS_PROXY` and the CA variables set in its environment. That
environment belongs to the child and to whatever the child starts. Your browser
is not the child: it was already running, it has its own proxy settings from the
operating system, and it has its own list of trusted certificate authorities.
So `fetch("https://api.stripe.com/v1/charges")` from a React component goes
straight from the browser to Stripe. Faultline never sees it, and no rule can
touch it.

Most dev servers already proxy API calls for you, precisely so the page can call
a relative path and avoid CORS. That proxying happens in the dev server, in
Node, which *is* the child process. The page calls `/api/charges`, the dev
server calls the upstream, and that second call is one Faultline can be put in
front of. Give the upstream an explicit route and point the dev proxy at it. In
Vite:

```ts
// vite.config.ts
export default defineConfig({
  server: {
    proxy: {
      "/api": {
        target: process.env.API_TARGET ?? "https://api.stripe.com",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ""),
      },
    },
  },
});
```

```
API_TARGET=http://localhost:9100 faultline run --route api=https://api.stripe.com -- npm run dev
```

Every call the page makes now runs through Faultline, with the page's own
loading, error and retry states showing what a user would actually see. A two
second delay rule on that route is the fastest way to find out whether your
loading state exists. Ports are the only thing that changes in your project,
and only in dev configuration: `API_TARGET` unset means the app talks to the
real upstream, so nothing about the setup leaks into production.

The mechanism is the same wherever the dev server does the forwarding:

| Dev server | Where the target lives |
| --- | --- |
| Vite | `server.proxy` in `vite.config.ts` |
| Next.js | `rewrites()` in `next.config.js`, or the API route's own fetch |
| Create React App | `proxy` in `package.json`, or `setupProxy.js` |
| Angular CLI | `proxyConfig` JSON |

A framework that renders on the server, Next.js included, makes its calls in the
child process to begin with, so those are caught by the wrapper alone, no route
needed. Only calls the browser starts need one.

You can instead configure a browser to use `http://localhost:9001` as its proxy
and install the Faultline CA in its trust store, and Faultline will then see
everything - including every request the browser makes to everything else you
have open. That is a lot of noise, it is per-browser manual setup, and it
changes the machine rather than the project. The route costs one environment
variable and shows exactly the traffic the application depends on, so that is
what Faultline recommends. [`examples/vite-frontend`](../examples/vite-frontend)
is a working version of everything above.

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
