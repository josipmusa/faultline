<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <img src="docs/assets/logo-light.svg" alt="Faultline" width="314">
</picture>

> **Status: pre-alpha.** Nothing here works yet. The CLI is a skeleton. See
> [ROADMAP.md](ROADMAP.md) for what is being built and in what order.

Faultline shows developers what their application says to the outside world, and
lets them make that world misbehave on purpose. It sits between an application
and the services it depends on. It shows every outbound call live, and it can
slow those calls down, fail them, cut them off, or make them flaky, so the
developer can see how the application copes before a real outage does it for
them.

Faultline is a single binary. Its front door is:

```
faultline run -- <your app's start command>
```

## Documents

- [SPEC.md](SPEC.md) — what the product does.
- [ROADMAP.md](ROADMAP.md) — the build plan, stage by stage.
- [AGENTS.md](AGENTS.md) — engineering conventions and fixed decisions.
- [CONTRIBUTING.md](CONTRIBUTING.md) — how to work on it.
- [docs/cli.md](docs/cli.md) - the command line.
- [docs/config.md](docs/config.md) — the faultline.yaml file.
- [docs/agents.md](docs/agents.md) - the MCP interface for coding agents.
- [docs/frontends.md](docs/frontends.md) — why browser traffic needs an explicit route.

## For coding agents

Faultline ships a skill, `resilience-check`, that teaches a coding agent when to
reach for fault injection and how to run a check end to end: see what the
application really calls, break one dependency, exercise it, and read what the
application did. It carries method rather than a catalogue, so it never goes
stale as faults are added, and it drives the command line, so it works for an
agent with no MCP server attached.

In Claude Code, this repository is also a plugin:

```
/plugin marketplace add josipmusa/faultline
/plugin install faultline@faultline
```

In any other harness that reads a skills directory, copy the skill into it:

```
cp -r skills/resilience-check <your skills directory>/
```

The skill names no harness and no paths, so the copy works anywhere. An agent
gets more out of it with the [MCP server](docs/agents.md) attached, and needs
nothing but the binary without one.

## Development

```
make tools     # install golangci-lint
make build     # ./bin/faultline
make test      # go test -race ./...
make lint      # go vet + golangci-lint
make schema    # regenerate schema/faultline.schema.json from the fault catalogue
```

Requires Go 1.25 or newer. `make test` uses the race detector, which needs a C
compiler on the PATH.

## License

MIT. See [LICENSE](LICENSE).
