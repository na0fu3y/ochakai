# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/na0fu3y/ochakai/security/advisories/new)
— do not open a public issue. You should receive a response within a few
days. The latest release is the supported version.

## Scope notes

ochakai's security posture is deliberately narrow (see
[docs/design/0002-authn-authz.md](docs/design/0002-authn-authz.md)):

- It holds **no warehouse credentials** and never executes SQL.
- It does **no authentication or authorization itself**: reachability is
  Cloud Run IAM's job, and ochakai trusts the identity headers Cloud Run
  forwards after its IAM check.

Especially interesting reports, given that design:

- Ways a request could smuggle or spoof the forwarded identity when
  deployed as documented in [deploy/cloudrun/README.md](deploy/cloudrun/README.md).
- Ways `compile_sql` output could be made to differ from the semantic
  definition or golden query it claims to come from (the compiled SQL is
  executed downstream with real warehouse credentials).
- Anything that makes `OCHAKAI_INSECURE_DEV` behavior reachable in a
  non-dev configuration.

Weaknesses that only manifest when the documented deployment posture is
not followed (e.g. running without Cloud Run IAM in front) are
documentation issues rather than vulnerabilities — still welcome, as
regular issues.
