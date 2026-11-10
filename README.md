# Caddy Cloudflare mTLS for Home Assistant OS

Experimental ARM64 Home Assistant app (add-on). Caddy v2.11.9 with
`caddy-dns/cloudflare` v0.2.4. `golang.org/x/net` is explicitly updated to
v0.62.0 for HTTP/2 security fixes found in the original v0.59.0 graph. One HTTPS listener, per-host mutual TLS,
Cloudflare DNS-01 and persistent ACME storage. No web UI, ingress, shell,
Docker socket, host network or extra ACME service.

**Status:** implementation candidate, not a published release. Repository: [Edheltur/ha-caddy-cloudflare](https://github.com/Edheltur/ha-caddy-cloudflare).
The GHCR image reference will be enabled only after an approved release.
No claim of installation/backup compatibility until HA OS acceptance passes.

## Architecture

Host TCP/443 maps to container TCP/8443. Caddy runs as UID/GID 65532 after a
small static launcher reads Supervisor options, prepares its private storage,
clears inherited environment and drops capabilities. Supervisor's Docker init
handles the container lifecycle (`init: true`); there is no S6.

The scratch runtime contains Caddy, the launcher and a public CA bundle.
Build tools, test Python packages and scanners are not runtime dependencies.
The standard Caddy modules are compiled; optional services are not enabled.
Caddy's standard module set has a substantial dependency graph: this project
does not maintain a trimmed fork to save a few megabytes.

## Files and secrets

| Path | Purpose |
|---|---|
| `/data/options.json` | Supervisor-managed token, config format and optional Caddy configuration |
| `/data/caddy` | Private ACME account, server certificates and keys |
| `/data/caddy/addon-config` | Configuration generated from `caddy_config` options, mode 0600 |
| `/data/caddy/client-ca.pem` | Public client CA generated from options |

Supervisor provides `/data`; no external configuration directory is mounted.
Do not rename the slug/repository identity or clear storage during upgrades.
Backups contain credentials and private keys as well as configuration options.

## Configuration and operation

See [installation and operations](caddy/DOCS.md),
[threat model](SECURITY.md), [tests](tests/README.md).
Examples are in [examples](examples). They are configuration examples, not
safe universal defaults for arbitrary upstreams. Replace domain names,
upstream addresses and actual Home Assistant ports before use.

Enter the complete configuration in `caddy_config` using the app settings YAML
editor; see [complete options example](examples/options.yaml). Supply the public client
CA in `client_ca_pem`. Configuration is mandatory; no file fallback exists.

Changes require an app restart. Admin API, config persistence, HTTP redirects
and HTTP/3 are disabled in the examples. DNS-01 requires no inbound port 80.
Do not enable Cloudflare's HTTP proxy in front of direct client mTLS.

## Build

Build on ARM64 with Docker BuildKit:

```sh
docker build --platform linux/arm64 -t caddy-ha:local caddy
```

Base image digest, Go module graph and action SHAs are fixed. Updating them
requires a reviewed PR. `go.mod/go.sum` plus checksum verification lock the
module inputs; no executable code is downloaded at container startup.
This is input reproducibility, not a claim of independently verified
bit-for-bit image reproducibility.

Only ARM64 images are built. Offline binary tests can also run on the
developer's native architecture without publishing another image variant.

## Release policy

The CI has no automatic publish on push or tag creation. Stage C requires a
manual workflow dispatch **on an approved version tag**, `publish=true`, and
the `ghcr-release` environment. Configure required reviewers for that
environment and protect `main`, version tags and workflow changes before use.

Version tags must identify commits reachable from `main` and agree with the
manifest. Do not retag releases. The public GHCR package and anonymous pull
must be checked explicitly. Supervisor consumes version tags; it does not
verify our SBOM/provenance as an installation admission policy.

Security update policy: weekly scheduled scans and update PRs; promptly review
relevant critical advisories, test patches, then approve a release. No automatic
merge or deployment. Retain the previous image and an encrypted pre-upgrade
backup for recovery.
