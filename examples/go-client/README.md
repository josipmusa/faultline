# go-client

A Go program that calls one URL in a loop and logs the status, size and
duration of every call. It is the quickest way to get traffic flowing through
Faultline, and the logged durations make a `delay` fault obvious without
looking at the UI.

## Start it

```
go run ./examples/go-client
```

Through Faultline:

```
faultline run -- go run ./examples/go-client
```

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-url` | `https://httpbin.org/get` | the URL to call |
| `-every` | `2s` | how long to wait between calls |
| `-count` | `0` | stop after this many calls; `0` keeps going until interrupted |
| `-timeout` | `30s` | give up on a single call after this long |

Ctrl-C ends the loop.

## Why it needs no Faultline-specific code

`http.DefaultTransport` honours `HTTP_PROXY` and `HTTPS_PROXY`, and Go reads
the CA bundle from `SSL_CERT_FILE`. `faultline run` sets all of those, so the
program has no idea Faultline is there.
