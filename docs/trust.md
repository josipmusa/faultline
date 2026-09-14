# Trust

To show and degrade HTTPS traffic, Faultline terminates the TLS connection
itself and presents a certificate it signed with a local certificate authority.
Your application only accepts that certificate if it trusts the CA. When it does
not, the call fails during the handshake, before any request exists, so the
application sees a bare TLS error and Faultline sees a client that hung up.

## Runtime by runtime

| Runtime | Under `faultline run` | In a container or a process Faultline did not start |
| --- | --- | --- |
| Go on Linux | nothing to do; reads `SSL_CERT_FILE` | set `SSL_CERT_FILE` to the CA path |
| Go on macOS | `faultline ca install`; Go reads the Keychain and no file | same |
| Node | nothing to do; reads `NODE_EXTRA_CA_CERTS` | set `NODE_EXTRA_CA_CERTS` |
| Python `requests`, `httpx` | nothing to do; read `REQUESTS_CA_BUNDLE` | set `REQUESTS_CA_BUNDLE` |
| curl, OpenSSL-based tools | nothing to do; read `CURL_CA_BUNDLE` or `SSL_CERT_FILE` | set the same |
| git | nothing to do; reads `GIT_SSL_CAINFO` | set `GIT_SSL_CAINFO` |
| Java | nothing to do; `JAVA_TOOL_OPTIONS` points at a trust store Faultline builds | `faultline trust java -- <command>` as the entrypoint, see [below](#a-process-faultline-did-not-start) |
| .NET, Rust, Ruby, anything else | depends on what its TLS library reads; `faultline ca install` covers the ones that read the system store | add `faultline ca path` to whatever store it reads |
| Browsers | not the child; use an [explicit route](attach.md#browsers-and-frontends) instead | `faultline ca install`, and Firefox has its own store |
| Clients that pin certificates | cannot be intercepted; use connection faults | same |

`faultline compose inject` sets the container column's variables for the
services it is given, so a Go, Node, Python or curl-based service under Compose
needs no change to its image. The rest of this page is the detail behind each
row.

You get here from a line like this during a run:

```
trust: api.stripe.com: client did not trust the Faultline CA; see docs/trust.md (Faultline set SSL_CERT_FILE, REQUESTS_CA_BUNDLE, CURL_CA_BUNDLE, NODE_EXTRA_CA_CERTS, GIT_SSL_CAINFO)
```

or from the same sentence on a host in `GET /api/upstreams`:

```json
{ "host": "api.stripe.com", "tier": "intercepted", "requests": 3, "hint": "client did not trust the Faultline CA; see docs/trust.md (...)" }
```

The hint names the variables Faultline set for the child, because those are the
things you do not need to do again. If `SSL_CERT_FILE` is in that list and the
handshake still failed, pointing the runtime at the CA is not the fix: either
the runtime ignores the variable, or it is not reading the CA you think it is.

## What `faultline run` does for you

`faultline run` creates the CA on first use and hands the child every variable
in the `faultline run` column above: `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`,
`CURL_CA_BUNDLE`, `NODE_EXTRA_CA_CERTS` (which Node reads in addition to its
built-in roots), `GIT_SSL_CAINFO`, and `JAVA_TOOL_OPTIONS`. They are set only
while Faultline is intercepting. Without a CA the certificates the child sees
are the real ones, so its own bundle is still the right answer and Faultline
leaves the variables alone.

The JVM cannot be handed a single extra certificate, so Faultline copies the
JDK's own `cacerts`, imports the Faultline CA into the copy with the JDK's
`keytool`, and points the child at that. Everything the JDK trusted before is
still trusted. The store is rebuilt only when the CA or `cacerts` is newer than
it, and it lives beside the CA in the config directory.

## A process Faultline did not start

A service in its own container gets the proxy address and the CA from outside,
and nothing sets those variables for it. Most runtimes need only the two: the
proxy variables in the environment and the certificate in the one their runtime
reads, from the table above.

A JVM needs the trust store built first, which is what `faultline trust java`
is for. Pointed at a mounted CA it does what `faultline run` does for a child -
copy `cacerts`, add the CA, render `JAVA_TOOL_OPTIONS` - and then runs the
command after `--`, so it can be the image's entrypoint:

```dockerfile
ENTRYPOINT ["faultline", "trust", "java", "--", "java", "-jar", "/app/app.jar"]
```

With no command it prints the assignment for a shell to export instead. The
certificate is the only half of the CA it needs, so the CA directory can be
mounted read-only; `--ca` names a certificate somewhere else. The trust store
is written to the temporary directory, or wherever `--out` says. [examples/compose](../examples/compose) is the whole
arrangement.

## Runtimes that need more

**Go on macOS.** Go reads roots from the Keychain on macOS and from no file at
all: `crypto/x509` does not build its Unix file-based loader for darwin, so
`SSL_CERT_FILE` is ignored. A Go program on macOS needs the CA in the system
trust store - `faultline ca install` - and there is no per-process alternative.
On Linux, `SSL_CERT_FILE` is enough.

**Browsers.** A browser is not the child process and does not read any of these
variables. It also is not usually the traffic you want: see
[attach.md](attach.md#browsers-and-frontends) for why, and for the explicit
route that puts Faultline in front of the dependency instead. If you do want the browser itself
to trust the CA, install it system-wide, and note that Firefox keeps its own
store and needs the certificate imported in its own settings.

**Pinned clients.** A client that pins its upstream's certificate, or its
issuer, rejects any substitute by design, and trusting the Faultline CA changes
nothing. There is no way around this, and there should not be. Use connection
faults instead, which act on the tunnel without terminating TLS: `refuse` and a
host-only `delay` work at `encrypted` tier. Or run with `--intercept=false` and
accept that only connection faults apply.

**Anything with its own store.** Some runtimes and images ship a bundle of their
own and read no environment variable at all. Faultline cannot know about those;
the certificate path from `faultline ca path` is what you add to whatever store
the thing uses.

## Trusting the CA system-wide

```
faultline ca install
```

prints the steps for your operating system and, on macOS and Linux, offers to
run them after an explicit yes. Faultline never touches the OS trust store
without that confirmation. `faultline ca path` prints the certificate if you
would rather do it by hand or add it somewhere else.

Installing the CA means every program on the machine will accept certificates
Faultline signs, but only while Faultline is the one serving them: it signs
nothing unless traffic is going through it, and the key is created readable by
its owner alone in the per-user config directory. Uninstalling it is the reverse
of whatever `ca install` printed.

## Checking that it worked

The hint disappears on its own. It stands while the last handshake for that host
was rejected, and clears as soon as a request comes through the tunnel, so a
`GET /api/upstreams` after trusting the CA is the confirmation - the host should
be at `tier: intercepted` with no `hint`, and its events should carry real
methods and paths rather than a bare `CONNECT`.
