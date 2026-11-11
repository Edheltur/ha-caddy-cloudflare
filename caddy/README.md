# Caddy Cloudflare mTLS

Experimental ARM64 HTTPS reverse proxy for Home Assistant OS with Cloudflare
DNS-01 and per-host mTLS. Read [DOCS.md](DOCS.md) before starting.

Enter `cloudflare_api_token`, `caddy_config` and public `client_ca_pem` in the
app settings YAML editor. Edit the prefilled native JSON in `caddy_config`.
No external configuration files or CA private keys are needed or accepted.

Configuration uses only the YAML editor. Keep `caddy_config: |` and indent
its content. Validation runs on startup; Supervisor no longer validates options
on save. The API token is visible in YAML. Client CA PEM accepts folded or removed
line breaks. See DOCS.md for formatting and external port mapping.
