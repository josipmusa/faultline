---
name: resilience-check
description: Break one of an application's dependencies on purpose with Faultline and read what the application really did about it, to answer any question about behavior under failure. Use it when asked whether something survives a dependency being down, whether it actually retries, whether a configured timeout is the one really in force, whether a test suite would catch an outage, or what a user would see when a service degrades. Trigger on casual phrasings too ("check that this handles httpbin being down", "does checkout survive Stripe going down", "do we actually retry", "is the 3s timeout real", "what happens when search is slow", "rehearse an outage against the tests"). Use it EVEN IF the faultline MCP tools are already attached and their instructions describe a loop - that loop is a summary, this is the method it summarises, and it is what stops a faulted count of zero being reported as resilience when the fault never applied.
---

# Resilience check

Faultline is a proxy that sits between an application and the services it
depends on. It shows every outbound call and can degrade those calls on
purpose. It never invents a response for an upstream it could not reach, so
everything it reports describes traffic that really happened.

## When to reach for this

The question is about behavior under failure rather than about code. "Does
checkout survive the payment API being down." "Do we actually retry." "Is the
three second timeout the one in force." "What does a user see when search is
slow."

Reading the code tells you what it intends. This tells you what it does. Reach
for it whenever the answer would otherwise be an inference from a configuration
value or a `catch` block, and especially when someone is about to trust that
inference.

Do not reach for it when the dependency is not called over HTTP, when the
question is answerable by reading one function, or when you can cause the
failure more directly than by proxying it.

## The loop

Five steps: attach, see, inject, exercise, read. Faultline lives as long as
the command you wrap, and that single fact decides how the loop is shaped.

**Wrapping something long lived** is the shape to prefer. A dev server, a
service, a client that loops:

    faultline run -- <the command that usually starts it>

Leave that running. Faultline stays up with it, so every other command below
joins the same instance from another shell and you can look, change one thing,
and look again without restarting anything.

**Wrapping a one-shot command**, typically a test suite, gives you a run that
is self-contained. Nothing can be inspected afterwards, because the instance
goes away with the child, so the rule has to be in force before it starts and
the report it prints at the end is the evidence. Rules for a run like this come
from the configuration file, which `faultline init` writes a commented starter
for, and a named group of them is a scenario:

    faultline run --scenario <name> -- <the test command>

If the MCP tools are available, prefer `start_wrapped` and skip all of this:
the instance outlives the command, so a rule added with `add_rule` is in force
for it and the report can still be read afterwards.

The exit code is the child's own, so a failing suite still fails.

### 1. See what the application actually calls

    faultline upstreams

Start here every time, and do it before injecting anything. The hosts an
application really talks to are routinely not the ones its configuration
suggests, and this is also where you learn how much Faultline can see of each
one. Read the tier column now; step 5 explains why it decides whether your
result means anything.

### 2. Inject one narrow fault

    faultline rule add --host <the host> --fault <fault> --set <name>=<value>

Name the fault you want in plain terms. You do not need to know the catalogue,
because Faultline refuses anything it does not have and the refusal lists what
it has instead, then names the parameters the fault you chose takes and the
field it rejected. Two or three refusals will teach you the whole catalogue,
and that is the intended way to learn it.

Give the fault a shape when you want it to stop, rather than breaking every
call forever: a behavior can fail only the first few matching calls, or a
percentage of them, or follow a repeating pattern. Same discovery rule.

Add one rule at a time and change one thing between measurements. A check with
two new faults in it cannot tell you which one the application reacted to.

### 3. Exercise the dependency

Make the application do the thing. Drive it the way a user or a test would:
call its endpoint, run its suite, let its loop come round. Nothing is measured
until real traffic goes out, and an application that was already running is
already sending it.

### 4. Read what happened

    faultline session report

That is the session's counts: calls, how many a rule broke, how many looked
like retries, the longest gap between an attempt and its repeat, and how many
failures nothing ever came back from. For the individual calls:

    faultline events export -o events.ndjson

### 5. Reset between measurements

    faultline session reset

This clears what was observed and re-arms every rule, so a fault set to fire a
fixed number of times fires again. It leaves the rules and the active scenario
alone. Run it between two runs of the same check, or the second measurement
reports the first one's leftovers on top of its own.

### The same loop with the MCP tools attached

If the Faultline MCP server is available, prefer these. They are the same
capabilities against the same state, and a rule added either way is the same
rule.

| Step | Tool |
| --- | --- |
| See | `list_upstreams` |
| Inject | `add_rule`, and `list_rules`, `remove_rule`, `set_rule_enabled` |
| Exercise | `start_wrapped`, or `wait_for_event` to block until a call arrives |
| Read | `get_report`, `get_events` |
| Reset | `reset_session` |

