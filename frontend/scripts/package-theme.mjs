// 生成 OPSD 主题包：根目录只含 theme.json 与 dist/。
import { readFileSync, readdirSync, statSync, writeFileSync, mkdirSync } from 'node:fs';
import { join, relative, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { zipSync } from 'fflate';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const dist = join(root, 'dist');
const entries = { 'theme.json': readFileSync(join(root, 'theme.json')) };
const walk = (dir) => {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path);
    else entries['dist/' + relative(dist, path).split('\\').join('/')] = readFileSync(path);
  }
};
walk(dist);
// 打包的 Inter 与 JetBrains Mono 均为 SIL OFL 1.1,分发 ZIP 必须随附许可证文本。
for (const [name, file] of [['OFL-Inter.txt', '@fontsource-variable/inter/LICENSE'], ['OFL-JetBrainsMono.txt', '@fontsource-variable/jetbrains-mono/LICENSE']]) {
  const license = join(root, 'node_modules', file);
  try {
    entries['dist/fonts/' + name] = readFileSync(license);
  } catch {
    throw new Error(`缺少字体许可证 ${license};独立主题 ZIP 不可分发`);
  }
}
const out = join(root, '..', 'artifacts', 'frontend');
mkdirSync(out, { recursive: true });
const file = join(out, 'opsd-theme.zip');
writeFileSync(file, zipSync(entries, { level: 9 }));
console.log(`主题包：${file}（${Object.keys(entries).length} 个文件）`);
