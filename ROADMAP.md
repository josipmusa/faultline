# Faultline — Build Roadmap

This roadmap breaks the product described in SPEC.md into stages, and each stage into small tasks. It is written for coding agents working one task at a time with a human verifying gates.

## How to use this document

- Work **one task at a time**, in order, unless a task says it is independent.
- Every task has a **Verify** block. `Agent` means the agent proves it with tests or commands and reports the output. `Manual` means the human runs the steps and confirms what they see. Do not start the next task until the current one is verified.
- Every stage ends with a **Stage gate**, always manual. Do not start the next stage until the gate passes.
- When a task is done, tick its checkbox here and note anything surprising under it in one line. Never rewrite a task's scope silently; propose changes in the conversation.
- Fixed decisions live in AGENTS.md. If a task seems to conflict with one, stop and ask.

Conventions used below:

- Admin address is `http://localhost:9000` (UI, HTTP API, WebSocket, MCP over HTTP).
- Forward proxy listens on `localhost:9001`.
- Explicit routes get their own ports starting at `9100`.
- Config file is `faultline.yaml` in the working directory. Per-user state (CA, truststores) lives in the OS config directory, `~/.config/faultline` on Linux.

---

## Stage 0 — Project skeleton

Goal: a buildable, testable, lintable Go module with a CLI that does nothing useful yet.

- [x] **0.1 Go module and CLI skeleton.** Create `go.mod` (module `github.com/josipmusa/faultline`, Go 1.25), `cmd/faultline/main.go` using Cobra with subcommands `serve`, `run`, `rule`, `scenario`, `ca`, `mcp`, `version`, each printing "not implemented" for now. Add a `Makefile` with `build`, `test`, `lint`, `run` targets.
  - Verify (Agent): `make build` produces `./bin/faultline`; `./bin/faultline --help` lists the subcommands; `./bin/faultline version` prints a version string.
  - Note: `make` is not installed on the dev machine, so the targets were verified by running the commands they wrap. Version is injected with `-ldflags -X main.version`.
- [x] **0.2 Lint and test tooling.** Add `golangci-lint` config with a sensible default set, a `go vet` step, and one trivial test so the pipeline has something to run.
  - Verify (Agent): `make lint` and `make test` both pass and exit 0.
  - Note: golangci-lint v2 config schema (`version: "2"`), pinned to v2.13.2 and installed by a new `make tools` target. `-race` needs a C compiler, absent here, so tests were verified without it.
- [x] **0.3 CI workflow.** GitHub Actions workflow running lint, test, and build on push and pull request, Linux and macOS.
  - Verify (Agent): workflow YAML validates with `actionlint` if available, otherwise by careful review; the human will see it run after the first push.
  - Note: `actionlint` exits clean. CI runs `make tools`/`lint`/`test`/`build` so the Makefile stays the single source of truth.
- [x] **0.4 Repository documents.** `README.md` stub with the one-paragraph pitch from SPEC.md and a "status: pre-alpha" banner. `LICENSE` (MIT). `CONTRIBUTING.md` stub pointing to AGENTS.md.
  - Verify (Agent): files exist and README renders sanely.
  - Note: LICENSE copyright reads "2026 Josip Musa" — correct it if that is not the right holder.

**Stage 0 gate (Manual).** Clone into a fresh directory, run `make build && ./bin/faultline --help`. Expect the help text with all subcommands and no warnings.

---

## Stage 1 — Core engine and explicit routes

Goal: traffic flows through Faultline in the simplest attach mode, faults can be applied, and everything observed is recorded. No UI yet; the HTTP API is the interface.

- [x] **1.1 Rule model.** Define the domain in `internal/rules`: `Rule` with an id, name, enabled flag, `Match` (upstream host, optional method, optional path glob, optional header equals), and a single `Fault`. Faults for this stage: `delay` (ms, optional jitter ms) and `status` (code, optional body). Values are immutable; updates return new copies. Include JSON tags matching the API shape in AGENTS.md.
  - Verify (Agent): unit tests for matching cover host only, host plus method, glob patterns like `/orders/*` and `/v1/**`, header match, and disabled rules never matching.
  - Note: no `behavior` field yet (Stage 1 is one fault per rule) and no `Validate()` — validation belongs at the API boundary in 1.6. Empty match fields, `host` included, match anything. `-race` still needs a C compiler that is absent here, so tests ran without it.
- [x] **1.2 Rule store.** An in-memory, concurrency-safe store with list, get, add, update, delete, enable, disable. Emits a change notification channel that later stages will use for hot reload and UI refresh.
  - Verify (Agent): unit tests including a concurrent add and read test run with `-race`.
  - Note: slice-backed so insertion order is preserved, which is the order 1.4 will consult rules in. One coalescing `Changes()` channel (buffered 1, non-blocking send), so a reader must re-read the store rather than count signals; the admin server fans out to multiple websocket clients in 1.7. No id generation — 1.6 must mint an id for `POST /api/rules`, whose example body carries none. The concurrent test could not run under `-race` here (no C compiler on this machine); CI runs `make test` with `-race` on Linux and macOS.
