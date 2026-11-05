# 验收

## 命令与渠道

本批使用 GitHub Actions CI，从仓库根目录运行：

```bash
npm ci
npm --prefix frontend ci
npm run build
npm test
```

构建包含接口生成、前端类型检查与生产构建、Linux amd64／arm64 编译。CI 校对生成文件、版本和安装脚本语法，在隔离网络空间执行原生互信规则的新连接验收，再安装生产程序，通过 Chromium 检查页面与交互；截图输出为 `ui-check` 工件。

```bash
go test -c -o artifacts/firewall-native.test ./internal/firewall
sudo env OPSD_NATIVE_TRUST=1 artifacts/firewall-native.test -test.run '^TestPeerTrustNativeNetwork$' -test.v
```

Release 复用同提交成功工件，通过公开地址下载，检查实际安装、两个 systemd 服务、版本与重复安装保留配置。

## 最近结果

2026-11-05；提交：`6bdee7cd05740b29aa3b571b412028ab292a7efc`；环境：GitHub Ubuntu 24.04、Go 1.26.2、Node.js 24.14.1、Chromium；覆盖项：构建、后端、原生互信、安装及控制台。

已验证：接口生成一致、前端类型与生产构建、Linux amd64／arm64 编译、后端测试；UFW／nftables 的 IPv4／IPv6 新连接放行、其他来源拦截、重复应用与撤除，nftables 持久配置重载，firewalld 永久配置及撤除；下载中断与版本错误在安装前停止；9 个业务页面、深浅主题、320／768／1440px、CSS 200% 缩放、浮层、未提交修改、互信开关保存、命令生成及 Clipboard API 缺失时复制；公开下载、amd64 systemd 安装、版本与重复安装保留名称和控制台状态。未验证：arm64 实机、firewalld 运行状态的新连接、跨地域实网与整机重启、浏览器原生缩放及慢加载／故障模拟；C091 未部署本批代码。

- [CI 41984265465](https://github.com/jkfujr/opsd/actions/runs/41984265465)
- [发布与公开安装 41984539506](https://github.com/jkfujr/opsd/actions/runs/41984539506)
- [v0.1.0-dev](https://github.com/jkfujr/opsd/releases/tag/v0.1.0-dev)

本文档更新复用上述成功结果，程序与验收脚本输入保持一致。
