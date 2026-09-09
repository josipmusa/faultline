# faultline.yaml

Everything Faultline can be told to do can be written down. The file is
`faultline.yaml` in the working directory, it is meant to be committed, and it
holds four things: the explicit routes, the hosts to leave alone, the rules, and
the scenarios.

A copy to start from is in [examples/faultline.yaml](../examples/faultline.yaml).

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/josipmusa/faultline/main/schema/faultline.schema.json

routes:
  - name: stripe
    upstream: https://api.stripe.com
    port: 9100

bypass:
  - "*.internal"

rules:
  - id: slow-stripe
    name: Stripe is slow
    match:
      host: api.stripe.com
      method: POST
      path: /v1/charges/*
    fault:
      type: delay
      ms: 2000
      jitter_ms: 500
    behavior:
      type: first_n
      n: 2

scenarios:
  - name: payments-down
    rules:
      - slow-stripe
      - id: stripe-503
        name: Stripe answers 503
        fault:
          type: status
          code: 503
```

## Where the file comes from

`faultline serve` and `faultline run` read `faultline.yaml` from the working
directory when it is there. There is no error when it is not: Faultline runs
with its rules in memory, exactly as it did before there was a file.

`--config <path>` names another file. That one has to exist; being told to use a
file that is not there is an error, because you meant that file.

```
faultline run --config ../ops/faultline.yaml -- npm run dev
```

The file and the flags are merged, and the flags win, because a flag is what you
typed just now:

- **routes.** The file's routes plus the `--route` ones. A `--route` with the
  same name as a route in the file replaces it. Ports are handed out over the
  whole set, so an automatic port never lands on one the file asked for.
- **bypass.** The union of the file's `bypass`, `--bypass`, and the entries
  Faultline always keeps. Nothing conflicts, so nothing has to win.
- **rules.** The file's, in force before anything is listening.

## While Faultline runs

The file is watched. Save a change and it is in force in about a second, with no
restart:

- **Rules are applied all at once.** There is no moment where half the file is
  in force.
- **A rule you did not touch is left alone.** Behavior state (`first_n`,
  `pattern`, `for_duration`) belongs to a rule as it is written, so editing one
  rule does not restart the others. A rule that did change starts over, which is
  what editing a rule has always meant.
- **A file that does not parse changes nothing.** The rules already in memory
  stay in force, and the problem is logged at error level with the file, the
  line, and the field. The next save that does parse recovers, still without a
  restart.
- **Routes and the bypass list are read once.** A route is a listener, and
  Faultline does not open and close listeners while it runs. Changing either in
  the file is reported as needing a restart rather than quietly ignored; the
  rules in that same save are applied as usual.

The file is polled rather than watched through the operating system, which is
what makes it work with editors that save by writing a temporary file and
renaming it over the original. A change is read once it has stopped moving, so
Faultline never reads the half written middle of a save.

## Changes made through the API

When a file is in use, every rule change through the API or the UI is written
back to it: create, update, delete, enable, disable. The file stays yours.

- Comments, routes, bypass entries, scenarios, and the order of the rules
  already written are left as they are. A rule nobody changed keeps the words it
  was written with, down to the quoting.
- A rule that changed is rewritten where it stands, keeping the comment above
  it. A rule Faultline has never seen is added at the end of `rules`, and one
  written inside a scenario is edited inside that scenario, not moved.
- `enabled` is written out for every rule Faultline writes, because leaving it
  out means different things in different places in the file.
- The file is replaced in one step, through a temporary file in the same
  directory, so a reader sees the old file or the new one and never half of
  either.
- A hand edit that the watch has not read yet is applied before the change is
  made, so an API change is made on top of what the file says rather than
  overwriting it.
- While the file does not parse, changes through the API are refused with `409`
  and the error names the line to fix. Writing into a file Faultline cannot read
  would either lose what is in it or lose the new rule at the next read.

## The first line

The `# yaml-language-server` comment points an editor at the published schema,
so a mistyped key or a fault parameter that does not exist is underlined while
you type rather than reported when you run. It works in VS Code with the YAML
extension and in any editor that speaks the language server protocol. The schema
is generated from the fault catalogue, so it always knows the same faults the
binary does.

## routes

An explicit route is for a client that ignores proxy settings: Faultline listens
on a local port and forwards to the upstream. Each route has a `name` and an
`upstream`, which is an absolute `http` or `https` URL. `port` is optional; left
out, Faultline assigns one from 9100 up. Names and ports have to be unique.

## bypass

Hosts to pass through untouched: no rules, no events, no interception, the way
`NO_PROXY` works. An entry is a host (`api.stripe.com`), a host and port
(`localhost:9000`), or a domain suffix (`*.internal`). `localhost` is always on
the list, so Faultline never proxies itself.

## rules

A rule is an `id`, a `name`, what it `match`es, the `fault` it applies, and
optionally the `behavior` that decides when the fault applies. This is the same
rule the API takes, written in YAML; `docs` on each fault and its parameters
live in the schema.

- `id` is how everything else names the rule: scenarios, the API, the
  `Faultline-Fault` header. Letters, digits, `-`, `_` and `.`, and it has to be
  unique across the file, inline scenario rules included.
- `enabled` defaults to `true` for a rule written at the top level. A rule
  written inside a scenario defaults to `false`, because a scenario does nothing
  until it is activated.
- Everything left out of `match` matches everything, so a `match` with only a
  `host` applies to every request to that host.
- `fault` and `behavior` are tagged by `type`. The rest of the keys are that
  type's parameters, and no other key is accepted: `delay` takes `ms` and
  `jitter_ms`, `status` takes `code` and `body`, and so on.

## scenarios

A scenario is a `name` and the rules it turns on. Each entry is either the id of
a rule declared under `rules`, which is how one rule joins more than one
scenario, or a whole rule written in place, which is how a rule that belongs to
one situation stays next to it.

## Errors

Reading is strict, and loading stops at the first problem. An unknown key, a
fault parameter that does not exist, a rule with no id, two rules with the same
id, or a scenario naming a rule that is not there are all errors, and each one
names the file, the line, and the field:

```
faultline.yaml:14: rules[0].fault.ms: ms must be 1 or more
faultline.yaml:9: rules[1].id: duplicate rule id "slow-stripe"; ids name a rule everywhere else, so each one is used once
faultline.yaml:22: scenarios[0].rules[0]: there is no rule with id "ghost"; write it under rules, or write it here in full
```

Strictness is the point. A key Faultline quietly ignored would be a rehearsal
that never ran and a report that said everything was fine.

The same messages appear in the log when a save is read while Faultline is
running, at error level, followed by the rules that stayed in force:

```
ERROR config: the file was not applied, the rules already in memory stay in force
  path=faultline.yaml line=14 problem="ms must be 1 or more" field=rules[0].fault.ms
```
