import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { root, run, npm } from './process.mjs';

npm(['run', 'generate']);
npm(['--prefix', 'frontend', 'run', 'build']);
const { version } = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
const source = join(root, 'frontend/dist');
writeFileSync(join(source, 'version.txt'), version + '\n');
// 每次 Vite 构建已清理输出目录；Go 直接嵌入该目录，避免维护第二份资源副本。
mkdirSync(join(root, 'artifacts'), { recursive: true });
writeFileSync(join(root, 'artifacts/install.sh'), readFileSync(join(root, 'scripts/install.sh'), 'utf8').replaceAll('@OPSD_VERSION@', version));
for (const arch of ['amd64', 'arm64']) {
  const output = join(root, 'artifacts', `opsd-linux-${arch}`);
  run('go', ['build', '-trimpath', '-buildvcs=false', '-ldflags', `-s -w -X github.com/jkfujr/opsd/internal/buildinfo.Version=${version}`, '-o', output, './cmd/opsd'], {
    CGO_ENABLED: '0', GOOS: 'linux', GOARCH: arch,
  });
}
