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