`add_rule` carries the whole catalogue in its schema and description, so there
is nothing to discover by refusal. `start_wrapped` runs a command under the
proxy and returns its exit code and the tail of its output, which makes the
one-shot shape above unnecessary: the instance outlives the command, so you can
still read the report and the events afterwards.

## Two traps

Both of these turn a check into a false reassurance, which is worse than no
check at all.

### A fault that never applied looks exactly like an application that coped

A fault that rewrites a response cannot be applied to a host Faultline can only
see as encrypted bytes. The rule is still accepted, because it will start
working the moment the client trusts Faultline's certificate authority, but
until then it does nothing. What you get is a report saying zero calls were
faulted, which is indistinguishable from a report about an application that
shrugged off the outage.

So: **read the report's warnings before its counts.** Faultline names every
rule in force that cannot currently fire, on the report and when the rule is
added. When you see one, the honest finding is that the check did not run, and
the next step is trusting the certificate authority, not writing up a result.
The tier column from step 1 tells you this before you have spent a run on it.

To fix it rather than report around it, `faultline ca install` trusts the
authority, asking before it touches anything the operating system trusts, and
`faultline ca path` prints the certificate for a runtime you would rather
configure directly. Faults that act on the connection need none of this, so if
trusting the authority is not yours to decide, switch to one of those and say
which question you answered.

Never report that an application handled a failure off a run where nothing was
broken. If the counts say nothing was faulted, either find out why or say you
could not establish it.

### An empty match breaks everything

A rule with no host matches every call the application makes, to every
dependency. That is almost never the check you meant, and it will produce a
result you cannot attribute to anything. Always give the match a host, and
narrow it further with a method or a path when the application calls the same
host for more than one reason.

## Recipes

Three checks worth knowing. Each is the loop above with a particular fault and
a particular number to read.

### Does it retry

Break the dependency for the first few calls only, so it recovers on its own,
then read the report. `faulted` says how many calls were broken, `retries` how
many were attempted again, and `abandoned` how many failures the application
never followed up on. An application that retries produces retries and zero
abandoned, and succeeds once the fault stops firing. One that does not produces
an abandoned failure and, usually, a visible error where the user would see it.

Read the retry numbers carefully, because the heuristic is honest about what it
measures and no more: a retry is a second call to the same method and path
within a few seconds of one that failed, whoever made it. A client that polls
on a fixed interval scores exactly like one that retries, and the longest gap
between attempts is then the poll interval rather than a backoff. So check the
application's own logs or its code for a backoff before you describe that
number as one. If the interval between attempts is suspiciously constant, it
probably is not a backoff.

### Is the configured timeout the one in force

Bracket it. Do not guess it and do not take a single reading.

Read the timeout out of the application's own configuration first. Then run the
check twice, resetting the session between them, holding the response for a
little less than that value and then for a little more:

- Under the timeout, the call should survive. The application waits and gets
  its answer.
- Over it, the application should give up, and give up at about the configured
  time rather than well after it.

One over-timeout run proves only that something, somewhere, eventually times
out. It could be a default several layers down, a timeout in a library, or an
infrastructure limit, and any of those will look like success. The bracket is
what proves the value in the configuration is the value actually in force,
which is the bug worth catching: a timeout that was set and never took effect.

Check where the application gave up, too. A retrying client with a per-attempt
timeout can hold a user far longer than the timeout suggests, and that total is
the number a person actually cares about.

### Rehearse an outage against the test suite

Take the dependency off the network entirely, for the length of a suite. Use a
fault that acts on the connection rather than on the response, since that is
what an unreachable service does and it works whatever Faultline can see of the
host. Declare it as a scenario, then run the suite with the scenario named, and
let the suite's own pass or fail be the result.

Two things make this trustworthy rather than merely green. The report has to
show the fault actually fired, or the suite passed because it never met the
outage, most often because the tests do not touch that dependency at all. And a
suite that passes should be read carefully: if the application is supposed to
degrade rather than fail, confirm the tests would have noticed the difference,
because a suite that never asserts on the degraded path passes either way.

## Reporting what you found

Lead with what the application did, with the numbers behind it, then say what
was broken to find out. Name the rule and the host, so the reader can tell
which dependency was under test and reproduce it. Give the number a user would
feel, in the units a user feels it in.

> `examples/go-client` kept going when httpbin.org started answering 503. The
> first two calls were faulted, it tried again on both, and the third
> succeeded, with nothing abandoned. The longest gap between attempts was about
> a second, which is its poll interval rather than a backoff, so a user would
> have waited roughly a second longer than usual.

What makes that report usable is that every number in it came from real
traffic, and that it says which of them is a measurement and which is an
interpretation. Keep that distinction when you write yours.

Say plainly when a check did not establish what it was supposed to: a fault
that could not apply, a suite that never called the dependency, a timeout you
could only bracket on one side. An inconclusive check reported as inconclusive
is a useful result. Reported as a pass, it is the thing that gets trusted right
up until the real outage.
