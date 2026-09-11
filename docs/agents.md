# The agent interface

Faultline speaks the Model Context Protocol, so a coding agent can inject a
fault, exercise the application, and read what happened without anyone clicking
anything.

The tools are the same capabilities the UI, the CLI and the HTTP API have. They
go through the HTTP API, so an agent cannot reach a state the other three cannot
describe, and a rule an agent adds appears in the UI immediately.

## Adding it to an agent

Over stdio:

```
claude mcp add faultline -- faultline mcp
```

`faultline mcp` looks for an instance at `--admin`, which defaults to
`http://localhost:9000`. If one is there, the agent joins it, so its rules and
the ones in your UI are the same rules. If none is, it starts one in the same
process and stops it again when the agent disconnects, so an agent needs no
setup step of its own. While the session lasts the UI is on the admin port as
usual, and you can watch what the agent is doing.

The instance it starts reads `faultline.yaml` from the working directory and
creates the interception CA if there is none, exactly as `faultline run` does.

The same tools are served over HTTP at `/mcp` on the admin port, for an agent
that would rather connect to a Faultline that is already up:

```
claude mcp add --transport http faultline http://localhost:9000/mcp
```

## The tools

**Seeing what the application calls**

| tool | what it answers |
| --- | --- |
| `list_upstreams` | which hosts the application actually calls, with request, fault and error counts, and the tier |
| `get_events` | the recorded calls, oldest first, filterable by host and by whether a rule broke them |
| `wait_for_event` | the application's next matching call, or that none came before the timeout |
| `get_report` | totals for the session: requests, faulted, retries, the longest retry wait, abandoned attempts |

`list_upstreams` is where to start. It reports what the application really
calls, which is often not what its configuration suggests.

**Breaking things**

| tool | what it does |
| --- | --- |
| `add_rule` | add a rule and put it in force |
| `list_rules` | the rules Faultline holds, in the order it applies them |
| `remove_rule` | delete a rule |
| `set_rule_enabled` | turn a rule on or off, which also re-arms its behavior |
| `list_scenarios` | the scenarios the configuration file declares, and which is active |
| `activate_scenario` | put a whole situation in force |
| `deactivate_scenario` | take it out of force |

**Running the application**

`start_wrapped` is `faultline run` as a tool: it runs a command with its
outbound calls going through Faultline and reports the exit code, how long it
took, and the end of each output stream.

```json
{ "command": ["go", "test", "./..."], "dir": "examples/go-client", "timeout_ms": 120000 }
```

The command is a list and is run directly, so there is no shell: wrap it in
`sh -c` yourself if you need a pipe or a redirection. It runs to completion or
until its timeout, 60 seconds by default and 300 at most, which suits a test
suite or a script rather than a development server that never exits. A timeout
is an answer rather than a failure: `timed_out` comes back true with the output
so far, and nothing is left running.

Only the end of each stream comes back - the last hundred lines or eight
kilobytes, whichever is smaller - with `stdout_truncated` and
`stderr_truncated` saying when there was more.

**Starting over**

`reset_session` clears the recorded calls and re-arms every rule, so a spent
`first_n` applies again. It leaves the rules and the active scenario alone, so
whatever you or the user set up survives. Call it between two runs of the same
check, or the second run measures the first one's leftovers.

## What an agent needs to know about faults

`add_rule` takes the fault as a type and its parameters together, the same shape
the API and the configuration file use:

```json
{
  "name": "Stripe is slow",
  "match": { "host": "api.stripe.com", "method": "POST", "path": "/v1/charges/*" },
  "fault": { "type": "delay", "ms": 2000, "jitter_ms": 500 },
  "behavior": { "type": "first_n", "n": 2 }
}
```

The `add_rule` input schema holds the fault and behavior types on offer, so an
agent that asks for one Faultline does not have is told so, and told what there
is instead, before the call goes anywhere. Which parameters a fault takes
depends on its type, which a schema cannot say usefully, so those live in the
tool description: every fault, its tier, and each parameter with its bounds.
Both come from the fault registry, so neither is ever out of date with what the
binary can actually do.

Two things are worth knowing before an agent is surprised by them:

- **An empty match matches everything.** A rule with no host breaks every call
  the application makes. Narrow it.
- **A response-tier fault cannot apply to encrypted traffic.** If
  `list_upstreams` reports a host as `encrypted`, Faultline can only tunnel it,
  and a fault that rewrites a response does nothing until the client trusts the
  Faultline CA. See [trust.md](trust.md). The rule is still accepted, because it
  will start working the moment interception does. `add_rule` answers with a
  `warnings` field saying so, and `get_report` repeats it for every rule in
  force that cannot fire, so a quiet run explains itself: a `faulted` of zero
  beside a warning means the fault never applied, not that the application
  handled it.

## Shape of a good answer

When the user asks whether the application survives a dependency failing, the
answer they want has numbers in it. `get_report` has them:

> `examples/go-client` retried twice after the first 503 and succeeded on the
> third attempt. The longest it waited between attempts was 1002ms, so a user
> would have waited about a second longer than usual, and nothing was
> abandoned.

Faultline never invents a response for an unreachable upstream, so every number
above describes real traffic. A synthetic response always carries a
`Faultline-Fault` header naming the rule that produced it, which is how a
synthetic 503 is told apart from the upstream's own.
