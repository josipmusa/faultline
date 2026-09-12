# compose

Faultline as a companion container, next to the application it watches. This is
how a service is attached in the shape it actually runs in: the application
keeps its own image and its own start command, and everything it calls goes
through the Faultline beside it.

```
cd examples/compose
docker compose up --build
```

Then, in another terminal:

```
curl localhost:8080/rest-client
curl localhost:8080/rest-client-retrying
open http://localhost:9000
```

The calls appear in the UI at tier `intercepted`, with the method, the path and
the body of a request the application made over HTTPS.

## What is in it

| Service | What it is |
| --- | --- |
| `faultline-ca` | runs `faultline ca init` once and exits, so the CA exists before anything needs it |
| `faultline` | the proxy and the UI, publishing 9000, reachable on the compose network as `faultline:9001` |
| `app` | [the Spring Boot example](../spring-boot), unchanged, in its own image |

The CA lives in a volume both containers mount: read-write for Faultline, which
signs with it, read-only for the application, which only has to trust it.

## What the application had to change

Nothing in the code. Two things around it:

**The proxy address, as environment variables.** `HTTP_PROXY`, `HTTPS_PROXY`
and `NO_PROXY` in `compose.yaml`, pointing at `http://faultline:9001`.

**The CA, and a JVM that will read it.** A JVM reads neither the proxy
variables nor a PEM certificate, so the image's entrypoint is one command in
front of the one it had:

```dockerfile
ENTRYPOINT ["faultline", "trust", "java", "--", "java", "-jar", "/app/app.jar"]
```

`faultline trust java` copies the JDK's own `cacerts`, adds the mounted
Faultline CA to the copy so the service still trusts everything it trusted
before, and renders `JAVA_TOOL_OPTIONS` with that trust store and with the
proxy variables written as the system properties a JVM does read. Then it
executes the command after `--`. The first line the container logs is the JVM
repeating it back:

```
Picked up JAVA_TOOL_OPTIONS: -Dhttp.proxyHost=faultline -Dhttp.proxyPort=9001 ...
```

That line is Faultline having reached the application. Without it, calls go
direct and nothing appears in the UI.

Only the JVM needs this. A Go, Node, Python or curl-based service reads the
proxy variables on its own and takes the CA from `SSL_CERT_FILE` or its
runtime's equivalent - see [docs/trust.md](../../docs/trust.md).

The binary comes from the repository in `app.Dockerfile`, because this example
builds Faultline from source. An image of your own would copy it from the
published one instead:

```dockerfile
COPY --from=ghcr.io/josipmusa/faultline /usr/local/bin/faultline /usr/local/bin/faultline
```

## Break something

With the stack up, from the UI at `http://localhost:9000`, or over the API:

```
curl -X POST localhost:9000/api/rules -H 'content-type: application/json' \
  -d '{"id":"httpbin-down","name":"httpbin is down","match":{"host":"httpbin.org"},
       "fault":{"type":"status","code":503}}'

curl localhost:8080/rest-client            # fails, the way it would in production
curl localhost:8080/rest-client-retrying   # retries, backs off, and still fails
curl localhost:9000/api/sessions/current/report
```

```json
{"total":7,"faulted":5,"retries":4,"max_retry_wait_ms":802,"abandoned":0}
```

A `status` fault reaching an HTTPS call is the part worth noticing: it works
because the application trusts the CA, which is what the entrypoint arranged.
Without that trust the same rule would be accepted and never apply, and the UI
would show the host at tier `encrypted` instead.

## Rules from a file

`faultline.yaml` is read from the working directory, which is `/work` in the
image, so a project's rules and scenarios come along with a mount on the
`faultline` service:

```yaml
    volumes:
      - faultline-ca:/var/lib/faultline
      - ./:/work
```

Mount the directory rather than the file: rules changed through the UI are
written back, and that replaces the file.

## Stopping

```
docker compose down -v
```

`-v` removes the CA volume too, so the next `up` mints a new one. Leave it out
to keep the CA the application already trusts.
