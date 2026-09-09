# vite-frontend

A React page with one button that calls `/api/get`. The Vite dev server
forwards everything under `/api` to `API_TARGET`, `https://httpbin.org` by
default, and the page shows the status, duration and body it got back.

## Start it

```
cd examples/vite-frontend
npm install
npm run dev
```

Then open http://localhost:5173 and click the button.

## Through Faultline

Browser traffic does not go through `faultline run`: the proxy variables it
sets belong to the process it started, and the browser is not that process.
What *is* that process is the Vite dev server, and it makes the `/api` call on
the page's behalf. So point the dev proxy at a Faultline route:

```
faultline run --route api=https://httpbin.org -- npm run dev
```

and start Vite with `API_TARGET` set to that route's port, which `run` prints:

```
API_TARGET=http://localhost:9100 faultline run --route api=https://httpbin.org -- npm run dev
```

Every call the page makes then runs through Faultline, with the page's own
loading and error states showing what a user would see.
[docs/frontends.md](../../docs/frontends.md) explains why the route is needed
and how to do the same thing in other frameworks.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `API_TARGET` | `https://httpbin.org` | where the dev server forwards `/api/*` |

The `/api` prefix is stripped before forwarding, so `/api/get` arrives at the
target as `/get`.
