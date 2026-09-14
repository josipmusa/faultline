# Security

Faultline is a local development tool that terminates HTTPS with a CA it
controls, reads the traffic that goes through it, and rewrites responses on
purpose. [docs/security.md](docs/security.md) describes what it holds, what it
exposes, and the trust boundary it assumes. Read that before pointing it at
anything real.

## Reporting a vulnerability

For something exploitable, do not open a public issue. Use the private
[security advisory form](https://github.com/josipmusa/faultline/security/advisories/new)
on this repository, or email the address on the repository owner's GitHub
profile. Expect an acknowledgement within a few days; please allow time for a
fix before disclosing.

Anything that is not itself exploitable, such as a hardening suggestion or a
documentation gap, is welcome as an ordinary issue.
