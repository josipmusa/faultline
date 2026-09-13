# compose-transparent

Faultline attached to an application that was never told about it. No proxy
variables, no entrypoint helper, no change to the application's image at all:
the application shares Faultline's network namespace, and iptables inside that
namespace sends its outbound port 80 and 443 traffic through Faultline on the
way out.

```
cd examples/compose-transparent
docker compose up --build
open http://localhost:9000
```

The Go example calls `https://httpbin.org/get` every two seconds. The calls
appear in the UI at tier `intercepted`, with the method, the path and the
response, and the application's own log line next to them says what it got.

Compare with [../compose](../compose), which attaches the same companion the
other way, with `HTTP_PROXY` and a trust store built at start-up.

## What is in it

| Service | What it is |
| --- | --- |
| `faultline-ca` | runs `faultline ca init` once and exits, so the CA exists before anything needs it |
| `faultline` | the proxy and the UI, built from the `transparent` image target, with `NET_ADMIN` |
| `app` | [the Go example](../go-client), in its own image, joined to the proxy's network namespace |

## What makes it transparent

Three lines, and none of them are in the application's image.

**`--transparent` on the proxy, with `NET_ADMIN`.** Faultline writes an
iptables chain in its own network namespace that redirects outbound 80 and 443
to two listeners of its own, and takes the chain back out when it stops.

**`network_mode: "service:faultline"` on the app.** The application's network
namespace *is* Faultline's, so those rules apply to everything it sends. It
keeps its own filesystem, its own processes and its own user; only the network
is shared. The cost is that the application can no longer publish ports of its
own - anything it serves is published by the `faultline` service instead.

**A uid the application does not use.** Faultline's own calls to the upstream
leave the same namespace, so the rules exempt the uid it runs as, 65532.
Anything else running as 65532 in that namespace would be mistaken for
Faultline and pass through unrecorded. The app here runs as root, which is the
default and is not 65532.

## The three ways a TLS call can be seen

`SSL_CERT_FILE` on the app service is the one environment variable in the
example, and it is not what attaches the application to Faultline. It is
trust: Go reads the CA from it, so Faultline can terminate the TLS and the
event carries the method, the path and the status. That is tier `intercepted`,
and it is what this example shows as it stands.

Take the CA away and there are two different outcomes, which are worth not
confusing:

**The application stops trusting the CA, but Faultline still has one.** Comment
out `SSL_CERT_FILE` and recreate the app. Faultline goes on terminating the TLS,
the application refuses the certificate it is offered, and the call fails
before it is made:

```
x509: certificate signed by unknown authority
```

The event is still tier `intercepted`, and carries the reason rather than a
status: `client rejected certificate; CA not trusted or pinned`. This is not
the encrypted tier, it is a trust problem, and Faultline says which it is.

**Faultline has no CA at all.** Bring the stack up without the `faultline-ca`
service, or against a Faultline whose CA directory is empty. Then HTTPS is
tunnelled blindly: the calls succeed, the application is none the wiser, and
the events are tier `encrypted` with the host and nothing else. Only connection
faults apply there - a `status` or `header` rule is accepted and never fires,
and the tier on the event is what explains why.

Other runtimes take the CA from somewhere else - see
[docs/trust.md](../../docs/trust.md).

## Break something

```
curl -X POST localhost:9000/api/rules -H 'content-type: application/json' \
  -d '{"id":"httpbin-down","name":"httpbin is down","match":{"host":"httpbin.org"},
       "fault":{"type":"status","code":503}}'

docker compose logs -f app
```

The application's own log starts reporting 503s, from a rule written after it
started, against a service it still believes it is calling directly.

## Stopping

```
docker compose down -v
```

The redirect rules live in the proxy container's network namespace and go with
it; nothing is left behind on the host.

## When to use which attach mode

Transparent mode when the application cannot be configured: a closed-source
image, a runtime that ignores the proxy variables, a client library that was
never given a proxy setting. Proxy variables when it can, because they need no
capabilities, no shared namespace and no Linux.
