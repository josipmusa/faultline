# node-fetch

A Node program that calls one URL in a loop with the native `fetch` and logs
the status, size and duration of every call. No dependencies, so there is
nothing to install.

## Start it

```
node examples/node-fetch/index.js
```

Through Faultline:

```
faultline run -- node examples/node-fetch/index.js
```

## Options

| Option | Default | Meaning |
| --- | --- | --- |
| `--url` | `https://httpbin.org/get` | the URL to call |
| `--every` | `2000` | milliseconds to wait between calls |
| `--count` | `0` | stop after this many calls; `0` keeps going until interrupted |
| `--timeout` | `30000` | milliseconds before giving up on a single call |

Ctrl-C ends the loop.

## Why this example exists

Node's native `fetch` is built on undici, which does **not** read `HTTP_PROXY`
and `HTTPS_PROXY` the way `http.request`, axios and curl do. It does read
`NODE_EXTRA_CA_CERTS`, so trust is not the problem; reaching the proxy at all
is. This example is the one Faultline's Node support is verified against.

`faultline run` closes the gap by setting `NODE_USE_ENV_PROXY=1`, which makes
`fetch` read the proxy variables like everything else. It exists in Node 24
and is backported to recent 22.x; on a Node old enough to lack it the variable
is ignored and native `fetch` calls go direct, so they do not appear in
Faultline while `http.request` and libraries built on it still do. Node prints
an experimental warning about `EnvHttpProxyAgent` on the first call, which is
expected and harmless.

Native `fetch` sends even `http://` requests through the proxy as a `CONNECT`
tunnel, which Faultline does not yet handle, so use an `https://` URL here.
