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

2026-11-05；提交：`4730ef08bf06f9878e5eed34e1f6451d602be189`；环境：GitHub Ubuntu 24.04、Go 1.26.2、Node.js 24.14.1、Chromium，CI 临时主机启用 UFW；覆盖项：构建、后端、原生互信、普通 IP 转交及控制台。

已验证：接口生成一致、前端类型与生产构建、Linux amd64／arm64 编译、后端测试；单节点双栈来源与本机关联、共享 IP 去重及撤销、地址冲突与失败保留旧值；迁移严格限定精确 IP 的全入站允许规则，受限规则保留，额外配置差异、过期互信、原生冲突及普通规则外部修改被拒绝；应答丢失查询原操作、同输入失败停止自动重试、显式重试、执行中断恢复、SQLite 重开后保留确认配置；UFW／nftables 的 IPv4／IPv6 新连接放行、其他来源拦截、重复应用与撤除，nftables 持久重载及 firewalld 永久配置撤除；实际安装程序中的普通 IP 自动转交、网段与端口规则保留、关闭后互信撤除且原条目保持移除；9 页深浅主题及 320／768／1440px、防火墙 1920px、40px 控件对齐、页签及表格间距、菜单键盘操作、地址复制降级、迁移状态、浮层及未提交修改。已人工检查 CI 截图中的深浅宽屏、窄屏、编辑浮层及迁移失败入口。

未验证：arm64 实机、firewalld 运行状态的新连接、跨地域实网、整机重启、浏览器原生缩放及慢加载／故障模拟；本批尚未发布及更新 C091。

- [CI 42002800295](https://github.com/jkfujr/opsd/actions/runs/42002800295)
- 界面工件：该运行的 `ui-check`，本次下载到 `artifacts/ui-42002800295/`。

本次文档更新复用上述成功结果，程序与验收脚本输入保持一致。
