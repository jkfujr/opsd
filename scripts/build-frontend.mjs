import { copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { root, npm } from './process.mjs';

const output = join(root, 'backend/internal/webassets/artifacts');
// 内置资源直接生成到 Go 包内；独立主题构建继续使用前端自己的相对路径和 dist。
npm(['--prefix', 'frontend', 'run', 'check', '--', '--base', '/', '--outDir', output, '--emptyOutDir']);
const { version } = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
writeFileSync(join(output, 'version.txt'), version + '\n');
mkdirSync(join(output, 'fonts'), { recursive: true });
// 打包字体均为 SIL OFL 1.1，随资源附带许可证。
for (const [name, file] of [['OFL-Inter.txt', '@fontsource-variable/inter/LICENSE'], ['OFL-JetBrainsMono.txt', '@fontsource-variable/jetbrains-mono/LICENSE']]) {
  const source = join(root, 'frontend/node_modules', file);
  if (existsSync(source)) copyFileSync(source, join(output, 'fonts', name));
}
