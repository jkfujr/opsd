import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, sep } from 'node:path';
import { root, run, npm } from './process.mjs';

npm(['run', 'generate']);
npm(['--prefix', 'frontend', 'run', 'build']);
const { version } = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
const source = join(root, 'frontend/dist');
writeFileSync(join(source, 'version.txt'), version + '\n');
// 每次 Vite 构建已清理输出目录；Go 直接嵌入该目录，避免维护第二份资源副本。
mkdirSync(join(root, 'dist'), { recursive: true });
for (const arch of ['amd64', 'arm64']) {
  const output = join(root, 'dist', `linux-${arch}`);
  mkdirSync(output, { recursive: true });
  run('go', ['build', '-trimpath', '-buildvcs=false', '-o', output + sep, './cmd/...', './internal/...', './frontend'], {
    CGO_ENABLED: '0', GOOS: 'linux', GOARCH: arch,
  });
}
