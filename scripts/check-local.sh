#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
[[ ${WSL_DISTRO_NAME:-} == OPSD-CI ]] || { echo '请在独立的 OPSD-CI 发行版运行' >&2; exit 1; }
stage=${1:-all}
case "$stage" in all|build|shared|backend|firewall|themes|api) ;; *) echo '未知验收阶段' >&2; exit 1 ;; esac
mkdir -p artifacts/local-ci
exec > >(tee "artifacts/local-ci/$stage.log") 2>&1
phase=$stage
finish() {
  status=$?
  if [[ $phase == api ]]; then
    if (( status != 0 )); then sudo journalctl --no-pager -n 80 -u opsd.service -u opsd-executor.service -u opsd-recover.service; fi
    sudo systemctl stop opsd.service opsd-executor.service || true
  fi
  printf '%s\n' "本地验收结束：$stage，退出码 $status"
  exit "$status"
}
trap finish EXIT

build() {
  npm ci
  npm --prefix frontend/gpt2 ci
  npm run build
  git -c core.safecrlf=false diff --exit-code -- api/openapi.json backend/internal/httpapi/api.gen.go backend/internal/httpapi/policy.gen.go backend/internal/control/strict.gen.go backend/internal/cluster/api_contract.gen.go backend/internal/consoletheme/api_version.gen.go api/typescript/schema.gen.ts
  test "$(artifacts/opsd-linux-amd64 version --plain)" = "$(node -p 'require("./package.json").version')"
  sh -n artifacts/install.sh
}
shared() {
  go -C backend test -c -o ../artifacts/shared-storage.test ./internal/control
  (cd backend/internal/control && sudo env OPSD_DATABASE_TEST=1 ../../../artifacts/shared-storage.test -test.run '^TestSharedStorageNativeMigrationAndRecovery$' -test.v -test.timeout=8m)
}
backend() {
  docker pull busybox:1.37.0
  OPSD_DOCKER_TEST=1 OPSD_DATABASE_TEST=1 npm test
}
firewall() {
  go -C backend test -c -o ../artifacts/firewall-native.test ./internal/firewall
  sudo env OPSD_NATIVE_TRUST=1 artifacts/firewall-native.test -test.run '^TestPeerTrustNativeNetwork$' -test.v
}
themes() {
  npm install --prefix artifacts/theme-ui/vue-runtime --no-save --package-lock=false vue@3.5.45
  npm exec -- playwright install --with-deps chromium
  npm --prefix frontend/gpt2 run check:presentation
  OPSD_THEME_UI=1 go -C backend test ./internal/control -run '^TestConsoleThemeBrowser$' -v -count=1 -timeout=8m
}
api() {
  sudo sh scripts/check-installer.sh
  sudo systemctl stop firewalld.service nftables.service || true
  sudo ufw --force reset > /dev/null
  sudo ufw default allow incoming > /dev/null
  sudo ufw --force enable > /dev/null
  sudo systemctl stop 'opsd-operation@*.service'
  for service in opsd.service opsd-executor.service opsd-recover.service; do
    if systemctl cat "$service" > /dev/null 2>&1; then sudo systemctl disable --now "$service"; fi
  done
  sudo rm -f /etc/systemd/system/opsd.service /etc/systemd/system/opsd-executor.service /etc/systemd/system/opsd-operation@.service /etc/systemd/system/opsd-recover.service /etc/opsd/instance.json
  sudo rm -rf /var/lib/opsd/node /var/lib/opsd/executor
  sudo systemctl daemon-reload
  export OPSD_API_PASSWORD
  OPSD_API_PASSWORD=$(openssl rand -hex 24)
  sudo mkdir -p /var/lib/opsd
  sudo chmod 0700 /var/lib/opsd
  sudo env OPSD_PASSWORD="$OPSD_API_PASSWORD" artifacts/opsd-linux-amd64 install --non-interactive --address http://127.0.0.1:8444 -n api-check --username api-check --entrance /signin > artifacts/install-check.log
  test "$(stat -c %a /var/lib/opsd)" = 755
  test "$(sudo stat -c %a /var/lib/opsd/node)" = 700
  test "$(sudo stat -c %a /var/lib/opsd/executor)" = 700
  if sudo /opt/opsd/bin/opsd network --listen 127.0.0.1:8444 > artifacts/network-running.log 2>&1; then exit 1; fi
  grep -q '请先停止 opsd.service' artifacts/network-running.log
  sudo systemctl stop opsd.service opsd-executor.service
  sudo /opt/opsd/bin/opsd network --listen 127.0.0.1:8444 --address-mode manual --address http://127.0.0.1:8444
  sudo systemctl start opsd-executor.service opsd.service
  sudo /opt/opsd/bin/opsd install -n '重复安装保留账号' --address http://127.0.0.1:1
  sudo /opt/opsd/bin/opsd console --enabled=true
  sudo /opt/opsd/bin/opsd api --enabled=true
  OPSD_API_TEST=1 go -C backend test ./internal/control -run '^TestProductionBackendAPI$' -v -count=1 -timeout=8m
}

if [[ $stage == all ]]; then
  for phase in build shared backend firewall themes api; do
    printf '%s\n' "开始本地验收：$phase"
    "$phase"
  done
else
  "$stage"
fi
