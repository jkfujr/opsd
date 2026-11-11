import { copyFileSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { root, npm } from './process.mjs';

const output = join(root, 'backend/internal/webassets/artifacts');
// 内置资源直接生成到 Go 包内；独立主题构建继续使用前端自己的相对路径和 dist。
npm(['--prefix', 'frontend/gpt2', 'run', 'build', '--', '--base', '/', '--outDir', output, '--emptyOutDir']);
const { version } = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
writeFileSync(join(output, 'version.txt'), version + '\n');
mkdirSync(join(output, 'fonts'), { recursive: true });
for (const name of readdirSync(join(root, 'frontend/gpt2/public/fonts'))) {
  if (/^(OFL-.*\.txt|README\.md)$/.test(name)) copyFileSync(join(root, 'frontend/gpt2/public/fonts', name), join(output, 'fonts', name));
}
