# Security policy and threat model

## Trust boundaries

Untrusted internet -> Caddy TLS policy -> exact HTTP host route -> upstream.
The configuration administrator, Supervisor, HA OS host and build/release
maintainers are trusted. User-supplied Caddy configuration is executable policy:
it can re-enable listeners, expose files or proxy elsewhere. This app does not
sandbox an administrator against their own configuration changes.

The launcher reads only the supported options, rejects malformed credentials
without echoing them, creates `/data/caddy` without following a symlink, restricts
permissions and executes Caddy with a minimal environment. It does not generate
routing configuration. Startup root is needed for Supervisor bind-mount ownership
and capability removal. Caddy itself has no capabilities and no new privileges.
No `privileged`, host namespace, hardware or API permissions are requested.

Supervisor may still give the container limited API access and inject tokens;
those tokens are not inherited by Caddy. Removing environment tokens is not a
network firewall or a boundary against a compromised host. Docker image USER
is intentionally root for startup, so the HA security score may not reflect the
actual Caddy process UID.

## Threats and mitigations

| Threat | Mitigation / remaining risk |
|---|---|
| TLS auth bypass by Host or HTTP/2 authority | Global strict SNI/Host, separate TLS policies and tests on a single reused connection |
| Wildcard certificate accepts unknown SNI | Native JSON exact-name TLS policies; no catch-all; auto-HTTPS policy generation disabled |
| Direct upstream exposure | Router/firewall must expose only Caddy TCP/443; keep HA authentication enabled |
| Public MA used to reach HA | No such Caddy route; upstream SSRF and MA credentials need separate application/network controls |
| Caddy compromise | Non-root, no capabilities, narrow mounts; active DNS token and TLS keys remain accessible by necessity |
| Cross-add-on/host compromise | Outside the isolation guarantees of this app; Supervisor network is shared |
| Token leak | Options not argv; runtime env placeholder; prevalidation; no debug/env dumps; negative tests |
| Backup theft | Encrypt backups and protect decryption keys; rotate token and certificates after exposure |
| Dependency/release compromise | Fixed inputs, reviewed changes, scanners, protected release environment, SBOM/provenance |
| DDoS | No upstream volumetric protection; configure external network controls if needed |

Cloudflare v0.2.4 includes malformed token values in a provisioning error. The
launcher duplicates its accepted token grammar to intercept that case. Keep
this check and its tests aligned when upgrading the provider. Never paste
secrets into config literals, enable trace/debug, or publish real credential
logs to diagnose failures. Secret scans with test values do not prove that
arbitrary future upstream error paths can never disclose a secret.

## Temporary dependency pin

The build pins `go-jose/v3` to upstream commit
[`0e502b87c7aa651155b11ae8e195ec33ce10230a`](https://github.com/go-jose/go-jose/commit/0e502b87c7aa651155b11ae8e195ec33ce10230a)
(`v3.0.6-0.20261030183946-0e502b87c7aa`). This is the upstream RSA JWK
denial-of-service backport for GHSA-mgcc-cww5-fp2q, pending a tagged v3 release.
`go-jose/v4` is updated to v4.1.8. Both remain checksum-locked in `go.sum`;
no local cryptographic patches or scanner exceptions are used. Replace the v3
pin with a reviewed tagged release containing the fix when available.

## Client revocation

CA chain verification checks validity and trust; it is not automatic CRL/OCSP
revocation enforcement. The baseline response to a stolen client key is removing
its issuing CA from the trust file, restarting (closing existing connections),
and reissuing affected clients under a new CA. Use short-lived client certificates.
A leaf allowlist could support individual client removal, but is not part of the
validated baseline. Do not assume publishing a CRL is sufficient.

## Reporting

After GitHub provisioning, enable private vulnerability reporting. Do not open
public issues containing keys, tokens or exploitable deployment details.
The project is experimental until the documented HA OS release gates pass.