- [x] **1.3 Event recording.** `internal/events`: an `Event` per proxied request with id, timestamp, upstream host, method, path, status, duration, bytes in and out, `faulted` flag, applied rule id, and a `tier` field (`plain`, `intercepted`, `encrypted`) telling how much Faultline could see. A ring buffer with configurable size, default 1000, plus a fan-out subscriber mechanism. Port the good parts of the previous projects' event manager.
  - Verify (Agent): unit tests for ring buffer wraparound, subscriber fan-out, and slow-subscriber drop behavior under `-race`.
  - Note: ported the ring arithmetic and the buffered-channel fan-out from both predecessors, dropped the `SourceServiceId` partitioning, and fixed two bugs in them — `NewEventBuffer(0)` divided by zero on first add, and `Close()` never cleared the listener slice so a second call closed a closed channel. `Subscribe()` returns a `*Subscription` that closes itself rather than making the caller hand the channel back. The ring buffer is the source of truth and the stream is best effort: a subscriber behind by more than 100 events loses them instead of blocking the request path. No `Clear()` (1.6 needs it for `DELETE /api/events`), no filtering (1.6), and no event id generation — 1.4 must mint ids. `duration_ms` is a whole number, so sub-millisecond round trips record as 0. `-race` still unavailable locally (no C compiler); CI covers it.
- [x] **1.4 Fault pipeline.** `internal/faults`: a `RoundTripper` wrapper that looks up the matching rule for a request, applies the fault, and records an event whether faulted or not. Delay must respect request context cancellation (no bare `time.Sleep`). Status fault short-circuits with a synthetic response and a `Faultline-Fault: <rule id>` header so users can tell a synthetic response from a real one. Start from `ChaosTransport` in `../resilience-proxy/internal/proxy/interceptor.go`.
  - Verify (Agent): unit tests using `httptest.Server` as upstream: no rule passes traffic unchanged; delay rule adds at least the configured delay; delay is cancelled promptly when the request context is cancelled; status rule returns the configured code and header without hitting upstream.
  - Note: `ChaosTransport` read the matched rule out of a context value and slept with a bare `time.Sleep`; this version looks the rule up itself from the store and waits on a timer against the request context. Event ids had to be minted here, so `events.NextID()` (atomic counter) was added — the predecessor used `UnixNano`, which collides under concurrency. `bytes_in`/`bytes_out` come from `Content-Length` and are 0 when the length is unknown.
- [x] **1.5 Explicit route listener.** `internal/proxy/reverse`: given an upstream URL and a local port, start a reverse proxy through the fault pipeline. Support multiple routes at once. Preserve the upstream `Host` header semantics correctly and strip hop-by-hop headers. Start from the `httputil.ReverseProxy` director in `../resilience-proxy/internal/proxy/handler.go`.
  - Verify (Manual): run `./bin/faultline serve --route stripe=https://httpbin.org --route-port stripe=9100`; run `curl -i localhost:9100/get`. Expect a 200 JSON response from httpbin with your request echoed back.
  - Note: ran the manual command; httpbin echoed `"Host": "httpbin.org"` and `"url": "https://httpbin.org/get"`, so the Host rewrite is right. Uses `ReverseProxy.Rewrite`/`SetURL` rather than the predecessor's `Director`, which also gets hop-by-hop stripping for free — except `TE: trailers`, which RFC 7230 requires be forwarded and net/http keeps; there is a test pinning that. The verify block invokes `faultline serve --route/--route-port`, so the `serve` placeholder was replaced with a real command; it starts routes only, since the admin server is 1.6, and it errors out if no `--route` is given. Routes without an explicit port are numbered from 9100 upwards, skipping ports already claimed. Signal handling lives in `serve` for now and 1.8 takes it over.
- [x] **1.6 HTTP API for rules and events.** `internal/admin`: JSON endpoints `GET/POST /api/rules`, `GET/PUT/DELETE /api/rules/{id}`, `POST /api/rules/{id}/enable|disable`, `GET /api/events?limit=&host=&faulted=`, `DELETE /api/events`, `GET /api/upstreams` (distinct hosts seen with tier and counts), `GET /api/health`. Validate every input and return structured errors `{ "error": "...", "field": "..." }`.
  - Verify (Agent): handler tests for every endpoint including validation failures. Verify (Manual): with the route from 1.5 running, `curl -X POST localhost:9000/api/rules -d '{"name":"slow","match":{"host":"httpbin.org"},"fault":{"type":"delay","ms":2000}}'`, then `time curl localhost:9100/get`. Expect roughly two seconds. Then `curl localhost:9000/api/events` and expect the request listed with `faulted: true`.
  - Note: ran both verifies; the manual one took 2.60s and the event came back `faulted: true` with `rule_id: slow`. Validation lives in `internal/admin/validate.go`, not on the domain type, as 1.1 decided; it names the field (`fault.ms`, `match.host`). A POSTed rule defaults to `enabled: true` — decoding into a pre-enabled `Rule` distinguishes absent from explicit `false`, so the verify body works without an `enabled` field — and unknown JSON fields are rejected, which will need revisiting when `behavior` arrives in Stage 3. `POST /api/rules` mints an id by slugifying the name and adding `-2`, `-3` on a clash; an explicit id that already exists is a 409. Events come back oldest first and `limit` keeps the most recent N. `/api/upstreams` reports the tier of the host's most recent event, so a host moves from `encrypted` to `intercepted` when interception starts working. `Recorder.Clear()` was added for `DELETE /api/events`; filtering lives in the admin package since it is query shaped. `serve` now starts the admin server on 9000 as well as the routes, which the manual verify needs. `-race` runs on this machine now, so the whole suite is green under it.
