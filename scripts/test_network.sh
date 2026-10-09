#!/usr/bin/env bash
# Real kernel verification in a disposable network stack. Never uses host networking.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/wgm-network-test.XXXXXX")
trap 'rm -rf "${test_dir}"' EXIT
CGO_ENABLED=0 go test -c -o "${test_dir}/services.test" ./internal/services
docker run --rm --network none --privileged \
  --env WGM_ISOLATED_NETWORK_TEST=1 \
  --env WGM_NETWORK_DIAGNOSTICS_SCRIPT=/tmp/diagnose_network.sh \
  --mount "type=bind,src=${test_dir}/services.test,dst=/tmp/services.test,readonly" \
  --mount "type=bind,src=${PWD}/scripts/diagnose_network.sh,dst=/tmp/diagnose_network.sh,readonly" \
  --entrypoint /tmp/services.test \
  "${WGM_NETWORK_TEST_IMAGE:-ghcr.io/chouhx/wireguardmanager:latest}" \
  -test.run '^Test(MultiInterfaceIntegration|LegacyMigrationIntegration|ProvisionRollbackIntegration|TenantNATTransportIntegration|GatewayBootstrapIntegration)$' \
  -test.v -test.timeout=120s
