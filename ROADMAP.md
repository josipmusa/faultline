# Roadmap

Faultline v0.1 covers HTTP and HTTPS fault injection for a locally run
application: the `faultline run` wrapper, the forward proxy with TLS
interception, explicit routes, the container companion and transparent mode,
the fault catalogue, scenarios, the report, and the same capabilities through
the web UI, the CLI, the HTTP API and the MCP server.

What comes next depends on what people ask for. Open an issue if one of these
matters to you, or if something not listed does.

## Deliberately out of v0.1

- Per-service identity through proxy credentials
  (`HTTPS_PROXY=http://service:token@faultline:9001`), with rules scoped per
  service and an admin token for the control surface.
- A Prometheus metrics endpoint.
- A Python client library.
- Kubernetes sidecar injection.
- gRPC, WebSocket and raw TCP.
- Response recording for later replay. Only if users ask: this sits next to
  mocking, which Faultline stays out of by principle.