- [x] **1.7 Live event stream.** WebSocket endpoint `/api/events/stream` that sends each new event as `{"type":"event","event":{...}}` and a `{"type":"rules_changed"}` message whenever the rule store changes. Read-only; the socket accepts no commands.
  - Verify (Manual): `websocat ws://localhost:9000/api/events/stream` (or `wscat`), then curl the route a few times. Expect one JSON line per request appearing live, and a `rules_changed` line when you add a rule via the API.
  - Note: ran the manual verify with a throwaway Go client (no `websocat` or `wscat` on this machine); two curls produced two event messages, `POST /api/rules` produced `{"type":"rules_changed"}`, and the next curl came back `faulted: true`. Added `github.com/coder/websocket` — Go has no stdlib WebSocket, and it is zero-transitive-dependency and context-native, which suits the "every network op takes a context" convention better than the predecessors' `gorilla/websocket`. **The task text originally said each event goes out as bare JSON; it is a tagged `admin.Message` envelope instead**, because discriminating on the absence of a `type` field breaks silently the day `events.Event` gains one — 2.2 adds CONNECT events — and because the strict-TypeScript UI wants a discriminated union. A new message kind gets a new `type` and its own optional payload field; the envelope is documented in AGENTS.md. No ordering is promised between an event and a notice — they are separate sources feeding one `select`. `ruleWatcher` in `internal/admin/stream.go` does the fan-out 1.2 anticipated, since `Store.Changes()` has a single reader; its per-client signals coalesce the same way the store's do, so a client re-reads `GET /api/rules` rather than counting signals. The handler subscribes *before* the upgrade, so nothing recorded during the handshake is lost. `CloseRead` gives the read-only property for free: any data message from the client closes the socket with `StatusPolicyViolation`. Writes have a 10s timeout so a stalled reader cannot pin a goroutine and its subscriptions. `Server.Shutdown` closes the watcher, which makes every open stream handler send `StatusGoingAway` and return — hijacked connections are invisible to `http.Server.Shutdown`, so nothing else would. It does not *wait* for those goroutines, though, so on a real Ctrl-C the process can exit before the frame is flushed and the client sees a bare EOF; confirmed by hand. Draining them belongs to 1.8.
- [x] **1.8 Graceful shutdown.** Ctrl-C stops accepting new requests, lets in-flight requests finish within a timeout, closes streams cleanly. Port and clean up the previous shutdown code.
  - Verify (Agent): integration test that starts the server, begins a delayed request, sends the shutdown signal, and asserts the request completes and the process exits within the timeout.
  - Note: `TestServeShutsDownGracefullyOnSignal` in `cmd/faultline/shutdown_test.go` is the Verify, and it sends a real `os.Interrupt` to the test process rather than cancelling a context, so the signal wiring is covered too; it holds its own `signal.Notify` for the length of the test so a stray interrupt can never fall through to the default action and kill the test binary. Also ran it by hand: a 3s delay fault in flight when Ctrl-C landed still returned 200 with a full body, shutdown took 2.66s, and the stream client saw `StatusGoingAway`, not the bare EOF 1.7 recorded. Little was portable from the predecessors: their global `IsShuttingDown()` atomic became a `draining` flag on `admin.Server` guarded by the mutex that already exists, and their `wg.Wait()` for in-flight proxy requests was dropped as redundant, since `http.Server.Shutdown` already waits and our handlers do not spawn goroutines; their `log.Fatalf` on a shutdown error went too. What was genuinely missing: `admin.Server.Shutdown` now waits for open stream goroutines (`enterStream`/`leaveStream` around a `WaitGroup`, `waitFor` bounding the wait by the shutdown context), because a hijacked connection is invisible to `http.Server.Shutdown`. A stream opened while draining gets a 503 rather than a socket that is about to close. **The close path needed bounding:** `coder/websocket`'s `CloseRead` goroutine calls `Close`, which waits on a channel that same goroutine only closes on return, so any close wedges for the library's fifteen second timeout once a client has sent a data message — that is now on the shutdown path, so both closes go through `closeBounded` and are abandoned after a second, by which point the frame is already written. `TestShutdownIsPromptAfterAClientCommand` pins it (15s to 2s). `serve` installs the signal handler before anything binds, shuts routes down before the admin server so the last events reach the streams before they close, and prints `shutdown complete`. Its internal signature took an `adminPort` argument, passed `admin.DefaultPort` by the command and 0 by the test, so the test does not fight a real Faultline for port 9000; no new CLI flag.

