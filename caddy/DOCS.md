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

Open the app's **Configuration → Edit in YAML**. The default JSON in
`caddy_config` sets up one HTTPS host with required mTLS and a server
certificate for that exact name.

1. Enter your zone-scoped token in `cloudflare_api_token`.
2. Paste the public client CA certificate into `client_ca_pem` as a YAML
   literal block (`|`) with two-space indentation. A bundle of public CA
   certificates is supported. Keep the CA private key on your signing device.
3. Replace every `ha.example.com` in the prefilled `caddy_config` with your
   hostname. Change `homeassistant:8123` if HA uses a different listener.
4. Save, start the app and inspect its log. Restart after later changes.

The app creates `/data/caddy/client-ca.pem` from the PEM option. Keep that
path in the default JSON trust pool. Clearing the option removes this file
on restart. The default configuration requires the CA to start. The JSON
and PEM fields each have a 128 KiB limit. Leave the Cloudflare token
placeholder `{env.CF_API_TOKEN}` in the JSON.

The default disables the Admin API, accepts TLS 1.2 and later, and checks
that the request Host matches its TLS SNI. Set `protocol_min` to `tls1.3` if
all clients support TLS 1.3. Keep exact SNI and Host policies for protected
hosts. Use Cloudflare DNS-only records so the client's TLS reaches Caddy.

## Home Assistant

Enable `use_x_forwarded_for` and trust the Caddy proxy's source address in HA's
HTTP settings (UI or configuration appropriate to your installed Core version).
Prefer its precise address. If using a broader Supervisor subnet for operational
reasons, explicitly accept that other apps then share that trust boundary.
Do not add the internet, all private ranges or client addresses as trusted proxies.
Keep HA login/MFA enabled: mTLS is an additional gate, not a HA user identity.

WebSocket support is built into Caddy reverse_proxy. Client-certificate support
depends on the browser and companion app. Test each intended client.

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

Set the HA DNS name and public IPv4 address below. Keep server certificate
verification enabled (do not use curl `-k`). The public CA bundle used for the
server is different from the private CA used to issue client certificates.

```sh
curl --resolve ha.example.com:443:PUBLIC_IPV4 https://ha.example.com/
curl --cert phone.pem --key phone.key \
  --resolve ha.example.com:443:PUBLIC_IPV4 https://ha.example.com/
openssl s_client -connect PUBLIC_IPV4:443 -servername ha.example.com \
  -verify_hostname ha.example.com -verify_return_error -tls1_3 \
  -cert phone.pem -key phone.key
```

The first request fails TLS client authentication. The second reaches HA
and can return its login page or redirect. The OpenSSL
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
