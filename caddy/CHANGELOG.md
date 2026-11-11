# Changelog

## 0.1.0 — unreleased

- ARM64 Home Assistant app with Caddy 2.11.9 and Cloudflare DNS provider 0.2.4.
- Scratch runtime with non-root Caddy, a static launcher and persistent ACME storage.
- Native JSON and public client CA configured entirely through app options.
- Default Home Assistant route requires mTLS, exact SNI/Host matching and TLS 1.2+.
- Go tests cover TLS, mTLS, HTTP/2, WebSocket, restart persistence and container privileges.
- Manual Cloudflare ACME staging checks; HA OS acceptance remains required.
- Versioned GHCR distribution with SBOM/provenance and tests of the release digest.
- Go 1.27.4 and reviewed security dependency pins; no scanner exceptions.
