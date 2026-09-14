# Security

Faultline is a local development tool that terminates HTTPS with a CA it
controls, reads the traffic that goes through it, and rewrites responses on
purpose. [docs/security.md](docs/security.md) describes what it holds, what it
exposes, and the trust boundary it assumes. Read that before pointing it at
anything real.

## Verifying a release

Faultline terminates TLS with a CA it puts on your machine, so it is worth more
than most tools that you got the binary its authors built. Every release is
signed and carries build provenance, and neither check needs a key from us.

The signature is over `checksums.txt`, which names every archive, so one
verification covers whichever one you downloaded:

```
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/josipmusa/faultline/\.github/workflows/release\.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --check --ignore-missing checksums.txt
```

The identity is the point of that command: it says the release was built by the
workflow in this repository, not merely that somebody signed it.

The provenance attestation says the same thing through GitHub, and needs only
the `gh` CLI:

```
gh attestation verify --owner josipmusa faultline_0.1.0_linux_amd64.tar.gz
```

The install script checks the checksums for you but does not check the
signature, because it cannot assume cosign is installed. If that matters to you,
take the archive from the release page and run the commands above.

## Reporting a vulnerability

For something exploitable, do not open a public issue. Use the private
[security advisory form](https://github.com/josipmusa/faultline/security/advisories/new)
on this repository, or email the address on the repository owner's GitHub
profile. Expect an acknowledgement within a few days; please allow time for a
fix before disclosing.

Anything that is not itself exploitable, such as a hardening suggestion or a
documentation gap, is welcome as an ordinary issue.
