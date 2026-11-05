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

2026-11-05；提交：本批待提交；环境：Windows 开发工作区，验证渠道为 GitHub Actions；覆盖项：包含本机的互信、普通 IP 自动转交、防火墙布局及相关恢复。

已验证：OpenAPI 接口生成完成。未验证：本批 CI 的构建、后端、原生流量与浏览器交互；本批代码尚未发布及更新 C091。此前结果已移至 CHANGELOG。
