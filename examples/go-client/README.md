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
| `-retries` | `0` | repeat a failed call this many times before moving on |
| `-backoff` | `250ms` | wait this long after the first failure, doubling after each one |

Ctrl-C ends the loop.

## Polling and retrying are not the same thing

By default this program does not retry. A failed call is logged and the loop
waits `-every` before making the next one, so the gap Faultline sees between
two attempts is the poll interval, and `max_retry_wait_ms` in the report is
that interval rather than a backoff.

`-retries` makes it a client that genuinely backs off: a failed call, or one
answered with a 5xx, is repeated up to that many times, waiting `-backoff`
after the first failure and twice as long after each one. That is the shape
the report's retry numbers are meant to describe.

```
go run ./examples/go-client -count 1 -retries 3 -backoff 500ms
```

With a `first_n: 2` `status` 503 rule on the upstream, that one call is three
attempts, two of them faulted, and the report's longest retry wait is the
second backoff of a second.

## Why it needs no Faultline-specific code

`http.DefaultTransport` honours `HTTP_PROXY` and `HTTPS_PROXY`, and Go reads
the CA bundle from `SSL_CERT_FILE`. `faultline run` sets all of those, so the
program has no idea Faultline is there.
