import { readFileSync, writeFileSync, mkdirSync, copyFileSync } from 'node:fs';
import { join } from 'node:path';
import { root, run } from './process.mjs';

const api = JSON.parse(readFileSync(join(root, 'api/openapi.json'), 'utf8'));
const base = api.servers[0].url;
const quote = value => JSON.stringify(value ?? '');
const methods = new Set(['get', 'post', 'put', 'patch', 'delete', 'head', 'options']);
const routes = [];
for (const [path, item] of Object.entries(api.paths)) for (const [method, op] of Object.entries(item)) {
  if (!methods.has(method)) continue;
  const access = op['x-opsd-access'];
  if (!access?.mode || !op.operationId || !op.security) throw new Error(`${method} ${path} 缺少明确认证定义`);
  routes.push({ path, method: method.toUpperCase(), op, access });
}
const policy = `// Code generated from api/openapi.json; DO NOT EDIT.\npackage httpapi\n\n` +
  `const APIVersion = ${quote(api.info.version)}\nconst APIBasePath = ${quote(base)}\n\n` +
  `type APIRoute struct { Method, Path, OperationID, Mode, Module, Level, Scope, Resource, Version, Condition string; Pagination, Idempotent, Stream bool; MaxBody int64 }\n\n` +
  `var APIRoutes = []APIRoute{\n${routes.map(({ path, method, op, access: a }) =>
    `{Method:${quote(method)},Path:${quote(path)},OperationID:${quote(op.operationId)},Mode:${quote(a.mode)},Module:${quote(a.module)},Level:${quote(a.level)},Scope:${quote(a.scope)},Resource:${quote(a.resource)},Version:${quote(op['x-opsd-version'])},Condition:${quote(op['x-opsd-condition'])},Pagination:${!!op['x-opsd-pagination']},Idempotent:${!!op['x-opsd-idempotent']},Stream:${!!op['x-opsd-stream']},MaxBody:${op['x-opsd-max-body'] ?? 262144}},`).join('\n')}\n}\n\n` +
  `var OpenAPI = []byte(${quote(JSON.stringify(api))})\n`;
writeFileSync(join(root, 'backend/internal/httpapi/policy.gen.go'), policy);
writeFileSync(join(root, 'backend/internal/cluster/api_contract.gen.go'), `// Code generated from api/openapi.json; DO NOT EDIT.\npackage cluster\n\nconst APIAuthorizationVersion = ${Number(api.info.version.split('.')[0])}\n`);
writeFileSync(join(root, 'backend/internal/consoletheme/api_version.gen.go'), `// Code generated from api/openapi.json; DO NOT EDIT.\npackage consoletheme\n\nconst SupportedAPIVersion = ${Number(api.info.version.split('.')[0])}\n`);

// 将领域处理接入生成的严格接口，输出由生成的响应类型编码。
const generated = readFileSync(join(root, 'backend/internal/httpapi/api.gen.go'), 'utf8');
const standard = generated.match(/type ServerInterface interface \{([\s\S]*?)\n\}/)[1];
const visitors = new Map();
for (const match of generated.matchAll(/func \(response (\w+)\) Visit(\w+)Response\(w http\.ResponseWriter\) error \{([\s\S]*?)\n\}/g)) {
  const code = match[3].match(/w\.WriteHeader\((\d+)\)/)?.[1];
  if (!code) continue;
  if (!visitors.has(match[2])) visitors.set(match[2], []);
  visitors.get(match[2]).push({ code, type: match[1] });
}
const upper = value => value[0].toUpperCase() + value.slice(1);
let adapter = '// Code generated from api/openapi.json and api.gen.go; DO NOT EDIT.\npackage control\n\nimport ("context";"fmt";"github.com/jkfujr/opsd/internal/httpapi")\n\ntype strictAPI struct { server *Server }\nvar _ httpapi.StrictServerInterface = (*strictAPI)(nil)\n\n';
for (const match of standard.matchAll(/\n\t(\w+)\(w http.ResponseWriter, r \*http.Request([^)]*)\)/g)) {
  const name = match[1];
  const args = match[2].split(',').map(x => x.trim()).filter(Boolean).map(x => x.split(/\s+/)[0]);
  const cases = visitors.get(name) ?? [];
  const stream = routes.find(r => upper(r.op.operationId) === name)?.op['x-opsd-stream'];
  adapter += `func (a *strictAPI) ${name}(ctx context.Context, request httpapi.${name}RequestObject) (httpapi.${name}ResponseObject, error) {\n`;
  if (stream) adapter += `return nil, fmt.Errorf("流式接口由专用处理器执行")\n}\n\n`;
  else {
    adapter += `r := strictRequest(ctx)\nw := newAPIResponse(r)\na.server.${name}(w,r${args.map(x => ',request.' + upper(x)).join('')})\na.server.prepareAPIResponse(w,r)\nswitch w.status() {\n`;
    for (const c of cases) adapter += `case ${c.code}: return strictResponse[httpapi.${c.type}](w)\n`;
    adapter += `}\nreturn nil,fmt.Errorf("${name} 返回未声明状态 %d",w.status())\n}\n\n`;
  }
}
writeFileSync(join(root, 'backend/internal/control/strict.gen.go'), adapter);
mkdirSync(join(root, 'backend/internal/httpapi/static'), { recursive: true });
for (const name of ['swagger-ui-bundle.js', 'swagger-ui.css']) copyFileSync(join(root, 'node_modules/swagger-ui-dist', name), join(root, 'backend/internal/httpapi/static', name));
run('gofmt', ['-w', 'backend/internal/httpapi/policy.gen.go', 'backend/internal/control/strict.gen.go', 'backend/internal/cluster/api_contract.gen.go', 'backend/internal/consoletheme/api_version.gen.go']);
