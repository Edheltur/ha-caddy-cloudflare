# Stage B validation

Checked locally on 2026-11-03 with the built Caddy v2.11.7 binary and
Cloudflare module v0.2.4:

- Caddyfile validation and DNS provider configuration.
- TLS 1.2/1.3 and TLS 1.3-only.
- Missing, foreign-CA, expired and valid client certificates.
- Public HTTPS, SNI/Host mismatch, unknown and missing SNI.
- Both exact-certificate Caddyfile and wildcard JSON variants.
- HTTP/2 requests for public/protected authorities on the same connection.
- WebSocket upgrade and frame forwarding.
- SIGTERM, synthetic-token log checks and persistent key reuse.

These are offline binary tests with ephemeral fixtures, not actual Cloudflare
issuance or Supervisor acceptance. The earlier internal-CA persistence test
is not evidence of ACME renewal. See README.md in this directory for the
remaining Docker, ARM64, Cloudflare staging and HA OS acceptance gates.

No image or release has been published. GitHub CI must pass before review is
complete. Source uploads/PR creation require write access for the GitHub App.
