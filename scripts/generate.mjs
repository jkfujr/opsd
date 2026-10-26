import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { root, run, npm } from './process.mjs';

mkdirSync(join(root, 'internal/httpapi'), { recursive: true });
mkdirSync(join(root, 'frontend/src/api'), { recursive: true });
run('go', ['tool', 'oapi-codegen', '--config', 'api/oapi-codegen.yaml', 'api/openapi.json']);
npm(['exec', '--', 'openapi-typescript', 'api/openapi.json', '-o', 'frontend/src/api/schema.gen.ts']);
