# Changelog

## 0.1.0 — unreleased

- Caddy 2.11.7 and Cloudflare DNS provider 0.2.4.
- Shell-less runtime with non-root Caddy and private persistent storage.
- Exact-host Caddyfile and strict SNI wildcard JSON examples.
- Offline TLS/HTTP2/WebSocket tests and gated real DNS-01 test.
- ARM64 CI with dependency/image scanning; manual release publication only.
- Security dependency override: golang.org/x/net v0.60.0 fixes GO-2026-6603,
  GO-2026-6610, GO-2026-6611, GO-2026-6612 and GO-2026-6617.