**Stage 1 gate (Manual).** Start Faultline with two routes to two different upstreams. Add a delay rule for one and a `503` rule for the other via the API. Curl both. Expect the first to be slow with a real body, the second to be an instant 503 with the `Faultline-Fault` header. List events and see both, correctly attributed. Ctrl-C and expect a clean exit message.

  - Passed 2026-09-08, run and confirmed by the human. Upstreams were `https://httpbin.org` on 9100 and `https://postman-echo.com` on 9101. The dry run beforehand gave: 200 with a real 287 byte httpbin body after 2.6s; 503 in 24ms carrying `Faultline-Fault: echo-is-down` and never contacting postman-echo; both events `faulted: true` with the right `rule_id` and `duration_ms` 2585 against 0; `shutting down` then `shutdown complete` on Ctrl-C.

---

## Stage 2 — Forward proxy and encrypted traffic

Goal: applications reach Faultline with standard proxy settings, and HTTPS works in both tiers.

- [x] **2.1 Plain HTTP forward proxy.** `internal/proxy/forward`: accept absolute-URL requests on `:9001`, run them through the same fault pipeline as routes. Port the hop-header handling from the old service. Reject non-absolute requests with a helpful message.
  - Verify (Manual): `curl -x localhost:9001 http://httpbin.org/get`. Expect a normal response and an event in `/api/events` with tier `plain`.
  - Note: ran the manual verify; httpbin echoed the request with no `Proxy-Connection` header and the event came back `tier: plain`, `faulted: false`, 431ms. A delay rule for the host applied through the proxy too, at 1.64s. **The old service's `cleanHopHeaders` was not portable as written** — it deletes `Connection` in the loop before reading it, so the per-connection headers that `Connection` lists were never removed, and it also ran on the inbound `http.Header` rather than on a clone. `httputil.ReverseProxy` with `Rewrite` does the job correctly in both directions, which is also what 1.5 uses, so the forward handler is a `ReverseProxy` whose destination comes from the request line instead of a fixed upstream. `Rewrite` rather than `Director` matters here for a second reason: it means no `X-Forwarded-*` is added, and a forward proxy has no business announcing its client to the upstream. CONNECT answers `501` naming CONNECT rather than falling into the absolute-URL message, since `curl -x` on an https URL would otherwise get told to use an absolute URL it cannot use; 2.2 replaces that branch. **Routes are no longer required:** `serve` starts the forward proxy unconditionally, so `--route` is optional and `parseRoutes` accepts none — HTTP_PROXY alone is reason enough to run Faultline. New `--proxy-port` flag, defaulting to 9001, so tests can bind port 0. `golangci-lint` was not installed on this machine; installed it, and `make lint` is clean.
- [x] **2.2 CONNECT tunnel with connection-tier faults.** Handle `CONNECT host:443`. Without interception, tunnel bytes both ways. Before dialing, consult rules whose match is host only and whose fault is a connection fault: apply delay, or refuse the connection when the fault says so. Record an event with tier `encrypted`, method `CONNECT`, no path.
  - Verify (Manual): `curl -x localhost:9001 https://httpbin.org/get` works. Add a delay rule for `httpbin.org` and repeat; expect the delay. Add a `refuse` fault (introduced in this task as the first connection fault) and expect curl to report the proxy refused the connection. Events show tier `encrypted`.
  - Note: ran the manual verify; the plain tunnel answered 200 in 0.95s, the same request with a 2000ms delay rule took 2.54s, and with a `refuse` rule curl said `(56) CONNECT tunnel failed, response 502`. All four events came back `method: CONNECT`, `path: ""`, `tier: encrypted`. **A refused connection is answered as `502 Bad Gateway` with `Faultline-Fault: <rule id>`** rather than by dropping the socket: that is what a client behind a proxy sees when the real upstream is not listening, and closing the socket would leave nothing to hang the rule id on. The `refuse` fault also works on the plain tier through the transport, as the same synthetic 502, so it never silently no-ops anywhere. Connection-tier matching lives in `faults.Dialer`, the counterpart of `faults.Transport`: a rule applies to a tunnel only if its match is host-only (`Rule.MatchesConnection`) and its fault is a connection fault (`Fault.IsConnection`); a host-only `status` rule is skipped so the rules behind it still get a turn, and the event's tier says why it did not run. The event is recorded when the tunnel opens, so `duration_ms` covers the delay and the dial and `bytes_in`/`bytes_out` stay 0: the bytes inside are opaque and counting them would hold the event back until the tunnel closes. `Dial` runs on the request context, so a client that gives up mid-delay cancels the wait and the tunnel is never dialed. Hijacked tunnel connections are invisible to `http.Server.Shutdown` and are not drained on exit; as with the WebSocket streams, that belongs to 1.8.
- [ ] **2.3 Local certificate authority.** `internal/tlsmitm`: `faultline ca init` creates a CA key and certificate in the user config directory with a ten-year validity and a clear common name like `Faultline Local CA`. `faultline ca path` prints the certificate path. `faultline ca install` prints instructions per OS for trusting it, and on macOS and Linux offers to do it with an explicit confirmation prompt. Never touch the OS trust store without the prompt.
  - Verify (Manual): run `faultline ca init` twice; expect the second run to say it already exists. `openssl x509 -in $(faultline ca path) -noout -subject -dates` shows the expected subject and dates.
