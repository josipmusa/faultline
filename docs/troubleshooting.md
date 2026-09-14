# Troubleshooting

Symptom first, then what it means and what to do. Most of these come down to
one of two things: the traffic is not reaching Faultline, or it is reaching
Faultline encrypted.

## Nothing appears

**`faultline upstreams` says `no upstreams seen yet` and the UI is empty,
though the application is making calls.**

The calls are not going through the proxy. Work down this list:

- **The process making the calls is not the child.** `faultline run` sets the
  proxy variables for the command after `--` and whatever it starts. A browser
  is not that process, and neither is a service that was already running. For
  a browser, put the dependency behind a route and point the dev server at it:
  [attach.md](attach.md#browsers-and-frontends). For a process Faultline did not start, set
  `HTTP_PROXY` and `HTTPS_PROXY` to `http://localhost:9001` yourself.
- **The client ignores `HTTP_PROXY`.** Some do. Give the dependency an explicit
  route with `--route name=url` and point the client's base URL at the local
  port `run` prints. [attach.md](attach.md#explicit-route).
- **Node's native `fetch`.** It does not read the proxy variables on its own.
  `faultline run` sets `NODE_USE_ENV_PROXY=1`, which Node 24 and recent 22.x
  understand; an older Node ignores it and `fetch` goes direct while
  `http.request` and libraries built on it still appear. Upgrade Node or use a
  route.
- **The host is on the bypass list.** `localhost` and loopback always are, and
  `faultline upstreams` notes a host that was skipped for this reason. Check
  `bypass` in `faultline.yaml` and `--bypass`.
- **A JVM that never got the environment.** A JVM reached by `faultline run`
  prints `Picked up JAVA_TOOL_OPTIONS: ...` as its first line. Without that
  line it read neither the proxy variables nor the CA, and its traffic never
  arrives; nothing is at tier `encrypted` to warn you, the application answers
  200, and the report is all zeros. Under Compose, that means the
  `faultline trust java` entrypoint is missing: [docker.md](docker.md).

## A host shows tier `encrypted`

**`faultline upstreams` reports `encrypted`, events carry a bare `CONNECT`
with no method or path, and response faults do nothing.**

Faultline could only tunnel this host's TLS. Connection faults - `delay`,
`hang`, `refuse`, `reset`, `throttle` - still work in full. For anything that
rewrites a response, the client has to trust the Faultline CA, and
[trust.md](trust.md) says what that takes for each runtime. If there is no CA
at all, the banner says `passing HTTPS through, connection faults only`; run
`faultline ca init`, or let `faultline run` create one.

## The hint says the client did not trust the CA

```
trust: api.stripe.com: client did not trust the Faultline CA; see docs/trust.md (Faultline set SSL_CERT_FILE, ...)
```

Faultline tried to intercept and the client rejected the certificate during
the handshake. The variables in brackets are the ones Faultline already set, so
if the runtime you are using is in that list, pointing it at the CA is not the
fix: the runtime is ignoring the variable, has a trust store of its own, or is
pinning. [trust.md](trust.md#runtimes-that-need-more). The hint clears on its
own once a request comes through the tunnel.

## Go on macOS ignores the CA

**A Go program on macOS fails every HTTPS call with a certificate error under
`faultline run`, though the same program works on Linux.**

Go on macOS reads roots from the Keychain and from no file, so `SSL_CERT_FILE`
is ignored. `faultline ca install` puts the CA in the system trust store after
an explicit yes; there is no per-process alternative on macOS.

## A warning next to a `faulted` of zero

```
REQUESTS  FAULTED  RETRIES  MAX RETRY WAIT  ABANDONED
7         0        0        0ms             0
warning: rule stripe-503 cannot apply: api.stripe.com has only been seen encrypted ...
```

The fault never fired, so the numbers say nothing about how the application
copes. A response fault on an encrypted host is the usual reason; a rule that
sits behind one that matches everything and has no behavior is the other, and
the warning says which. Fix the cause and measure again. Never read a quiet
report as resilience while a warning stands beside it.

## The report's retry numbers look wrong

The report cannot tell a retry from a poll, and `max_retry_wait_ms` is then the
poll interval rather than a backoff. Check the application's own logs before
describing that number as a backoff, and read
[cli.md](cli.md#sessions-and-reports) for what each number does and does not
measure.

## The default ports are taken

```
faultline: listen tcp 127.0.0.1:9000: bind: address already in use
```

Another Faultline is up, most often one a coding agent started through
`faultline mcp`, which holds the ports for as long as the agent is connected.
Either join it - `faultline rule add`, `faultline upstreams` and the rest talk
to whatever is on `--admin` - or give the new run ports of its own:

```
faultline run --admin-port 0 --proxy-port 0 -- go test ./...
```

`0` asks the operating system for free ports and the banner says which ones it
got. Test suites that start their own Faultline should always do this.

## The UI says it is not built into this binary

```
faultline: the web UI is not built into this binary.
```

A binary built from source without the UI export. Run `make ui` and then `make
build`, or use a released binary, which always carries the UI. The HTTP API at
`/api` works either way.

## `--config` names a file that is not there

`faultline.yaml` in the working directory is optional: without it Faultline
runs with its rules in memory. A file named with `--config` is not optional,
because you meant that file, so a missing one is an error. Check the path
relative to where you ran the command.

## Changes through the API or UI are refused with 409

The configuration file does not parse, and the error names the line. While the
file is broken, Faultline keeps the rules already in memory in force and
refuses to write into a file it cannot read, because doing so would lose either
what is in it or the new rule at the next read. Fix the line the error names
and the next save is applied without a restart.

## A rule was accepted but the log says the file was not applied

```
ERROR config: the file was not applied, the rules already in memory stay in force
  path=faultline.yaml line=14 problem="ms must be 1 or more" field=rules[0].fault.ms
```

A hand edit to `faultline.yaml` did not parse. Nothing changed; the previous
rules are still in force. Fix the line and save again.

## Transparent mode sees nothing, or sees Faultline's own traffic

Faultline's own calls to the upstream leave the same network namespace as the
application's, so the iptables rules exempt the uid Faultline runs as, `65532`.
An application running as that uid is mistaken for Faultline and passes
through unrecorded. Run the application as any other uid. Transparent mode also
needs Linux, `NET_ADMIN`, and the `-transparent` image tag, which carries the
iptables binary the default image does not; Faultline refuses to start half
attached rather than come up without them. [docker.md](docker.md).

## macOS says `killed` when the binary starts

A binary downloaded by hand is quarantined by Gatekeeper, and an unsigned
quarantined binary is killed rather than refused. The Homebrew cask clears the
attribute on install; for an archive you unpacked yourself:

```
xattr -d com.apple.quarantine ./faultline
```

## The child exited but the report counts fewer failures than the log shows

`abandoned` settles only after a failed call's retry window has closed, and the
report is taken the moment the child is gone, so calls that failed in the last
seconds are not in it yet ([cli.md](cli.md#sessions-and-reports)). `faultline
session report` against a running instance a few seconds later has the settled
number.

## A synthetic response is hard to tell from the upstream's

Every response Faultline produces carries `Faultline-Fault: <rule id>`. An
upstream's own 503 does not. Look for the header in the event inspector or in
the client's own logging before blaming either side.
