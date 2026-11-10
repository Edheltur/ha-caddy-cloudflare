# Installation and operations

## Before installation

This implementation is not released yet. The repository is
https://github.com/Edheltur/ha-caddy-cloudflare. The public image reference will be enabled
only after the approved GHCR release.

After release: Settings -> Apps (Add-ons on older UI) -> App store -> menu ->
Repositories -> add the published GitHub repository URL. Install Caddy Cloudflare
mTLS on **64-bit HA OS**; Raspberry Pi 4 hardware alone does not establish that
the OS is aarch64. Leave Protection mode enabled. Do not enable automatic updates
until you have an encrypted backup and a verified restore procedure.

The host must have TCP/443 available. Publish/forward only TCP/443 to HA OS.
Do not expose upstream ports, UDP/443 or TCP/80. Outbound DNS, HTTPS to Cloudflare
and Let's Encrypt, and access to configured upstreams are required.

## Cloudflare token

In Cloudflare create a custom API token:

- Zone / DNS / Edit.
- Zone / Zone / Read.
- Zone Resources: Include only the zone(s) used for issuance.

Do not use the Global API Key or account-wide permissions. This still grants
DNS write access across each selected zone, not just ACME TXT records. For
stronger separation, delegate ACME challenges to a dedicated DNS zone and
configure Caddy's supported challenge delegation; test it before deployment.
Ensure token expiry will not silently break certificate renewal.

Enter the token in `cloudflare_api_token` in the app configuration. `password`
means UI masking, not encryption at rest. Supervisor writes it to options;
the launcher passes it as `CF_API_TOKEN`, never as an argument. Do not run
`caddy environ`, `--environ` or debug/trace with production credentials.

## Configuration in Home Assistant

All user configuration is supplied through the app options. No external
configuration folder is mounted. In Configuration, select **Edit in YAML**:

```yaml
cloudflare_api_token: "YOUR_TOKEN"
config_format: caddyfile
client_ca_pem: |
  -----BEGIN CERTIFICATE-----
  PASTE_YOUR_PUBLIC_CA_CERTIFICATE_HERE
  -----END CERTIFICATE-----
caddy_config: |
  # Paste the complete examples/Caddyfile here with two-space indentation.
```

Use `examples/options.yaml` as the complete starting point. Replace the token,
exact domain names and upstream addresses. `homeassistant:8123` is the usual HA
listener; use the actual reachable hostname/IP and port for Music Assistant.
`localhost` refers to this container, not another app.

`caddy_config` is mandatory. Select `config_format: json` to provide native JSON
in the same option. `client_ca_pem` accepts public CA certificates, including a
bundle for rotation; private keys, leaf certificates and malformed input are
rejected without logging their content. Leave it empty only when client CA
trust is not needed (for example public HTTPS-only configuration).

The launcher writes `/data/caddy/addon-config` and, if provided,
`/data/caddy/client-ca.pem` atomically as UID 65532, mode 0600. The examples use
`/data/caddy/client-ca.pem` in their trust pool. Clearing the CA option removes
the generated CA on restart; a configuration still requiring that file fails
startup. No old external files or generated config are used as fallback.
Each option has a 128 KiB content limit. Caddy validates routing/TLS syntax.
Never put secrets directly in Caddy config: use `{env.CF_API_TOKEN}`. Syntax
errors from Caddy can quote configuration fragments.

Save and restart after any change; existing WebSockets reconnect. Admin API is
disabled, so `caddy reload` is not available. Supervisor stops via SIGTERM.
Keep HA login/MFA enabled. Test client-certificate support in each browser/app.

Migration from the earlier experimental file-based version: paste Caddyfile or
JSON into `caddy_config`, paste the public CA into `client_ca_pem`, and replace
old trust-pool paths with `/data/caddy/client-ca.pem`. Back up options and data
before updating. Old `/config` files are no longer mounted or read.

## Wildcard and TLS policies

Paste `examples/caddy-wildcard.json` into `caddy_config` and choose
`config_format: json`. Replace `*.example.com` in both certificate automation
locations; replace exact HA/MA names in both TLS policies and HTTP routes.
The wildcard controls certificate issuance, **not a wildcard upstream route**.

The JSON example explicitly disables automatic HTTP policy generation and
supplies exact SNI policies. Unknown or missing SNI has no matching TLS policy,
even if the loaded wildcard certificate would cover it. Do not add a catch-all
TLS policy, default/fallback SNI, on-demand TLS, or a wildcard host route.

