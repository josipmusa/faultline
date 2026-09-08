# Contributing to Faultline

Read [AGENTS.md](AGENTS.md) first. It holds the engineering conventions, the
repository layout, the fixed architectural decisions, and the definition of done
for a task. It applies to humans and to coding agents equally.

The short version:

- Work on one [ROADMAP.md](ROADMAP.md) task at a time, in order.
- A task is not done until its **Verify** block passes.
- `make test` and `make lint` must be clean before you open a pull request.
- Prefer the smallest change that satisfies the task. No speculative
  abstractions.
- If a task is unclear or seems to conflict with AGENTS.md, ask before widening
  its scope.

Faultline is pre-alpha and the roadmap is the plan of record. If you want to
propose something that is not on it, open an issue first.
