# Frontends

`faultline run -- npm run dev` wraps a dev server, and the calls the dev server
makes go through Faultline. The calls the *page* makes do not. This document
explains why, and what to do instead.

## Why the wrapper does not catch browser traffic

`faultline run` works by starting your command as a child process with
`HTTP_PROXY`, `HTTPS_PROXY` and the CA variables set in its environment. That
environment belongs to the child and to whatever the child starts. Your browser
is not the child: it was already running, it has its own proxy settings from the
operating system, and it has its own list of trusted certificate authorities.
Nothing Faultline hands the dev server reaches it.

So `fetch("https://api.stripe.com/v1/charges")` from a React component goes
straight from the browser to Stripe. Faultline never sees it, and no rule can
touch it.

## What to do instead: an explicit route

Most dev servers already proxy API calls for you, precisely so the page can call
a relative path and avoid CORS. That proxying happens in the dev server, in
Node, which *is* the child process. The page calls `/api/charges`, the dev
server calls the upstream, and that second call is one Faultline can be put in
front of.

Give the upstream an explicit route and point the dev proxy at it:

```
faultline run --route api=https://api.stripe.com -- npm run dev
```

`run` prints the port it picked:

```
route api: http://127.0.0.1:9100 -> https://api.stripe.com
```

Then set the dev server's proxy target to that address. In Vite:

```ts
// vite.config.ts
export default defineConfig({
  server: {
    proxy: {
      "/api": {
        target: process.env.API_TARGET ?? "https://api.stripe.com",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ""),
      },
    },
  },
});
```

```
API_TARGET=http://localhost:9100 faultline run --route api=https://api.stripe.com -- npm run dev
```

Every call the page makes now runs through Faultline, with the page's own
loading, error and retry states showing what a user would actually see. A two
second delay rule on that route is the fastest way to find out whether your
loading state exists.

Routes start at port 9100 and are numbered in the order you declare them, so the
target stays the same across runs. `--route` is repeatable, once per upstream.

Ports are the only thing that changes in your project, and only in dev
configuration: `API_TARGET` unset means the app talks to the real upstream, so
nothing about the setup leaks into production.

## Why not just point the browser at the proxy

You can configure a browser to use `http://localhost:9001` as its proxy and
install the Faultline CA in its trust store, and Faultline will then see
everything - including every request the browser makes to everything else you
have open. That is a lot of noise, it is per-browser manual setup, and it
changes the machine rather than the project. The route costs one environment
variable and shows exactly the traffic the application depends on, so that is
what Faultline recommends.

## Frameworks other than Vite

The mechanism is the same wherever the dev server does the forwarding:

| Dev server | Where the target lives |
| --- | --- |
| Vite | `server.proxy` in `vite.config.ts` |
| Next.js | `rewrites()` in `next.config.js`, or the API route's own fetch |
| Create React App | `proxy` in `package.json`, or `setupProxy.js` |
| Angular CLI | `proxyConfig` JSON |

A framework that renders on the server, Next.js included, makes its calls in the
child process to begin with, so those are caught by the wrapper alone, no route
needed. Only calls the browser starts need one.

## See also

- [`examples/vite-frontend`](../examples/vite-frontend) - a working version of
  everything above.
