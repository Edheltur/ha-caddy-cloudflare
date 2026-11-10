# Test levels and release gates

## Offline developer tests

Build the two binaries using the pinned Go toolchain and `CGO_ENABLED=0`, then:

```sh
python3 -m venv .test/venv
.test/venv/bin/pip install -r tests/requirements.txt
.test/venv/bin/python tests/security.py ./caddy-bin
```

`security.py` validates the shipped Caddyfile and wildcard JSON, substitutes
locally generated server/client certificates and loopback upstreams, then runs
TLS 1.2/1.3, missing/foreign/expired/valid clients, public HTTPS, missing/unknown
SNI, Host mismatch, actual HTTP/2 connection reuse, WebSocket frame forwarding,
TLS 1.3-only and SIGTERM/log checks. No production secrets, DNS writes or ACME
issuance occur here. Client-side verification is disabled ONLY for ephemeral
server fixtures; server-side client verification remains the subject of testing.

Launcher tests cover malformed options/token non-disclosure, environment
allowlisting, multiline/size validation and refusal to follow state-directory
symlinks. Inline config writes replace destination symlinks without following
them and use mode 0600.

## Runtime / CI

On a root-capable disposable Linux host:

```sh
sudo python3 tests/runtime.py --image caddy-ha:test
```

This starts the actual image with root-owned bind mounts, inspects the actual
Caddy process UID/GID and all capability sets, tests internal-CA key permissions,
SIGTERM and restart persistence. The restart supplies a public CA through options and checks generated
configuration and CA ownership/mode. Optional no-argument mode uses an empty chroot
with locally built static binaries. Neither may be run against production paths.
These tests require real kernel privileges: uid 0 alone is not sufficient.

CI uses a native ARM64 runner, no production credentials, Go vulnerability scans
and Trivy. New findings fail the job; evaluate and fix them rather than silently
adding an ignore. BuildKit publishes SBOM/provenance only in the gated release job.

## Real Cloudflare integration (explicit opt-in)

Use a dedicated disposable test zone and scoped token. Never use production
credentials on a public PR. `acme-staging.yaml` runs only by manual dispatch
from main, uses the protected `acme-staging` environment, and injects secrets
only into the final test step. Set environment secret `CF_API_TOKEN` and variable
`ACME_TEST_DOMAIN` (an ordinary exact hostname in the test zone).

Locally set those environment variables securely, then run:

```sh
ACME_ALLOW_DNS_WRITE=yes python3 tests/acme_staging.py ./caddy-bin
```

The script uses Let's Encrypt staging, performs real DNS-01 issuance, restarts
and compares ACME account/server keys. Staging server certificates are not
publicly trusted; validation is deliberately disabled only in this test.
Do not upload logs or temporary private material. Confirm ACME TXT cleanup in
the zone after both successful and interrupted tests. This test does not claim
renewal coverage: verify renewal in a long-running staging acceptance deployment.

## Required HA OS acceptance before public release

- Record HA OS, Supervisor and Core versions; install on RPi4 aarch64.
- Keep Protection mode/AppArmor enabled; verify options-based config and CA permissions.
- Verify first start with root-owned /data, no extra permissions, only TCP/443.
- Verify HA and MA actual upstream addresses/ports, public DNS and external mTLS.
- Verify companion-app client certificates and WebSocket reconnect after restart.
- Issue and renew a staging certificate with the real DNS provider.
- Update and roll back; verify ACME identity and certificates are retained.
- Restore an encrypted cold backup, including options, on clean HA.
- Verify no credentials in logs, backup sharing, source, image or build artifacts.
- After stage C, anonymously pull the exact public GHCR version from a clean client.

No local Docker test proves Supervisor-specific acceptance. Keep the project
experimental and do not declare these gates passed without recorded evidence.