- [ ] **2.4 TLS interception.** When the CA exists and interception is enabled (`--intercept`, default on when CA exists), CONNECT terminates TLS with a per-host leaf certificate minted on demand and cached, then handles the decrypted requests through the full fault pipeline. Events get tier `intercepted` and full method and path. Detect handshake failures that look like pinning or distrust and record an event with a clear `error` such as `client rejected certificate; CA not trusted or pinned`.
  - Verify (Manual): `curl --cacert $(faultline ca path) -x localhost:9001 https://httpbin.org/get`. Expect success and an event with tier `intercepted` and path `/get`. Add a `status 503` rule with path `/get` and repeat; expect 503 with `Faultline-Fault`. Run curl without `--cacert`; expect a TLS error from curl and a clear error event in Faultline.
- [ ] **2.5 Bypass list.** `--no-proxy` style list of hosts Faultline should tunnel blindly without recording, plus a default bypass for `localhost` and the admin port so Faultline never proxies itself. Also expose which upstreams are bypassed in `/api/upstreams`.
  - Verify (Agent): unit tests for bypass matching including wildcards like `*.internal`. Verify (Manual): add a bypass for `httpbin.org` and confirm no events are recorded for it.

**Stage 2 gate (Manual).** With CA initialized, export `HTTPS_PROXY=http://localhost:9001` and `SSL_CERT_FILE=$(faultline ca path)`, then run `curl https://httpbin.org/anything/orders`. Expect success and an intercepted event with the full path. Then delete the env var for the CA and repeat with `-k` removed; expect a clear certificate error event. Finally add a delay rule for the host and a 503 rule for `/anything/*`; verify each triggers as expected.

---

## Stage 3 — The `faultline run` wrapper

Goal: the five-minute experience. One command wraps any start command and traffic appears with no edits.

- [ ] **3.1 Process wrapper.** `faultline run -- <cmd...>` starts the admin server and forward proxy, then runs the child with an environment that sets `HTTP_PROXY`, `HTTPS_PROXY`, `http_proxy`, `https_proxy`, `NO_PROXY` (localhost and the admin port), and forwards stdin, stdout, stderr and signals. Exit code mirrors the child. Print the admin URL on one line at start.
  - Verify (Manual): `faultline run -- curl https://httpbin.org/get`. Expect curl output, a Faultline banner with the admin URL, and an event recorded. `faultline run -- false` exits with code 1.
- [ ] **3.2 CA trust injection for common runtimes.** Extend the child environment with `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`, `NODE_EXTRA_CA_CERTS`, and `GIT_SSL_CAINFO` pointing at the CA. Create the CA automatically on first run with a one-line notice.
  - Verify (Manual): `faultline run -- python3 -c "import urllib.request;print(urllib.request.urlopen('https://httpbin.org/get').status)"` prints 200 and an intercepted event appears.
- [ ] **3.3 Example applications.** Create `examples/` with four minimal apps used for verification from here on: `go-client` (stdlib client calling httpbin in a loop), `node-fetch` (Node 22, native fetch), `spring-boot` (Java 21, one endpoint that calls httpbin with `RestClient`, plus a second with `WebClient`), and `vite-frontend` (React app calling `/api/*` through the Vite dev server proxy). Each has a README with its start command.
  - Verify (Agent): each example builds and runs standalone and makes at least one upstream call successfully without Faultline.
- [ ] **3.4 Go and Python verification.** No new code expected.
  - Verify (Manual): `faultline run -- go run ./examples/go-client`. Expect intercepted events for every call. Add a delay rule and observe the example's logged durations grow.
- [ ] **3.5 Node 22 support.** Research whether Node 22 honors `NODE_USE_ENV_PROXY` for native `fetch` (it exists in Node 24; check backport status for the installed 22.x). If yes, set it. If not, implement a small preload script shipped inside the binary and injected via `NODE_OPTIONS=--import`, that routes native fetch through the proxy variables. Document the fallback clearly in the run banner when neither works.
  - Verify (Manual): `faultline run -- node examples/node-fetch/index.js`. Expect intercepted events. Record in this task's note which mechanism was needed for your Node version.
- [ ] **3.6 Java and Spring Boot support.** Extend the environment with `JAVA_TOOL_OPTIONS` setting `http.proxyHost`, `http.proxyPort`, `https.proxyHost`, `https.proxyPort`, `http.nonProxyHosts`, and a trust store. Build the trust store on first run by copying the JDK `cacerts` (located via `JAVA_HOME` or `java` on PATH) and adding the Faultline CA, so existing trust is preserved. Document that Reactor Netty based `WebClient` needs `proxyWithSystemProperties()`, and set the example up that way.
  - Verify (Manual): `faultline run -- ./gradlew bootRun` (or `mvnw`) in `examples/spring-boot`, then curl its endpoints. Expect intercepted events for both `RestClient` and `WebClient` calls. Add a `503` rule and confirm the Spring app surfaces the failure.
