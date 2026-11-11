# frontend-ds · OPSD 调度台

OPSD 控制台的第二套前端：同一份 `/api/v1` 契约，完全不同的视觉系统。
主题是「三联单 / 货运单据」——暖白纸面、墨色侧轨、等宽编号、签收印章。

```powershell
npm install
npm run dev          # http://127.0.0.1:5290，默认使用内置演示数据
npm run typecheck
npm run build        # 产物在 dist/，相对路径引用，可直接交给节点程序托管
```

## 两种数据来源

| 命令 | 数据来源 | 用途 |
| --- | --- | --- |
| `npm run dev` | `src/dev/mock-api.ts` 挂在 Vite 中间件上 | 离线预览、评审、截图 |
| `$env:OPSD_API='http://127.0.0.1:65533'; npm run dev` | 反向代理到真实节点 | 对着真机调试 |

生产构建不包含演示数据；`dist/` 里只有静态资源。

## 结构

```
src/
  api/          openapi-fetch 客户端 + 由 api/openapi.json 生成的类型
  components/   Icon、ui 原子件、Shell 外壳、Operations 操作队列
  dev/          演示数据（仅开发环境）
  lib/         格式化、状态词表、导航顺序
  pages/        概览、节点、容器、数据库、防火墙、存储、设置
  styles/       base（纸面色）/ shell（外壳）/ parts（零件）/ pages（版式）
scripts/        截图脚本（shoot / shoot-dark / shoot-flows，使用本机 Edge）
```

## 约定

- `src/api/schema.gen.ts` 保留原演示契约的生成类型，不要手改。
- 写请求由 `api/client.ts` 统一补 `X-CSRF-Token` 与 `Idempotency-Key`；需要幂等键的接口在类型上强制传入。
- 颜色只承担语义：朱红 = 待签收 / 危险，琥珀 = 进行中，印绿 = 已签收，碳蓝 = 信息与链接。
- 新增页面先读 `DESIGN.md`（本目录），复用现有的 sheet / docket / stamp / table 语汇，不要新造卡片样式。

## 与旧前端的关系

默认内置前端为 `frontend/gpt2`，本方案保留独立开发和构建入口。目录及根命令见 [前端开发](../../README.md)；本目录 `npm run build` 仍生成相对路径静态资源，不改变默认内置选择。
