# spring-boot

A Spring Boot application with two endpoints that make the same outbound call
with the two HTTP clients a Spring service is likely to use:

| Endpoint | Client | Runs on |
| --- | --- | --- |
| `GET /rest-client` | `RestClient` | the JDK's own HTTP stack |
| `GET /web-client` | `WebClient` | Reactor Netty |

Both call `example.upstream` + `example.path`, `https://httpbin.org/get` by
default, and answer with the status, duration and body size they saw. Failures
are not caught: a fault injected into the upstream surfaces as a failed
request here, the way the real thing would.

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

Then, in another terminal:

```
curl localhost:8080/rest-client
curl localhost:8080/web-client
```

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
`RestClient` goes through Faultline as soon as those are set. Reactor Netty,
which `WebClient` is built on, ignores them unless the client is built with
`proxyWithSystemProperties()`. Having both here keeps the difference visible.

## A warning you can ignore

On macOS, Netty logs `Unable to load ... MacOSDnsServerAddressStreamProvider`
at ERROR level the first time `WebClient` is used. It falls back to the system
resolver and the call succeeds; adding the native resolver is not worth a
dependency in an example.