- [ ] **3.7 Frontend guidance via explicit route.** Make `faultline run` accept `--route name=url` too, so a Vite project can be wrapped and its dev proxy target pointed at the route port. Add a short `docs/frontends.md` explaining why browser traffic is not caught by the wrapper and how the route solves it.
  - Verify (Manual): `faultline run --route api=https://httpbin.org -- npm run dev` in `examples/vite-frontend`, with Vite's proxy target set to the route port. Load the page, click the button that calls the API, see the event. Add a 2 second delay and see the UI's loading state.
- [ ] **3.8 Pinning and distrust diagnostics.** When a TLS handshake fails from the client side, the run banner and `/api/upstreams` show a per-host hint: "client did not trust the Faultline CA; see docs/trust.md" including which runtime variables were set. Write `docs/trust.md`.
  - Verify (Manual): `faultline run -- env -u SSL_CERT_FILE curl https://httpbin.org/get`. Expect the hint to appear for `httpbin.org`.

**Stage 3 gate (Manual).** On a fresh machine or clean user account: install the binary, run `faultline run -- go run ./examples/go-client` then the Node and Spring examples in turn. For each, expect intercepted events with no manual trust setup. Time yourself from install to first event; the target is under five minutes.

---

## Stage 4 — Faults and behaviors

Goal: the full fault catalogue from SPEC.md with stateful behaviors, and enough test coverage that adding a fault is routine.

- [ ] **4.1 Fault interface and registry.** Refactor `internal/faults` so each fault is one file implementing a small interface, registered by name, with its own JSON schema for validation and its tier requirement (`connection` or `response`). The API rejects a response-tier fault on a rule whose only observed traffic is encrypted with a warning, not an error, since interception may come later.
  - Verify (Agent): existing tests still pass; a table test asserts every registered fault validates its own good and bad inputs.
- [ ] **4.2 Connection faults.** `refuse` (already exists, move it), `reset` (close the connection after N bytes or immediately), `hang` (never respond until the client disconnects or an optional cap), `throttle` (bytes per second on the response).
  - Verify (Manual): for each fault add a rule for `httpbin.org` and observe with curl: `refuse` fails to connect, `reset` shows "connection reset by peer", `hang` makes `curl --max-time 3` time out, `throttle` at 1024 bytes/s makes `/bytes/8192` take about eight seconds.
- [ ] **4.3 Response faults.** `status` (exists), `rate_limit` (429 with `Retry-After` seconds), `headers` (set and remove on the response), `truncate` (cut the body after N bytes or a percentage, keeping the original `Content-Length` so clients see a broken transfer), `corrupt` (flip bytes in the body), `slow_body` (deliver a correct response over a configured duration).
  - Verify (Manual): each fault on `httpbin.org/json` via curl: `rate_limit` shows 429 and the header; `headers` shows the added header; `truncate` makes curl report a transfer error; `corrupt` produces invalid JSON; `slow_body` makes the response arrive in visible chunks over the duration.
- [ ] **4.4 Behaviors.** Wrap any fault with an optional `behavior`: `percent` (apply to N% of matching requests), `first_n` (fail the first N matching requests then pass), `for_duration` (apply for N seconds after activation then pass), `pattern` (a string like `FFP` meaning fail, fail, pass, repeated). State is per rule and resets when a rule is enabled or edited.
  - Verify (Agent): unit tests for each behavior including reset on enable. Verify (Manual): a `first_n: 2` status rule with three curls gives 503, 503, 200. A `pattern: FP` gives alternating results.
- [ ] **4.5 Retry detection in events.** Mark events with a `retry_of` reference when the same client makes the same method and path to the same upstream within a short window after a faulted or failed response. Expose `GET /api/sessions/current/report` returning counts: total, faulted, retries, max wait between retries, abandoned (faulted with no retry within the window).
  - Verify (Agent): tests with synthetic event sequences. Verify (Manual): use the Go example, which retries on 5xx, under a `first_n: 2` status rule; the report shows two retries.

**Stage 4 gate (Manual).** Using the Spring Boot example, apply in turn: a 30 percent `status 503`, a `first_n: 3` `rate_limit`, a `hang`, and a `truncate`. For each, observe the app's behavior and check the event stream attributes the fault correctly. Confirm the report endpoint's numbers match what you counted by eye.

---

## Stage 5 — Configuration file, scenarios, and CLI

Goal: everything is declarable in `faultline.yaml`, hot-reloaded, and controllable from the command line.

- [ ] **5.1 Config schema and loading.** `faultline.yaml` with `routes`, `bypass`, `rules`, and `scenarios` (a scenario is a name plus a list of rule ids or inline rules). Strict parsing: unknown keys are errors with line numbers. Publish a JSON Schema at `schema/faultline.schema.json` and add the `# yaml-language-server` header hint in examples so editors validate.
  - Verify (Agent): tests for valid files, unknown keys, bad fault parameters, and duplicate ids, each producing a message with file and line.
- [ ] **5.2 Hot reload.** Watch the file; on change, validate and apply atomically. On invalid content, keep the old state and log the error prominently. The API's write operations persist back to the file when one is in use, preserving comments where feasible.
  - Verify (Manual): start with a config, edit a rule's delay in the file, save, curl and see the new delay without restart. Introduce a typo, save, see the error in the log and the old rule still active.
