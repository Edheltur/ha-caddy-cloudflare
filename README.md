# Caddy Cloudflare mTLS for Home Assistant OS

Experimental ARM64 Home Assistant app (add-on). Caddy v2.11.9 with
`caddy-dns/cloudflare` v0.2.4. `golang.org/x/net` is explicitly updated to
v0.62.0 for HTTP/2 security fixes found in the original v0.59.0 graph. One HTTPS listener, per-host mutual TLS,
Cloudflare DNS-01 and persistent ACME storage. No web UI, ingress, shell,
Docker socket, host network or extra ACME service.

Install through the Home Assistant app store using this repository URL:
https://github.com/Edheltur/ha-caddy-cloudflare.
Published versioned ARM64 images are pulled from GHCR; installation does not
require local builds, Samba or FTP. See [operations](caddy/DOCS.md).
The app remains experimental until the [HA OS acceptance checks](tests/README.md) pass.

## Architecture

Host TCP/443 maps to container TCP/8443. Caddy runs as UID/GID 65532 after a
small static launcher reads Supervisor options, prepares its private storage,
clears inherited environment and drops capabilities. Supervisor's Docker init
handles the container lifecycle (`init: true`); there is no S6.

The scratch runtime contains Caddy, the launcher and a public CA bundle.
Build tools, Go integration tests and scanners are not runtime dependencies.
The standard Caddy modules are compiled; optional services are not enabled.
Caddy's standard module set has a substantial dependency graph: this project
does not maintain a trimmed fork to save a few megabytes.

## Files and secrets

| Path | Purpose |
|---|---|
| `/data/options.json` | Supervisor-managed token, required native JSON and public client CA |
| `/data/caddy` | Private ACME account, server certificates and keys |
| `/data/caddy/addon-config` | Configuration generated from `caddy_config` options, mode 0600 |
| `/data/caddy/client-ca.pem` | Public client CA generated from options |

Supervisor provides `/data`; no external configuration directory is mounted.
Do not rename the slug/repository identity or clear storage during upgrades.
Backups contain credentials and private keys as well as configuration options.

## Configuration and operation

See [installation and operations](caddy/DOCS.md),
[threat model](SECURITY.md), [tests](tests/README.md).
Edit the JSON prefilled in `caddy_config` in the app settings YAML editor.
The defaults proxy one Home Assistant hostname and require mTLS. Replace the
hostname, enter the Cloudflare token and supply the public client CA in
`client_ca_pem`.

Changes require an app restart. Admin API, config persistence, HTTP redirects
and HTTP/3 are disabled in the default configuration. DNS-01 requires no inbound port 80.
Do not enable Cloudflare's HTTP proxy in front of direct client mTLS.

Build and test instructions are in [the developer guide](tests/README.md).

## Release policy

See [the release checklist](RELEASING.md). Publication is manual and restricted
to approved version tags from main. The exact release image is tested and
scanned by digest before receiving its version tag. SBOM and provenance are
retained; Supervisor does not verify these as an installation admission policy.

Security update policy: weekly scheduled scans and reviewed update PRs.
Review relevant critical advisories promptly, test patches, then release.
No automatic merge or deployment. Retain the previous image and an encrypted
pre-upgrade backup for recovery.
