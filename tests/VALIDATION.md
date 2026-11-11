# Stage B validation

The [ARM64 run before the JSON-only change](https://github.com/Edheltur/ha-caddy-cloudflare/actions/runs/42907651857)
passed dependency checks, the image build, offline TLS tests, container runtime
checks and the Trivy scan. See [PR #1](https://github.com/Edheltur/ha-caddy-cloudflare/pull/1)
for the current revision and its checks.

The offline tests use ephemeral certificates and test the JSON default
from the add-on manifest. Internal CA key reuse does not verify ACME renewal.
Real Cloudflare staging and Supervisor acceptance remain open. See
[the test guide](README.md) for the remaining checks.