- [ ] **5.3 Scenarios.** `POST /api/scenarios/{name}/activate|deactivate`, `GET /api/scenarios`. Activating enables the scenario's rules and resets their behavior state; deactivating disables them. Only one scenario active at a time in this release; activating another deactivates the current one.
  - Verify (Manual): define two scenarios, activate one, check `GET /api/rules` shows the right enabled flags, activate the other, check they flipped.
- [ ] **5.4 CLI commands.** `faultline rule list|add|rm|enable|disable`, `faultline scenario list|on|off`, `faultline events tail|export`, `faultline upstreams`. They talk to a running instance over the API, printing tables for humans and `--json` for machines.
  - Verify (Manual): drive the whole Stage 1 gate again using only CLI commands.
- [ ] **5.5 Scenario runs with report.** `faultline run --scenario <name> -- <cmd>` activates the scenario, runs the command, deactivates, and prints the session report. `--report json` writes it to a file. Non-zero exit if the child fails.
  - Verify (Manual): `faultline run --scenario orders-flaky -- go test ./examples/go-client/...`. Expect the test output followed by a report table with faulted counts and retries.
- [ ] **5.6 Init command.** `faultline init` writes a commented starter `faultline.yaml` with one example scenario.
  - Verify (Manual): run it in an empty directory, then `faultline serve`; expect no validation errors.

**Stage 5 gate (Manual).** From an empty directory: `faultline init`, edit the file to add a scenario that delays and fails httpbin, `faultline run --scenario <it> -- go run ./examples/go-client`, read the report. Then, while it runs, edit the file to change the delay and observe the change live.

---

## Stage 6 — Web UI

Goal: everything above is visible and controllable in the browser, embedded in the binary.

- [ ] **6.1 Port the existing UI.** Move the previous `resilience-proxy/web` Next.js app into `web/`, upgrade dependencies, keep static export, embed the output into the binary served at `/`. Replace the old WebSocket rule-push protocol with REST calls for mutations and the read-only event stream for updates.
  - Verify (Manual): `make build` embeds the UI; open `localhost:9000`; the page loads with an empty request list and no console errors.
- [ ] **6.2 Live request stream and inspector.** Show events live with method, host, path, status, duration, tier badge, and a fault badge linking to the rule. Filters for host, method, status class, and faulted only. Clicking an event opens headers and body for request and response when intercepted, or a clear "encrypted" notice otherwise.
  - Verify (Manual): run the Go example wrapped; watch events stream; filter to faulted only; open one and read its body.
- [ ] **6.3 Upstreams panel.** List observed upstreams with tier, counts, error rate, and a per-host hint when the CA is not trusted. Quick actions: add delay, add 503, bypass.
  - Verify (Manual): after running the examples, see each upstream listed with the correct tier; click "add delay" and confirm the next requests slow down.
- [ ] **6.4 Rule editor.** Create and edit rules with fields driven by each fault's schema, so a new fault appears in the UI without UI changes. Enable and disable toggles. Behavior section with the four behavior types.
  - Verify (Manual): create a `first_n: 2` status rule from the UI; curl three times; see 503, 503, 200 in the stream.
- [ ] **6.5 Scenarios panel and report.** List scenarios, activate and deactivate, show the current session report live, and a "reset session" button.
  - Verify (Manual): activate a scenario, generate traffic, watch the counts update, reset, watch them clear.
- [ ] **6.6 Config file awareness.** When a config file is in use, the UI shows its path and a "changes are saved to faultline.yaml" notice; when not, it shows "in-memory, use init to persist."
  - Verify (Manual): start once with and once without a config file and confirm the notice.

**Stage 6 gate (Manual).** Repeat the five-minute experience from SPEC.md using only the browser after the `faultline run` command: see traffic, add delay, change to error, save as scenario, run the tests under it, read the report.

---

## Stage 7 — Agent interface

Goal: a coding agent can complete an inject, test, report cycle with no human involvement.

- [ ] **7.1 MCP server.** Using the official Go MCP SDK, expose tools over stdio (`faultline mcp`) and streamable HTTP at `/mcp` on the admin port. Tools: `list_upstreams`, `list_rules`, `add_rule`, `remove_rule`, `set_rule_enabled`, `list_scenarios`, `activate_scenario`, `deactivate_scenario`, `get_events` (with filters and limit), `wait_for_event` (filter plus timeout), `get_report`, `reset_session`. Each tool has a precise description and typed input schema derived from the fault registry.
  - Verify (Agent): tests with the SDK's in-memory transport calling every tool. Verify (Manual): add the server to Claude Code with `claude mcp add faultline -- faultline mcp` and ask it to list upstreams and add a delay rule; confirm the rule appears in the UI.
- [ ] **7.2 Standalone mode for agents.** `faultline mcp` without a running instance starts one in the background on default ports and stops it on exit, so an agent needs no separate step. Add a `start_wrapped` tool that runs a command under the proxy environment and returns its exit code and output tail.
  - Verify (Manual): with no Faultline running, ask the agent to run the Go example under a 503 rule and report retries. Expect a correct answer and a clean shutdown when the agent session ends.
