# Contributing to Faultline

Thanks for looking at this. [AGENTS.md](AGENTS.md) holds the conventions the
code follows, the repository layout and the decisions that are settled; it is
written for coding agents and applies to people just the same.

## Before you start

Faultline is early. If you want to add something larger than a fix, open an
issue first and say what you have in mind, so the design can be agreed before
the code exists. Bug reports are welcome at any size; the most useful one has
the command you ran, what Faultline printed, and what you expected.

## Working on it

You need Go 1.25 or newer, Node 22 for the UI and the TypeScript client, and a
C compiler on the PATH for the race detector.

```
make tools     # golangci-lint
make ui        # build the UI export the binary embeds
make clients   # install the TypeScript client's toolchain
make build     # ./bin/faultline
make test      # Go with -race, then the UI and client suites
make lint      # go vet, golangci-lint, eslint, tsc
```

`make test` and `make lint` must be clean before a pull request is opened, and
CI runs both on Linux and macOS.

A few things the code holds to:

- Prefer the smallest change that solves the problem. No speculative
  abstractions and no configurability nobody asked for.
- A behavior change comes with a test that fails without it. Tests live next
  to the code they test and use `httptest` for upstreams.
- Validate at the boundary and name the field in the error.
- Files stay under about 400 lines, split by feature.

## Documentation

User documentation lives in `docs/`, and user-visible behavior changes come with
the matching doc change. Two files are generated from the fault catalogue and
must not be edited by hand: `schema/faultline.schema.json` and
`docs/reference.md`. `make docs` rewrites both, and a test fails when they are
stale.

## License

By contributing you agree that your contributions are licensed under the MIT
license in [LICENSE](LICENSE).
