# Running Faultline in a container

The image is the same single binary, built with the UI embedded:

```
docker run --rm -p 9000:9000 -p 9001:9001 ghcr.io/josipmusa/faultline
```

The admin UI is then at `http://localhost:9000`, and anything that sends its
traffic through `http://localhost:9001` is recorded:

```
curl -x localhost:9001 http://httpbin.org/get
```

## Why the image says `--bind 0.0.0.0`

Every listener binds localhost by default, on purpose: Faultline sees an
application's credentials and can rewrite its traffic, so reaching it from off
the machine is something to ask for. Inside a container, localhost is the
container, which would make every published port a dead end - so the image's
default command asks for all interfaces:

```
CMD ["serve", "--bind", "0.0.0.0"]
```

The flag is not container-only. `faultline serve --bind 0.0.0.0` does the same
on a VM, and it widens all three listeners together: the admin server, the
forward proxy, and every explicit route. Publish the ports deliberately. On a
shared network, anyone who can reach port 9000 can add a rule, and anyone who
can reach 9001 can send traffic through Faultline.

## Keeping the CA

HTTPS is only intercepted when the local CA exists, and a CA that is recreated
on every `docker run` is a CA nothing can trust for long. It lives in
`/var/lib/faultline`, which is declared a volume:

```
docker volume create faultline-ca
docker run --rm -v faultline-ca:/var/lib/faultline ghcr.io/josipmusa/faultline ca init
docker run --rm -v faultline-ca:/var/lib/faultline -p 9000:9000 -p 9001:9001 \
  ghcr.io/josipmusa/faultline
```

The banner says which of the two happened:

```
tls: intercepting HTTPS with CA "/var/lib/faultline/ca.crt"
tls: passing HTTPS through, connection faults only (run faultline ca init to intercept)
```

To hand the certificate to a client that has to trust it, copy it out of the
volume - the image has no shell, so `docker cp` from a running container is the
way:

```
docker cp <container>:/var/lib/faultline/ca.crt ./faultline-ca.crt
curl -x localhost:9001 --cacert ./faultline-ca.crt https://httpbin.org/get
```

See [trust.md](trust.md) for what each runtime needs beyond the file itself.

## Rules from a file

The working directory is `/work`, and `faultline.yaml` is read from the working
directory, so a project's rules and scenarios come along with a mount:

```
docker run --rm -v "$PWD":/work -p 9000:9000 -p 9001:9001 \
  ghcr.io/josipmusa/faultline
```

Rules added through the UI or the API are written back to that file, as they are
outside a container.

## Next to an application, in Compose

The image is most useful as a companion container: the application keeps its own
image and start command, and its calls go through the Faultline beside it. It
needs the proxy address as environment variables, the CA volume mounted
read-only, and - for a JVM, which reads neither of those on its own - the
`faultline trust java` entrypoint helper this image also carries:

```dockerfile
COPY --from=ghcr.io/josipmusa/faultline /usr/local/bin/faultline /usr/local/bin/faultline
ENTRYPOINT ["faultline", "trust", "java", "--", "java", "-jar", "/app/app.jar"]
```

It builds a trust store from the mounted CA by copying the JDK's own `cacerts`
and adding to the copy, renders `JAVA_TOOL_OPTIONS` with that store and with the
proxy variables as the system properties a JVM does read, and then runs the
command after `--`. With no command it prints the assignment instead, for a
shell to export.

[examples/compose](../examples/compose) is the whole arrangement, working:
Faultline, a one-shot service that creates the CA before either of the others
starts, and the Spring Boot example unchanged.

## Ports

| Port | What |
| --- | --- |
| 9000 | admin API, UI, WebSocket, MCP |
| 9001 | forward proxy, the one `HTTP_PROXY` points at |
| 9100 | the first explicit route, then upwards |

Routes are declared with `--route name=url`, which means overriding the default
command:

```
docker run --rm -p 9000:9000 -p 9100:9100 ghcr.io/josipmusa/faultline \
  serve --bind 0.0.0.0 --route stripe=https://api.stripe.com
```

## Images

Tags publish `ghcr.io/josipmusa/faultline:<version>`, `:<major>.<minor>`, and
`:latest` for a release without a pre-release suffix, for `linux/amd64` and
`linux/arm64`. To build it yourself:

```
docker build -t faultline .
```