- [ ] **7.3 Claude Code skill.** `skills/faultline/SKILL.md` teaching the agent when to reach for Faultline and three recipes: verify retry behavior, verify timeout configuration, and rehearse a dependency outage against a test suite. Include the expected shape of a good report to the user.
  - Verify (Manual): install the skill, say "check that examples/go-client handles httpbin being down," and expect the agent to run the recipe and produce a report without further prompting.
- [ ] **7.4 Thin client libraries.** Minimal Go and TypeScript clients (`clients/go`, `clients/ts`) for use inside tests: add rule, activate scenario, wait for event, get report. No business logic, just typed API calls.
  - Verify (Agent): each client has a test against a live Faultline started in the test. Verify (Manual): the Go example gains one integration test that uses the client to inject a `first_n` fault and asserts two retries occurred.

**Stage 7 gate (Manual).** In a fresh Claude Code session with the MCP server and skill installed, ask: "Verify the Spring Boot example retries on 503 from httpbin and tell me how long the user would have waited." Expect the agent to start Faultline, run the app, inject the fault, exercise it, and answer with numbers matching the UI report.

---

## Stage 8 — Containers

Goal: the container companion attach mode for Docker Compose, matching how Aevon runs services.

- [ ] **8.1 Docker image.** Multi-stage build producing a small image with the binary, listening on all interfaces, with the CA directory as a volume. Published to GHCR by CI on tags.
  - Verify (Manual): `docker run -p 9000:9000 -p 9001:9001 faultline` then `curl -x localhost:9001 http://httpbin.org/get` and see the event.
- [ ] **8.2 Compose companion example.** `examples/compose/` with Faultline plus the Spring Boot example, where the app service gets the proxy env vars and the CA mounted, and the JVM trust store built at startup by a tiny entrypoint helper shipped in the image (`faultline trust java`).
  - Verify (Manual): `docker compose up`, curl the app, see intercepted events in the UI at `localhost:9000`.
- [ ] **8.3 Transparent mode.** Faultline gains `--transparent`: with `NET_ADMIN`, it installs iptables rules in its own network namespace redirecting outbound 80 and 443 to itself and uses the original destination for routing. App services attach with `network_mode: "service:faultline"` and need no env vars for connection-tier faults. Response-tier faults still need the CA trusted in the app image.
  - Verify (Manual): second compose example using transparent mode with the Go example; no env vars on the app; events show tier `encrypted` until the CA is added, then `intercepted`.
- [ ] **8.4 Compose helper.** `faultline compose inject --service <name>` reads `docker-compose.yml` and writes `docker-compose.faultline.yml` override adding the companion and the settings for the named services, printing the `docker compose -f ... -f ...` command to run.
  - Verify (Manual): run it against `examples/compose` with the companion removed; the generated override reproduces the working setup.

**Stage 8 gate (Manual).** Take a real Aevon Compose file on a VM, run the helper against one Spring Boot service, bring it up, and rehearse a dependency outage from the UI. Expect intercepted events and a correct report, with no edits to the application image beyond trusting the CA.

---

## Stage 9 — Release

Goal: a stranger can install and use it.

- [ ] **9.1 Release pipeline.** GoReleaser builds Linux, macOS, Windows binaries for amd64 and arm64 on tags, publishes GitHub releases and the Docker image, and updates a Homebrew tap.
  - Verify (Manual): tag `v0.1.0-rc1`, watch the workflow, `brew install josipmusa/tap/faultline` on a Mac, run `faultline version`.
- [ ] **9.2 GitHub Action.** `josipmusa/setup-faultline` action installing the binary and optionally starting it, with an example workflow running the Go example's integration test.
  - Verify (Manual): the example workflow passes on a pull request in the repository.
- [ ] **9.3 Documentation.** README with the five-minute walkthrough and a GIF, `docs/` covering attach modes, trust setup per runtime, fault catalogue with examples, config reference generated from the schema, MCP tools reference, and troubleshooting.
  - Verify (Manual): hand the README to someone who has never seen the project and time them to first event.
- [ ] **9.4 Security review.** Confirm the admin API binds to localhost by default with an `--admin-bind` flag to widen it, the CA private key is created with owner-only permissions, event bodies can be disabled with `--no-bodies` for sensitive traffic, and the MCP HTTP endpoint respects the same bind rule. Document the threat model in `docs/security.md`.
  - Verify (Agent): tests for default bind and file permissions. Verify (Manual): read `docs/security.md` and confirm every claim matches behavior.
- [ ] **9.5 v0.1.0.** Tag, announce, open issues for the "later" list below.
  - Verify (Manual): release page shows all artifacts; a fresh install works on Linux and macOS.

**Stage 9 gate (Manual).** The full five-minute experience from SPEC.md on a machine that has never seen the source code, using only the README.

---

## Later, deliberately out of v0.1

- Shared instance identity via proxy credentials in `HTTPS_PROXY=http://service:token@faultline:9001`, with rules scoped per service and an admin token for the control surface.
- Prometheus metrics endpoint.
- Python client library.
- Kubernetes sidecar injection.
- gRPC, WebSocket, and raw TCP.
- Response recording for later replay (only if users ask; this is adjacent to mocking and out of scope by principle).
