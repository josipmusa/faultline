# Faultline

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
- [docs/config.md](docs/config.md) — the faultline.yaml file.
- [docs/frontends.md](docs/frontends.md) — why browser traffic needs an explicit route.

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
