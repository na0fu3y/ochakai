# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/na0fu3y/ochakai/security/advisories/new)
— do not open a public issue. You should receive a response within a few
days. The latest release is the supported version — there is no support
window and no backporting ([docs/compatibility.md](docs/compatibility.md)).

## Scope notes

ochakai's security posture is deliberately narrow (see
[docs/design/0002-authn-authz.md](docs/design/0002-authn-authz.md)):

- It holds **no warehouse credentials** and never executes SQL.
- It does **no authorization**: whoever can reach a deployment can read
  and write; identity is recorded as provenance only. Reachability is
  Cloud Run IAM's job: ochakai trusts the identity headers Cloud Run
  forwards after its IAM check, and a deployment that reads those headers
  must **never run publicly invokable** — nothing verified their
  signature, so a public one would let any caller name any person.
  (The publicly reachable MCP OAuth connector service existed briefly
  and was retired in 0.9.0.)
- There is exactly one public posture, and it is public because it
  believes nothing: `OCHAKAI_PUBLIC_READ_ONLY` reads no identity at all
  and refuses every write (design docs
  [0040](docs/design/0040-read-only-mode.md),
  [0042](docs/design/0042-public-read-only.md)). A deployment that is
  publicly readable and writable is not a configuration ochakai accepts.
  A report that this posture reads a header it should not, or that a
  write reaches the database through it, is a vulnerability.

Especially interesting reports, given that design:

- Ways a request could smuggle or spoof the forwarded identity when
  deployed as documented in [deploy/cloudrun/README.md](deploy/cloudrun/README.md).
- Anything that makes `OCHAKAI_INSECURE_DEV` behavior reachable in a
  non-dev configuration.

Weaknesses that only manifest when the documented deployment posture is
not followed (e.g. running the *private* service without Cloud Run IAM
in front) are documentation issues rather than vulnerabilities — still
welcome, as regular issues.
