# spring-boot

A Spring Boot application with three endpoints that make the same outbound
call. Two of them use the two HTTP clients a Spring service is likely to use,
and the third retries:

| Endpoint | Client | Runs on | On failure |
| --- | --- | --- | --- |
| `GET /rest-client` | `RestClient` | the JDK's own HTTP stack | surfaces it |
| `GET /web-client` | `WebClient` | Reactor Netty | surfaces it |
| `GET /rest-client-retrying` | `RestClient` | the JDK's own HTTP stack | retries, then surfaces it |

All three call `example.upstream` + `example.path`, `https://httpbin.org/get`
by default, and answer with the status, duration and body size they saw. The
first two do not catch failures: a fault injected into the upstream surfaces as
a failed request here, the way the real thing would.

Java 21, Maven, no dependencies beyond the two Spring starters.

## Start it

```
cd examples/spring-boot
./mvnw spring-boot:run
```

Through Faultline, from the same directory:

```
faultline run -- ./mvnw spring-boot:run
```

Every JVM prints `Picked up JAVA_TOOL_OPTIONS: ...` when it starts that way.
That line is the JVM saying Faultline reached it.

Then, in another terminal:

```
curl localhost:8080/rest-client
curl localhost:8080/web-client
curl localhost:8080/rest-client-retrying
```

## The endpoint that recovers

`/rest-client-retrying` makes the same call, but repeats it when it fails,
waiting 200ms after the first failure and twice as long after each one, and
gives up after four attempts. It answers with `attempts` and `waitedMs`
alongside the usual fields, and logs every attempt and every wait, so what the
application did can be read against what Faultline's report says it did:

```
$ curl localhost:8080/rest-client-retrying
{"client":"RestClient","status":200,"durationMs":1287,"bytes":312,"attempts":3,"waitedMs":600}
```

The backoff is a loop in the controller rather than a retry library, because
what it costs the caller is the thing worth reading in an example, and it is
what `max_retry_wait_ms` in the report counts. A 4xx is not repeated: the
upstream understood the request and rejected it.

It is worth keeping beside `/rest-client`. The same 503 rule against the two of
them is the difference between an application that survives a dependency
faltering and one that does not, and `max_retry_wait_ms` is zero for the second
because nothing was ever repeated.

## Configuration

`src/main/resources/application.properties`:

| Property | Default | Meaning |
| --- | --- | --- |
| `server.port` | `8080` | where this app listens |
| `example.upstream` | `https://httpbin.org` | the dependency it calls |
| `example.path` | `/get` | the path on that dependency |

## Why two clients

The JVM's own HTTP stack reads the `http.proxyHost` and `https.proxyHost`
system properties, and the trust store from `javax.net.ssl.trustStore`, so
`RestClient` goes through Faultline as soon as those are set, and
`faultline run` sets them through `JAVA_TOOL_OPTIONS` without this project
being changed.

Reactor Netty, which `WebClient` is built on, is the exception: it ignores
those properties unless the client asks for them, so a default `WebClient`
calls the upstream directly while everything else in the same JVM goes through
Faultline. One line fixes it, and this example is set up that way:

```java
WebClient client = builder
        .clientConnector(new ReactorClientHttpConnector(
                HttpClient.create().proxyWithSystemProperties()))
        .build();
```

Trust is not part of that difference. Both clients use the JDK trust store
that `javax.net.ssl.trustStore` names, which is the copy of the JDK's own
`cacerts` that `faultline run` builds with the Faultline CA added, so the
application still trusts everything it trusted before.

## A warning you can ignore

On macOS, Netty logs `Unable to load ... MacOSDnsServerAddressStreamProvider`
at ERROR level the first time `WebClient` is used. It falls back to the system
resolver and the call succeeds; adding the native resolver is not worth a
dependency in an example.
