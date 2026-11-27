# 验收

## 命令与渠道

本批先在本机独立 WSL 2 发行版 `OPSD-CI` 完成验收，通过后再提交。发行版安装在 `F:\VM\WSL\OPSD-CI`，系统为 Ubuntu 24.04；启用 systemd，安装 Go 1.26.2、Node.js 24.14.1、Docker、Compose、nftables、iptables、firewalld、UFW 与中文字体，`ci` 用户可使用 Docker 和免密 sudo。Go 下载使用 `https://goproxy.cn,direct`，Docker Hub 使用 `https://docker.m.daocloud.io` 镜像。GitHub Actions 的 push、PR 和手动触发保持原值。

从仓库根目录执行：

```powershell
pwsh -NoProfile -File scripts/check-wsl.ps1
```

入口同步当前工作区到 WSL 的 `/home/ci/opsd`，在 Linux 文件系统构建，保留依赖缓存；产物和日志复制到本机被忽略的 `artifacts/wsl-ci/`。安装验收会修改独立发行版内的 OPSD 服务与防火墙，每次重建验收节点及执行器数据，脚本校验发行版名称。需要重跑失败阶段时使用 `-Stage build|shared|backend|monitoring|firewall|themes|api`；成功且输入未变的阶段直接复用。

本地与 GitHub Actions 使用相同的项目构建及验收命令，Linux 手动构建入口为：

```bash
npm ci
npm --prefix frontend ci
npm run build
OPSD_DOCKER_TEST=1 OPSD_DATABASE_TEST=1 npm test
```

构建包含接口生成、前端类型检查与生产构建、Linux amd64／arm64 编译。CI 使用 busybox:1.37.0 验证真实容器启停、重启与 TTY／非 TTY 日志，校对生成类型、严格适配、授权与能力定义、版本和安装脚本语法，在隔离网络空间执行原生互信规则的新连接验收，再安装生产程序，通过 v2 HTTP／WebSocket API 检查任务及 Compose 的 Bearer 授权、任务执行和取消、日志分页、容器更新、Compose 操作及 root 终端释放；结果输出为 `api-check` 工件。

数据库检查启动三成员 MariaDB Galera、两个 ProxySQL、Prometheus 与 Grafana，覆盖业务库、账号、代理实际登录、权限、部分恢复、受管排序、活动事务、故障切换、重启及多写冲突，结果输出为 `database-check` 工件。固定部署模板另覆盖单成员创建、扩为三成员、TLS 全量同步、已有数据保留、首成员重启入群及代理部署，结果为 `database-deployment-result.json`。四逻辑 OPSD 节点在同一 CI 主机运行，其中一台仅管理代理；Docker 缺失安装、已有安装和安装失败使用命令替身；协调节点及执行器恢复使用持久数据库重新打开检查。

共享迁移专项另启动三个运行节点，复用两个本机代理并自动安装第三个代理；新成员单独检查引导节点停止后的接入资料获取。覆盖迁移、共享策略、地址补传、原请求与步骤复用、协调接管、代理恢复和账号撤销，结果输出为 `shared-storage-result.json`。Linux 手动复现从仓库根目录执行：

```bash
go -C backend test -c -o ../artifacts/shared-storage.test ./internal/control
(cd backend/internal/control && sudo env OPSD_DATABASE_TEST=1 ../../../artifacts/shared-storage.test -test.run '^TestSharedStorageNativeMigrationAndRecovery$' -test.v -test.timeout=8m)
```

```bash
go -C backend test -c -o ../artifacts/firewall-native.test ./internal/firewall
sudo env OPSD_NATIVE_TRUST=1 artifacts/firewall-native.test -test.run '^TestPeerTrustNativeNetwork$' -test.v
```

Release 复用同提交成功工件，通过公开地址下载，检查实际安装、两个 systemd 服务、版本与重复安装保留配置。

节点存储根验收在 `OPSD-CI` 的 Linux 仓库根目录执行。用例创建两块独立 ext4 loop 盘和专用 systemd 单元。MinIO 参数指向监控验收已准备的本机工件。

```bash
mkdir -p artifacts/opsd-i3
go -C backend build -o ../artifacts/opsd-i3/opsd ./cmd/opsd
go -C backend test -c -o ../artifacts/opsd-i3/cli.test ./internal/cli
go -C backend test -c -o ../artifacts/opsd-i3/storageroot.test ./internal/storageroot
opsd_fixture_exec=$(mktemp -d /var/tmp/opsd-i3-bin-XXXXXXXX)
trap 'rm -f "$opsd_fixture_exec/opsd"; rmdir "$opsd_fixture_exec"' EXIT
chmod 755 "$opsd_fixture_exec"
install -m 755 artifacts/opsd-i3/opsd "$opsd_fixture_exec/opsd"
sudo env OPSD_TEST_STORAGE_ROOT=1 OPSD_TEST_BINARY="$opsd_fixture_exec/opsd" \
  artifacts/opsd-i3/storageroot.test -test.run '^TestCompletedMetadataRollbackOnSystemd$' -test.v -test.timeout=3m
sudo env OPSD_TEST_STORAGE_ROOT=1 OPSD_TEST_BINARY="$opsd_fixture_exec/opsd" \
  OPSD_TEST_MINIO="$PWD/artifacts/monitoring-native/bin/minio" \
  artifacts/opsd-i3/cli.test -test.run '^TestStorageRootDualDisk$' -test.v -test.timeout=8m
```

