# Test levels and release gates

## Offline developer tests

Build Caddy and run the integration tests from the repository root:

```sh
CGO_ENABLED=0 go -C caddy/build build -mod=readonly -buildvcs=false -trimpath -o ../../caddy-bin .
CADDY_TEST_BINARY="$PWD/caddy-bin" go -C caddy/build test -mod=readonly -count=1 -v ./integration
```

`TestTLS` validates the default JSON from `caddy/config.yaml` and runs it with
ephemeral server/client certificates and a loopback upstream. It checks TLS
1.2/1.3, missing/foreign/expired/valid client certificates, unknown and missing
SNI, Host mismatch, HTTP/2 connection reuse and WebSocket forwarding. A
test-only public hostname shares a wildcard certificate with HA for the
cross-host checks. `TestInternalStorage` checks key reuse across SIGTERM and
restart. Offline tests do not write DNS records or use production credentials.

Launcher unit tests run in the existing launcher module:

```sh
go -C caddy/launcher test -mod=readonly ./...
```

## Runtime / CI

On a disposable Linux host with Docker, build the test binary as your user,
then run the container test as root:

```sh
mkdir -p .test
docker build -t caddy-ha:test caddy
go -C caddy/build test -mod=readonly -c -o "$PWD/.test/integration.test" ./integration
(cd caddy/build/integration && sudo env CADDY_TEST_IMAGE=caddy-ha:test ../../../.test/integration.test -test.v -test.run '^TestRuntime$')
```

The test binds HTTPS to an ephemeral loopback port, inspects the running
process, and checks the mounted state and generated CA. It also verifies
mTLS, stale CA removal, key reuse, safe token rejection and SIGTERM.
CI runs on ARM64 with Go vulnerability checks and Trivy. BuildKit publishes
SBOM and provenance in the gated release job.

## Real Cloudflare integration (explicit opt-in)

Use a disposable test zone and a scoped token. The manual workflow runs
from main with the protected `acme-staging` environment. Set its secret
`CF_API_TOKEN` and variable `ACME_TEST_DOMAIN` to an exact test hostname.

For a local run, set those variables securely and use the binary built above:

```sh
ACME_ALLOW_DNS_WRITE=yes CADDY_TEST_BINARY="$PWD/caddy-bin" go -C caddy/build test -mod=readonly -count=1 -v ./integration -run '^TestACMEStaging$'
```

The test checks staging issuance and certificate/account key reuse after
restart. It verifies the certificate hostname and keeps Caddy logs private.
Check ACME TXT cleanup after successful and interrupted runs. Renewal still
requires a long-running staging test.

## Required HA OS acceptance before public release

- Record HA OS, Supervisor and Core versions; install on RPi4 aarch64.
- Keep Protection mode/AppArmor enabled; verify options-based config and CA permissions.
- Verify first start with root-owned /data, no extra permissions, only TCP/443.
- Verify HA upstream address/port, public DNS and external mTLS.
- Verify the JSON default appears in HA, then edit, save and apply it.
- Verify companion-app client certificates and WebSocket reconnect after restart.
- Issue and renew a staging certificate with the real DNS provider.
- Update and roll back; verify ACME identity and certificates are retained.
- Restore an encrypted cold backup, including options, on clean HA.
- Verify no credentials in logs, backup sharing, source, image or build artifacts.
- After stage C, anonymously pull the exact public GHCR version from a clean client.

No local Docker test proves Supervisor-specific acceptance. Keep the project
experimental and do not declare these gates passed without recorded evidence.
