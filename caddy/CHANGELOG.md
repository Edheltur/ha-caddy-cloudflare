# Changelog

## 0.1.0 — unreleased

- Caddy 2.11.9 and Cloudflare DNS provider 0.2.4.
- Shell-less runtime with non-root Caddy and private persistent storage.
- Exact-host Caddyfile and strict SNI wildcard JSON examples.
- Offline TLS/HTTP2/WebSocket tests and gated real DNS-01 test.
- ARM64 CI with dependency/image scanning; manual release publication only.
- Security dependency override: golang.org/x/net v0.60.0 fixes GO-2026-6603,
  GO-2026-6610, GO-2026-6611, GO-2026-6612 and GO-2026-6617.

- Accept complete Caddyfile/JSON through `caddy_config` in HA options; retain file fallback.

- Refresh go-jose/v4 to 4.1.9, x/net to 0.62.0, x/crypto to 0.58.0 and x/sys to 0.50.0; retain upstream v3 security backport.
- Refresh test-only cryptography to 50.0.2 and h2 to 4.4.1.

- Breaking (experimental): config and public client CA now come exclusively from app options; remove external config mount and file fallback.
