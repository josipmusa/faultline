# faultline-proxy

The [Faultline](https://github.com/josipmusa/faultline) binary, on npm.

Faultline sits between an application and the services it depends on, shows
every outbound call live, and can slow those calls down, fail them, cut them
off, or make them flaky, so you can see how the application copes before a real
outage does it for you.

```
npx faultline-proxy run -- npm run dev
```

Or pin it to the project, which is what most people want, so everyone working
on it and CI all get the same version:

```
npm install --save-dev faultline-proxy
```

```
npx faultline run -- npm run dev
```

Then open http://127.0.0.1:9000.

This package holds a small launcher with no dependencies. The binary itself
arrives in a platform package (`faultline-proxy-linux-x64` and friends) that
npm selects for your machine, so you download one binary rather than six and
nothing runs at install time.

Everything else, including the CLI, the HTTP API, the MCP server and the fault
catalogue, is documented in the
[main repository](https://github.com/josipmusa/faultline).
