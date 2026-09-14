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

The instance it starts reads `faultline.yaml` from the working directory, or
the file `--config` names, and creates the interception CA if there is none,
exactly as `faultline run` does. Its admin port is the one in `--admin`, and
`--proxy-port` moves its forward proxy off 9001. It also holds those ports for as long as the agent is connected, so a
`faultline run` in another shell finds them taken and says so. Either join the
agent's instance from that shell with the `rule`, `scenario` and `session`
commands, or give the run ports of its own with `--admin-port` and
`--proxy-port`.

The same tools are served over HTTP at `/mcp` on the admin port, for an agent
that would rather connect to a Faultline that is already up:

```
claude mcp add --transport http faultline http://localhost:9000/mcp
```

## The tools

Thirteen tools, in the order a check uses them: see what the application
calls, break one thing, run the application, read what it did, start over.
Every input is optional unless the table says otherwise.

### Seeing what the application calls

`list_upstreams` is where to start. It reports what the application really
calls, which is often not what its configuration suggests, and the tier of
each host, which decides whether a response fault can apply.

| tool | input | output |
| --- | --- | --- |
| `list_upstreams` | none | `upstreams`: each host with `tier`, `requests`, `faulted`, `errors`, `last_seen` and a `hint` when something is in the way |
| `get_events` | `host` to keep one upstream's calls; `faulted` true for the calls a rule broke, false for the untouched ones; `limit` for the most recent N | `events`, oldest first, each with host, method, path, status, duration, bytes, tier, `faulted` and the `rule_id` that acted |
| `wait_for_event` | `host`, `faulted`, `timeout_ms` (10000 by default, 300000 at most) | `matched`, the `event` when one came, and `waited_ms` |
| `get_report` | none | `total`, `faulted`, `retries`, `max_retry_wait_ms`, `abandoned`, and `warnings` for every rule in force that cannot fire |

Only calls recorded after `wait_for_event` begins count, so call it before
exercising the application, not after.

### Breaking things

| tool | input | output |
| --- | --- | --- |
| `add_rule` | `name` (required), `match`, `fault` (required), `behavior`, `id`, `enabled` | the rule as stored, with its `id`, and `warnings` when it cannot apply yet |
| `list_rules` | none | `rules`, in the order Faultline applies them |
| `remove_rule` | `id` (required) | `removed`: the id that is gone |
| `set_rule_enabled` | `id`, `enabled` (both required) | the rule as stored; either way its behavior state starts over |
| `list_scenarios` | none | `scenarios`, each with `name`, `rules` and `active` |
| `activate_scenario` | `name` (required) | the scenario; its rules are enabled and their behavior starts over, and whichever scenario was active goes off |
| `deactivate_scenario` | `name` (required) | the scenario; its rules are disabled |

`match` takes `host`, `method`, `path` and `header`, and everything left out
matches everything. `path` is a glob: `/v1/charges/*` for one segment, `/v1/**`
for any depth.

### Running the application

`start_wrapped` is `faultline run` as a tool: it runs a command with its
outbound calls going through Faultline and reports the exit code, how long it
took, and the end of each output stream.

| tool | input | output |
| --- | --- | --- |
| `start_wrapped` | `command` (required, a list), `dir`, `timeout_ms` (60000 by default, 300000 at most) | `exit_code`, `timed_out`, `duration_ms`, `stdout_tail`, `stderr_tail`, `stdout_truncated`, `stderr_truncated` |

```json
{ "command": ["go", "test", "./..."], "dir": "examples/go-client", "timeout_ms": 120000 }
```

The command is a list and is run directly, so there is no shell: wrap it in
`sh -c` yourself if you need a pipe or a redirection. It runs to completion or
until its timeout, 60 seconds by default and 300 at most, which suits a test
suite or a script rather than a development server that never exits. A timeout
is an answer rather than a failure: `timed_out` comes back true with the output
so far, and the command and everything it started are stopped.

Only the end of each stream comes back - the last hundred lines or eight
kilobytes, whichever is smaller - with `stdout_truncated` and
`stderr_truncated` saying when there was more.

### Starting over

| tool | input | output |
| --- | --- | --- |
| `reset_session` | none | `reset`: true |

`reset_session` clears the recorded calls and re-arms every rule, so a spent
`first_n` applies again. It leaves the rules and the active scenario alone, so
whatever you or the user set up survives. Call it between two runs of the same
check, or the second run measures the first one's leftovers. `faultline session
reset` is the same thing from a shell, and `faultline session report` reads the
report an agent gets from `get_report`.

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
