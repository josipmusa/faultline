# Security

Faultline is a development tool that does, on purpose, what an attacker would
want to do: it terminates an application's HTTPS with a CA it controls, reads
the headers and bodies that go through it, and rewrites the responses. That is
the product. This page says what it exposes while doing it, so the decision to
point it at something real is an informed one.

## What Faultline holds

| Asset | Where it lives | Lifetime |
| --- | --- | --- |
| The interception CA private key | `ca.key` in the user config directory (`~/.config/faultline` on Linux, `~/Library/Application Support/faultline` on macOS), mode `600` in a directory mode `700` | until deleted |
| Request and response headers and bodies | memory only, up to 32 MB and 64 KB per body, oldest dropped first | the process |
| Event metadata: host, method, path, status, timing, tier | memory only, the most recent 1000 | the process |
| Rules, scenarios and the bypass list | memory, and `faultline.yaml` when one is in use | until changed |

Nothing captured is written to disk on its own. `faultline events export` and
`--report` write when you ask them to, and both write event metadata - hosts,
methods, paths, timings - never headers or bodies.

## The trust boundary is the machine

Every listener binds `127.0.0.1` by default: the admin API and UI on 9000, the
forward proxy on 9001, and every explicit route. `faultline run` has no flag to
change that at all.

**There is no authentication.** The REST API, the WebSocket and the MCP
endpoint at `/mcp` are all on the admin port, and anyone who can reach that
port can read every captured header and body, add a rule that rewrites any
response, and read the configuration file's path. The forward proxy has no
credentials either: anyone who can reach port 9001 can send traffic through it
and have Faultline make the outbound connection.

That is a deliberate v0.1 position - a local development tool trusts the local
machine - and it is what makes `--bind` the flag to think about:

```
faultline serve --bind 0.0.0.0
```

widens all three listeners together. The container image's default command does
exactly this, because inside a container localhost is the container and a
published port would otherwise be a dead end. So publishing Faultline's ports on
a shared network hands the control surface to that network. On anything but a
machine you are alone on, put it behind something: an SSH tunnel, a host
firewall, or a Compose network with no published ports.

A web page you visit is the one thing that can reach a loopback port from
outside the machine, so the admin server checks two headers a browser sets and
a page cannot forge. While it is bound to localhost it answers only to a
loopback name in `Host` (`localhost`, `127.0.0.1`, `::1`, or a name under
`.localhost`): a page that points a name it controls at 127.0.0.1 (DNS
rebinding) arrives with that name and is refused with a 403. And a request
carrying an `Origin` that is not the admin server's own address is refused
whatever it asks for, so a page on another site cannot create a rule with a
request the browser sends without asking. Neither check is authentication:
anything that can open a socket to the port, including a browser extension or
a process on the machine, is still trusted. Per-service proxy credentials and
an admin token are on the roadmap's "later" list.

With `--bind 0.0.0.0` the `Host` check stands down, since being reached by name
from other machines and containers is the point of widening it; the `Origin`
check does not.

## The CA

`faultline run` creates the CA if there is none. It is a real certificate
authority: while a client trusts it, whoever holds `ca.key` can sign a
certificate for any host that client will accept. The key is created mode `600`
inside a directory created mode `700`, and it never leaves the machine - the
certificate, `ca.crt`, is the half that gets handed to clients.

`faultline run` and the Compose helper point a child at the certificate through
environment variables (`SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`,
`REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`, and for `run` also `GIT_SSL_CAINFO`),
which is trust scoped to that process and gone when it exits. That is the mode
to prefer.

`faultline ca install` is the other kind: it adds the CA to the operating
system's trust store, for every process and every browser, until it is removed.
It prints the commands and runs them only after an explicit yes, and it is never
run for you - not by `run`, not by `serve`, not by the container image. To undo
it, remove the certificate the way your platform removes any root: Keychain
Access on macOS, `trust anchor --remove` or the distribution's `update-ca-*` on
Linux.

If the key is ever copied off the machine, treat it as compromised: delete the
directory, run `faultline ca init` for a fresh one, and remove the old
certificate from any trust store it reached.

## Sensitive traffic

The inspector shows what went over the wire, which for a real application means
credentials. `--no-bodies` is the switch for traffic Faultline should not be
holding:

```
faultline run --no-bodies -- npm run dev
faultline serve --no-bodies
```

Bodies are then never captured - not in memory, not for the inspector, not for
the API. Everything else is unchanged: the faults still apply, the report is
still accurate, and the exchange is still captured.

**Headers are still captured.** An `Authorization` header, a session cookie and
an API key in a custom header all survive `--no-bodies`, because the flag is
about payloads and the inspector's value is mostly in the headers. The capture
says which mode it was taken in - `"bodies": false` in
`GET /api/events/{id}/capture`, and a notice in the inspector - so an empty body
is never mistaken for a request that had none.

If headers are too much as well, do not point Faultline at that traffic: there
is no flag for capturing nothing, and there is no redaction. Bypass the host
instead (`--bypass`), which passes it through untouched and records nothing.

## Container and transparent modes

The image binds all interfaces, as above. It runs as an unprivileged user and
the CA lives in a volume at `/var/lib/faultline`; a volume shared with anything
else shares the private key with it.

Transparent mode needs `NET_ADMIN`, and uses it to write iptables rules in its
own network namespace redirecting outbound 80 and 443 into Faultline. The rules
exempt Faultline's own uid, which is why an application sharing that namespace
must not run as uid 65532. The capability applies to the namespace the container
is in; on `network_mode: "service:faultline"` that namespace is shared with the
application.

## Reporting a vulnerability

Open an issue for anything that is not itself exploitable. For something that
is, email the address on the GitHub profile of the repository owner rather than
filing publicly, and give it a few days before disclosing.