集群 S3 池验收在独立的 `OPSD-CI` 运行，使用三个逻辑成员、三个模拟故障域、固定镜像、原生 loop 挂载和真实 systemd。五类容器以 `65534:65534` 运行。先启动 PolicyKit，使其挂载命名空间在测试盘挂载之前建立。

```bash
set -o pipefail
mkdir -p artifacts/opsd-i5
go -C backend build -o ../artifacts/opsd-i5/opsd ./cmd/opsd
go -C backend test -c -o ../artifacts/opsd-i5/api.test ./internal/control
sudo systemctl start polkit.service
sudo env OPSD_TEST_STORAGE_POOL=1 \
  artifacts/opsd-i5/api.test -test.run '^TestStoragePoolSharedDiskCapacityNative$' -test.v -test.timeout=3m \
  2>&1 | tee artifacts/opsd-i5/capacity.log
sudo env OPSD_TEST_STORAGE_POOL=1 OPSD_TEST_STORAGE_POOL_SOAK=2h \
  OPSD_TEST_BINARY="$PWD/artifacts/opsd-i5/opsd" \
  artifacts/opsd-i5/api.test -test.run '^TestStoragePoolNative$' -test.v -test.timeout=2h20m \
  2>&1 | tee artifacts/opsd-i5/native.log
```

EC 验收在 OPSD-CI 使用四个独立故障域，每域有独立 ext4 数据挂载、Volume 进程和私网地址。先构建并加载[固定衍生镜像](../tools/seaweedfs-opsd/README.md)，再从 Linux 仓库根目录执行。

```bash
set -o pipefail
mkdir -p artifacts/opsd-i6
go -C backend build -o ../artifacts/opsd-i6/opsd ./cmd/opsd
go -C backend test -c -o ../artifacts/opsd-i6/control.test ./internal/control
sudo systemctl start polkit.service
sudo env OPSD_TEST_STORAGE_POOL_EC=1 OPSD_TEST_STORAGE_POOL_EC_TRANSITION=1 \
  OPSD_TEST_STORAGE_POOL_EC_REPAIR=1 OPSD_TEST_BINARY="$PWD/artifacts/opsd-i6/opsd" \
  artifacts/opsd-i6/control.test -test.run '^TestStoragePoolECConservativeNative$' -test.v -test.timeout=15m \
  2>&1 | tee artifacts/opsd-i6/transition.log
sudo env OPSD_TEST_STORAGE_POOL_EC_LIFECYCLE=1 OPSD_TEST_STORAGE_POOL_EC_REALTIME=1 \
  OPSD_TEST_STORAGE_POOL_EC_REPAIR=1 OPSD_TEST_BINARY="$PWD/artifacts/opsd-i6/opsd" \
  artifacts/opsd-i6/control.test -test.run '^TestStoragePoolECLifecycleNative$' -test.v -test.timeout=90m \
  2>&1 | tee artifacts/opsd-i6/lifecycle.log
```

主题检查沿用 Go 测试和 Playwright，CI 的“整套主题与内置恢复界面”步骤构建 React、Vue 成品包，在内置生产资源上检查安装、连续条件切换、双登录模式、深层路由、未知应答后的读取和损坏主题恢复，同时验证 Swagger 会话／Bearer、CSRF、条件写入、原键查询与根路径指标。另在独立主题、默认内置与内置恢复三模式执行受影响状态、日志、受管路由和防火墙接纳检查，截图随 `theme-check` 工件保存。手动复现从仓库根目录运行：

```powershell
npm install --prefix artifacts/theme-ui/vue-runtime --no-save --package-lock=false vue@3.5.45
# 使用已安装的 Edge；CI 使用 Playwright Chromium。
$env:OPSD_THEME_UI='1'
$env:OPSD_THEME_BROWSER='msedge'
go -C backend test ./internal/control -run '^TestConsoleThemeBrowser$' -v -count=1 -timeout=8m
```

## 最近结果

2026-11-27，基于 `a311e75` 的 I6 改动在独立 WSL 2 `OPSD-CI` 验证。固定衍生镜像的两次无缓存 linux/amd64 构建，manifest、config 和程序 SHA-256 完全一致。最终镜像专项用例用时 116.76 秒。每份源卷 1,048,597,608 字节，覆盖 100 个 10 MiB 对象。真实时钟轮次经过 64 次阈值前检查，最后写入后 1 小时 8.884 秒进入编码。该轮次使用槽位修订前的镜像。补齐容量拒绝后的真实大小 HTTP 用例用时 299.61 秒。首轮后端回归的 Galera 启动和部署等待用例失败，两项单独复跑均通过。逐项读回见 `artifacts/opsd-v4/final-review.md`、`artifacts/opsd-v4/I6/` 和 `artifacts/opsd-v4/I7/`。

已验证：四个逻辑故障域、14 个唯一分片、单域失联、缺片与错代际拒绝、同代际修复、跨入口版本和原回执、删源前后读取、转换期间新写入、`cleanup_pending`、容量不足时拒绝并保留原分片、容量恢复后反向转换。未验证：独立物理宿主联合验收、生产池和 arm64 衍生镜像。

历史结果见 [变更记录](../CHANGELOG.md)。
