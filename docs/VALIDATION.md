# 验收

## 命令与渠道

本批使用 GitHub Actions CI，从仓库根目录运行：

```bash
npm ci
npm --prefix frontend ci
npm run build
npm test
```

构建包含接口生成、前端类型检查与生产构建、Linux amd64／arm64 编译。CI 校对生成文件、版本和安装脚本语法，并在隔离网络空间执行原生互信规则的新连接验收。

```bash
go test -c -o artifacts/firewall-native.test ./internal/firewall
sudo env OPSD_NATIVE_TRUST=1 artifacts/firewall-native.test -test.run '^TestPeerTrustNativeNetwork$' -test.v
```

Release 复用同提交成功工件，通过公开地址下载，检查实际安装、两个 systemd 服务、版本与重复安装保留配置。

## 最近结果

2026-11-04；提交：发布前工作树；环境：Windows 编辑、GitHub Linux CI 待运行；覆盖项：接口生成与源码变更。

已验证：OpenAPI 生成。未验证：本批 CI、原生流量、公开安装与浏览器交互；C091 未部署本批代码。
