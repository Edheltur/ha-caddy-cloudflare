# Caddy Cloudflare mTLS

Experimental ARM64 HTTPS reverse proxy for Home Assistant OS with Cloudflare
DNS-01 and per-host mTLS. Read [DOCS.md](DOCS.md) before starting.

Enter `cloudflare_api_token`, `caddy_config` and public `client_ca_pem` in the
app settings YAML editor. Edit the prefilled native JSON in `caddy_config`.
No external configuration files or CA private keys are needed or accepted.

HA uses single-line fields in the form view. For readable JSON, use
**Configuration → menu → Edit in YAML** with `caddy_config: |` and indent
its content. Client CA PEM also accepts folded or removed line breaks.
See DOCS.md for the editor limitations and external port mapping.
