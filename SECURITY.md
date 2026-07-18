# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/na0fu3y/ochakai/security/advisories/new)
— do not open a public issue. You should receive a response within a few
days. The latest release is the supported version.

## Scope notes

ochakai's security posture is deliberately narrow (see
[docs/design/0002-authn-authz.md](docs/design/0002-authn-authz.md) and
[0010](docs/design/0010-mcp-oauth-connector.md)):

- It holds **no warehouse credentials** and never executes SQL.
- It does **no authorization**: whoever can reach a deployment can read
  and write; identity is recorded as provenance only. Reachability is
  controlled per surface:
  - **Private service** (the default): Cloud Run IAM. ochakai trusts the
    identity headers Cloud Run forwards after its IAM check and must
    **never run publicly invokable**.
  - **Connector service** (opt-in, design doc 0010): publicly reachable
    by design. Reachability is its own OAuth tokens; Google id_tokens
    are signature-verified and the `hd` Workspace-domain claim is
    enforced. Tokens are 256-bit random values stored only as SHA-256
    hashes, refresh tokens rotate, and reuse of a rotated refresh token
    revokes the grant.

Especially interesting reports, given that design:

- Ways a request could smuggle or spoof the forwarded identity when
  deployed as documented in [deploy/cloudrun/README.md](deploy/cloudrun/README.md).
- On the connector: bypassing the `hd` domain check, bypassing CIMD
  client validation (including SSRF through the client_id URL fetch),
  abusing refresh-token rotation races, or reaching any non-OAuth,
  non-`/mcp` surface (REST, web UI) on a connector-mode deployment.
- Ways `compile_sql` output could be made to differ from the semantic
  definition or golden query it claims to come from (the compiled SQL is
  executed downstream with real warehouse credentials).
- Anything that makes `OCHAKAI_INSECURE_DEV` behavior reachable in a
  non-dev configuration.

Weaknesses that only manifest when the documented deployment posture is
not followed (e.g. running the *private* service without Cloud Run IAM
in front) are documentation issues rather than vulnerabilities — still
welcome, as regular issues.
