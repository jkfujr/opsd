import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { root, run, npm } from './process.mjs';

mkdirSync(join(root, 'backend/internal/httpapi'), { recursive: true });
mkdirSync(join(root, 'api/typescript'), { recursive: true });
run('go', ['-C', 'backend', 'tool', 'oapi-codegen', '--config', '../api/oapi-codegen.yaml', '../api/openapi.json']);
npm(['exec', '--', 'openapi-typescript', 'api/openapi.json', '-o', 'api/typescript/schema.gen.ts']);
await import('./generate-api-runtime.mjs');
