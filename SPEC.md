# Faultline — Product Specification

Faultline shows developers what their application says to the outside world, and lets them make that world misbehave on purpose.

It sits between an application and the services it depends on. It shows every outbound call live, and it can slow those calls down, fail them, cut them off, or make them flaky, so the developer can see how the application copes before a real outage does it for them.

This document describes what Faultline does for its users. It does not describe how it is built. See ROADMAP.md for the build plan and AGENTS.md for engineering conventions.

## The problem

Every service depends on other services: payment providers, identity providers, internal microservices, third-party APIs, databases behind HTTP. Those dependencies fail in the real world. They get slow, they time out, they return errors, they drop connections, they rate limit.

Most teams write retry logic, timeouts, circuit breakers, and fallbacks, and then never see them run. The first time the code path executes is during an incident. Testing it today requires either editing production code to fake failures, standing up heavyweight chaos platforms, or writing mocks that replace the dependency entirely and therefore never exercise the real client, the real serialization, or the real TLS handshake.

There is also a simpler, everyday problem: developers cannot easily see what their own service calls. When a request is slow, nobody knows which of the six downstream calls took the time.

## Who it is for

**The application developer** working locally. Wants to see outbound calls while developing and occasionally ask "what happens if this is slow." Wants zero setup and no changes to the application.

**The developer writing integration tests.** Wants to assert that retry, timeout, and fallback logic actually works, inside an automated test that runs in CI, against the real HTTP client.

**The team on a shared staging environment.** Wants a fault injector deployed once that any service can opt into by redeploying with a setting, to rehearse outages before they happen.

**The AI coding agent.** Wants to verify its own work: inject a failure, run the tests, read what the application did, and report. Faultline is a tool an agent can drive end to end without a human clicking anything.

## Principles

1. **Works in five minutes with no code changes.** Install, run one command that wraps the application's own start command, open the browser, see traffic.
2. **Real traffic, not mocks.** Faultline degrades real calls to real dependencies. It never fabricates a dependency that is not there.
3. **One binary, no dependencies.** No database, no separate UI process, no runtime to install. Configuration lives in a file that can be committed to the repository.
4. **Everything has four faces.** Every capability is available from the web UI, the command line, the HTTP API, and the agent interface. They all do the same thing to the same state.
5. **Degrade gracefully.** When Faultline cannot see inside encrypted traffic, it still does what it can and tells the user exactly what it cannot do and why.
6. **Honest reporting.** After a test run, Faultline tells the user what the application actually did: how many times it retried, how long it waited, whether it gave up.

## Core concepts

These are the words users see in the UI, the CLI, the config file, and the agent tools.

- **Upstream.** A dependency the application talks to, identified by hostname. `api.stripe.com`, `orders-service`, `login.microsoftonline.com`.
- **Rule.** A condition plus a fault. The condition says which traffic is affected: an upstream, optionally a method, a path pattern, or a header. The fault says what happens to it.
- **Fault.** One thing that goes wrong. Faults have three tiers depending on how much of the traffic Faultline can see:
  - **Connection faults** work on any traffic, even encrypted: added delay, jitter, connection refused, connection dropped mid-transfer, hang until the client gives up, bandwidth throttle.
  - **Response faults** need Faultline to see inside the request: forced status codes, rate-limit responses with a retry hint, header changes, truncated or corrupted bodies, slow streaming of an otherwise fine response.
  - **Behaviors** make faults stateful over time: apply to a percentage of requests, fail the first N requests then recover, fail for a duration then recover, alternate in a pattern.
- **Scenario.** A named group of rules that describes a situation: `payment-provider-down`, `identity-slow`, `orders-flaky`. Scenarios are turned on and off as a unit and can be committed to the repository.
- **Session.** One observed run. Everything Faultline saw while a scenario was active, and a report of how the application behaved.
- **Attach mode.** How traffic reaches Faultline. Users pick one; the rest of the product is identical regardless.

## Attach modes

The product offers four, in the order users should meet them.

**Wrap.** `faultline run -- <your usual start command>`. Faultline starts the application as a child and arranges for its outbound HTTP and HTTPS calls to pass through. Nothing in the application changes. Works on a laptop and in CI. This is the front door.

**Container companion.** For applications running in Docker Compose. Faultline runs as one more service. Applications opt in with a couple of settings, or, in transparent mode, by sharing the network of the Faultline container so all their traffic passes through without any settings at all. A helper generates the Compose changes so nobody edits YAML by hand.

**Shared instance.** For teams. Faultline runs once in a staging environment. A service opts in by being deployed with one setting that names Faultline as its proxy and identifies the service. Rules can then target a specific service. This mode arrives after the core is stable.

**Explicit route.** For clients that ignore proxy settings, and for frontends. The user gives Faultline an upstream and gets back a local address. Pointing the application's configuration at that address sends traffic through. This is the fallback, not the default.

## Encrypted traffic

Most real traffic is HTTPS. Faultline handles this in two tiers so that it is always useful and never mysterious.

