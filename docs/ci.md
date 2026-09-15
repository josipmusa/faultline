# Running Faultline in CI

A test suite that proves an application survives a dependency going down is
worth running on every pull request, not only on the machine where it was
written. The `josipmusa/faultline/setup` action installs the binary on a GitHub
Actions runner, and optionally starts one for the rest of the job.

## Wrapping the tests

The shortest useful workflow installs Faultline and puts the test command
inside `faultline run`, which is the same front door as on a developer's
machine: the tests see proxy and trust variables they never had to set up, and
the report lands in a file.

```yaml
jobs:
  resilience:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod

      - uses: josipmusa/faultline/setup@v0

      - run: faultline run --report report.json -- go test ./...
```

`@v0` is a tag that moves to each new release once that release has been
verified, so a workflow written this way keeps up without being edited. Pin an
exact tag such as `@v0.1.0` instead if you would rather decide when to move.
Note that this is the version of the *action*; the version of Faultline it
installs is the `version` input below, and the two are independent.

The tests add their own rules through the API at `http://127.0.0.1:9000`, the
way `docs/cli.md` describes for a local run. Nothing in the test code names
Faultline's address for traffic: the proxy comes from the environment.

## Starting one for the whole job

`start: true` brings up a Faultline in the background instead, and writes the
attach environment into `$GITHUB_ENV`, so every later step in the job is
proxied without being wrapped. Use it when the thing to exercise is not a test
command - a built binary, a `curl`, a service started by another action.

```yaml
      - uses: josipmusa/faultline/setup@v0
        id: faultline
        with:
          start: true

      # Proxied and trusting the CA, because the step above said so.
      - run: curl -sS https://api.stripe.com/v1/charges

      - run: faultline session report --admin ${{ steps.faultline.outputs.admin-url }}
```

The action creates the interception CA before starting: `serve` does not create
one the way `run` does, and without a CA every response-tier rule is accepted
and never applies, which reads as an application that coped.

## Inputs

| input | default | meaning |
| --- | --- | --- |
| `version` | `latest` | the release to install; `v0.1.0` and `0.1.0` both work. `latest` is the newest non-prerelease, so it never resolves to a release candidate |
| `start` | `false` | start an instance and point the rest of the job at it |
| `bind` | `127.0.0.1` | the address the started instance listens on |
| `admin-port` | `9000` | the UI, the API and the MCP endpoint |
| `proxy-port` | `9001` | the forward proxy |
| `github-token` | `${{ github.token }}` | used to read the releases API and download the archive |

## Outputs

| output | meaning |
| --- | --- |
| `version` | the tag actually installed |
| `admin-url` | the admin address; empty unless `start` was true |
| `proxy-url` | the forward proxy address; empty unless `start` was true |
| `ca-path` | the CA certificate; empty unless `start` was true |

## What the action does and does not do

The archive is checksummed against the release's `checksums.txt` before it is
extracted, and `faultline version` runs as a smoke check, so a broken or
tampered download fails in the install step rather than somewhere downstream.

With `start: true`, the readiness check polls the admin API and fails the job if
nothing answers within 15 seconds, printing the server log. A job whose proxy
never came up must not run its tests unproxied and report a pass.

It does not set `JAVA_TOOL_OPTIONS`. A JVM reads neither the proxy variables nor
the CA bundle, so a Java step needs `faultline trust java` - see
[docs/trust.md](trust.md). It does not install the CA into the runner's OS trust
store either; the trust variables cover Go, Node, Python, curl and git.

## A worked example

[`.github/workflows/example.yml`](../.github/workflows/example.yml) in this
repository is the first shape above, running `examples/go-client`'s own test
under `faultline run`. The test breaks `example.com` with a `status` fault for
the first two calls and asserts the report saw three calls, two faulted and two
retries - so a pass means the installed binary, the proxy, the CA and the
injected environment all did their part.
