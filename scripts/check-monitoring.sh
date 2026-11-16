#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
[[ ${WSL_DISTRO_NAME:-} == OPSD-CI ]] || { echo '请在独立的 OPSD-CI 发行版运行' >&2; exit 1; }
mkdir -p artifacts/monitoring-native/bin artifacts/local-ci
exec > >(tee artifacts/local-ci/monitoring.log) 2>&1
for spec in 'minio RELEASE.2025-09-07T16-13-09Z' 'mc RELEASE.2025-08-13T08-35-41Z'; do
  read -r name release <<< "$spec"
  binary="artifacts/monitoring-native/bin/$name"
  if [[ ! -x $binary ]] || ! "$binary" --version | head -n 1 | grep -q "$release"; then
    curl -fsSL -C - --retry 2 --max-time 3600 -o "$binary" "https://github.com/minio/$name/releases/download/$release/$name.linux-amd64.$release"
    chmod +x "$binary"
    "$binary" --version | head -n 1 | grep -q "$release"
  fi
done
docker image inspect quay.io/thanos/thanos:v0.42.5 > /dev/null 2>&1 || docker pull quay.io/thanos/thanos:v0.42.5
go -C backend test -c -o ../artifacts/monitoring-native.test ./internal/cluster
(cd backend/internal/cluster && sudo env OPSD_MONITORING_TEST=1 ../../../artifacts/monitoring-native.test -test.run '^TestMonitoringNativeS3ThanosAndLoad$' -test.v -test.timeout=12m)
printf '%s\n' '监控验收通过：artifacts/monitoring-native-result.json'