Without any trust setup, Faultline can see which host the application is talking to but not what it says. Connection faults work fully. Response faults do not, and the UI marks such upstreams as "encrypted, connection faults only."

To unlock response faults, Faultline creates a local certificate authority on first use and, in wrap mode, teaches the child process to trust it automatically for common runtimes. In other modes the user does this once per environment following a short guide. Nothing is ever installed into the operating system trust store without an explicit command.

Applications that pin certificates cannot be inspected. Faultline detects this and says so instead of producing confusing errors.

## User stories

### Seeing traffic

- As a developer, I run my app through Faultline and see every outbound request appear live, with method, host, path, status, and duration, so I know what my service actually calls.
- As a developer, I click a request and see its headers and body, and the response headers and body, so I can debug integrations without adding logging.
- As a developer, I see at a glance which upstreams are encrypted and which faults are available for them, so I am never surprised by a fault that does nothing.
- As a developer, I filter the live stream by upstream, method, status, or "faulted only," so I can focus on the calls I care about.
- As a developer, I export what Faultline saw as a file, so I can attach it to a bug report or feed it to another tool.

### Breaking things on purpose

- As a developer, I click an upstream and add two seconds of delay, and see my UI's loading states and timeouts actually trigger.
- As a developer, I make one specific endpoint return 503 while everything else works, so I can test partial failure rather than total outage.
- As a developer, I make an endpoint fail twice and then succeed, so I can watch my retry logic work and see how long the user waited.
- As a developer, I make ten percent of requests to an upstream fail, so I can see how my service behaves under a flaky dependency over many requests.
- As a developer, I make an upstream hang without responding, so I can confirm my client timeout is set and is what I think it is.
- As a developer, I make an upstream return 429 with a retry-after hint, so I can confirm my client honors it.
- As a developer, I make an upstream drop the connection halfway through the response body, so I can confirm my code handles truncated data.
- As a developer, I toggle rules on and off without deleting them, so I can compare behavior with and without a fault in seconds.

### Scenarios and repeatability

- As a developer, I save a set of rules as a named scenario, so I can reproduce a situation later with one click or one command.
- As a developer, I commit scenarios to my repository so my teammates get the same set of outage rehearsals.
- As a developer, I run my test suite under a scenario with one command, and Faultline turns the scenario on, runs the tests, turns it off, and prints a report.
- As a developer, I get a report after a session that tells me how many requests were faulted, how many times my application retried, how long the longest wait was, and whether any request was abandoned.

### Automated tests and CI

- As a developer writing an integration test, I add a rule from inside the test, exercise my code, and assert on what Faultline observed, so my retry logic has a real test.
- As a developer, I use Faultline in a CI pipeline with a single setup step, and my resilience tests run on every pull request.
- As a developer, I use the same scenarios locally and in CI, so what I rehearsed by hand is what the pipeline checks forever after.

### Working with AI agents

- As a developer using a coding agent, I ask the agent to verify that a service handles a dependency outage, and the agent starts Faultline, injects the failure, runs the tests, reads the report, and tells me what happened.
- As an agent, I can list upstreams that have been observed, add and remove rules, activate scenarios, read recent requests, and wait for a request matching a filter to arrive.
- As a developer, I install a skill that teaches my agent the common recipes, so I can say "test the retry behavior against the orders service" and get a meaningful result.

### Teams and shared environments

- As a platform engineer, I deploy Faultline once in staging, and any team can route their service through it by changing one deployment setting.
- As a platform engineer, I scope rules to the service that made the request, so one team's outage rehearsal does not break another team's test run.
- As a platform engineer, I protect the control surface so that only authorized people and pipelines can add rules.

### Frontend applications

- As a frontend developer, I point my development server's API proxy at a Faultline route and watch how my UI handles slow and failing APIs, including loading states, error boundaries, and retry banners.

## What Faultline does not do

- It does not mock. If the upstream is unreachable, Faultline reports that, it does not pretend to be the upstream.
- It does not handle gRPC, WebSocket, or raw TCP protocols in the first release. HTTP and HTTPS only.
- It does not run experiments on production traffic. It is a development and testing tool.
- It does not require or provide a database, an account, or a hosted service.
- It does not inject itself into Kubernetes clusters in the first release.

## The five-minute experience

This is the experience the first release must deliver, end to end, on a fresh machine.

1. Install with one package manager command.
2. Run `faultline run -- <the command you already use to start your app>`.
3. Open the address Faultline prints. See your application's outbound calls appearing as you use it.
4. Click an upstream. Add a delay. Watch your application slow down.
5. Change the delay to a forced error. Watch your application handle it, or not.
6. Save what you did as a scenario. Run your tests under it with one command. Read the report.

If any step needs the user to read documentation, that step is not finished.

## Success looks like

- A developer with no prior knowledge reaches step 3 above in under five minutes.
- A resilience test written with Faultline is shorter than the same test written with a hand-rolled mock.
- A coding agent can complete a full "inject, test, report" cycle with no human in the loop.
- Teams commit scenario files to their repositories the way they commit CI configuration.