TLS 1.2+ is default. For TLS 1.3-only set `protocols tls1.3 tls1.3` in each
Caddyfile site, or `protocol_min: tls1.3` in each JSON TLS policy. Keep
`strict_sni_host` enabled globally. HTTP/2 coalescing across different names is
intentionally refused with 421; clients must open the appropriate connection.

Use Cloudflare DNS-only records for direct client mTLS. An intermediate HTTP
proxy terminates the client's TLS and changes which certificate Caddy sees.

## Home Assistant

Enable `use_x_forwarded_for` and trust the Caddy proxy's source address in HA's
HTTP settings (UI or configuration appropriate to your installed Core version).
Prefer its precise address. If using a broader Supervisor subnet for operational
reasons, explicitly accept that other apps then share that trust boundary.
Do not add the internet, all private ranges or client addresses as trusted proxies.
Keep HA login/MFA enabled: mTLS is an additional gate, not a HA user identity.

WebSocket support is built into Caddy reverse_proxy. Client-certificate support
in every browser, companion application and media client must be tested; some
clients cannot supply a certificate. MA is publicly reachable without mTLS:
configure its authentication and permissions accordingly.

## External client certificate issuance

Example on an **offline administration workstation**, not HA. Protect the CA
private key with a passphrase and offline backup. These commands use an example
30-day client certificate; renew before expiry. Use unique keys per device.

```sh
umask 077
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -aes-256-cbc -out client-ca.key
openssl req -new -x509 -sha256 -days 3650 -key client-ca.key -out client-ca.pem \
  -subj '/CN=Home client CA' \
  -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign'
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out phone.key
openssl req -new -key phone.key -out phone.csr -subj '/CN=phone'
cat > client.ext <<'EXT'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature
extendedKeyUsage=clientAuth
EXT
openssl x509 -req -in phone.csr -CA client-ca.pem -CAkey client-ca.key \
  -CAcreateserial -days 30 -sha256 -extfile client.ext -out phone.pem
openssl pkcs12 -export -inkey phone.key -in phone.pem -certfile client-ca.pem -out phone.p12
```

PKCS#12 export prompts for a password. Transfer it securely to the intended
client. Retain issuance records outside the app. The example is an issuance
recipe, not a complete managed PKI. Never upload the generated private files
to GitHub or include them in a support request.

For emergency revocation, remove the compromised certificate's CA from trust,
restart Caddy and reissue affected clients using a new CA. Merely generating a
CRL does not make the baseline Caddy configuration enforce it. Already-open
connections are not retroactively authenticated until closed.

## Verify from outside your network

Set HA/MA DNS names and the public IPv4 address below. Keep server certificate
verification enabled (do not use curl `-k`). The public CA bundle used for the
server is different from the private CA used to issue client certificates.

```sh
curl --resolve ha.example.com:443:PUBLIC_IPV4 https://ha.example.com/
curl --cert phone.pem --key phone.key \
  --resolve ha.example.com:443:PUBLIC_IPV4 https://ha.example.com/
curl --resolve ma.example.com:443:PUBLIC_IPV4 https://ma.example.com/
curl --http2 --resolve ma.example.com:443:PUBLIC_IPV4 \
  -H 'Host: ha.example.com' https://ma.example.com/
openssl s_client -connect PUBLIC_IPV4:443 -servername ha.example.com \
  -verify_hostname ha.example.com -verify_return_error -tls1_3 \
  -cert phone.pem -key phone.key
```

Expected: first fails TLS client authentication; second reaches HA (possibly
its login/redirect); third reaches MA; fourth is 421, never HA. The OpenSSL
command must validate the server and complete with the valid client. Repeat
with `-tls1_2`; this must fail only if 1.3-only was configured. A TLS 1.3 client
can receive the client-auth alert on its first read rather than at connect.
Use the automated suite for unknown/expired client certificates and reused
HTTP/2 connections; separate curl commands do not test coalescing.

## Backup, update and rollback

Back up this app's options and data, encrypted. `backup: cold`
requests a stopped-app backup for consistent state. A backup contains the
Cloudflare token, ACME account key and private server TLS keys. Keep decryption
keys outside the same machine. The public CA and routing configuration are included in options.

Before updating: read release notes, take a backup, retain the previous image
version. Update through Supervisor, verify logs, HTTPS and client authentication.
Do not delete/reinstall as an update method and do not wipe `/data/caddy`.

To roll back, use the pre-upgrade app backup and its matching version. Restore
options and data together. Only one instance should operate that
restored state. Confirm account/certificate reuse and expiry after restart.
If the old image is no longer available, restore requires rebuilding its exact
source and pinned inputs; retain release images to avoid this dependency.

After host/backup compromise rotate the Cloudflare token and affected private
keys; restoring the same stolen keys does not remediate compromise.
