#!/usr/bin/env bash
set -euo pipefail
image=${1:?image reference required}
root=$(git rev-parse --show-toplevel)
cd "$root"
mkdir -p .test
cid=$(docker create "$image")
trap 'docker rm "$cid" >/dev/null' EXIT
docker cp "$cid:/usr/bin/caddy" .test/caddy-bin
go -C caddy/build test -mod=readonly -c -o "$root/.test/integration.test" ./integration
cd caddy/build/integration
CADDY_TEST_BINARY="$root/.test/caddy-bin" \
  "$root/.test/integration.test" -test.v -test.run '^(TestTLS|TestInternalStorage)$'
sudo env CADDY_TEST_IMAGE="$image" \
  "$root/.test/integration.test" -test.v -test.run '^TestRuntime$'
