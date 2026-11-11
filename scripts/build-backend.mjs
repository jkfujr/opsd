import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { root, run } from './process.mjs';

const { version } = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
const source = join(root, 'backend/internal/webassets/artifacts');
if (!existsSync(join(source, 'index.html')) || !existsSync(join(source, 'version.txt')) || readFileSync(join(source, 'version.txt'), 'utf8').trim() !== version) {
  throw new Error('请先运行 npm run build:frontend，准备当前版本的内置资源');
}
mkdirSync(join(root, 'artifacts'), { recursive: true });
writeFileSync(join(root, 'artifacts/install.sh'), readFileSync(join(root, 'scripts/install.sh'), 'utf8').replaceAll('@OPSD_VERSION@', version));
for (const arch of ['amd64', 'arm64']) {
  const output = join(root, 'artifacts', `opsd-linux-${arch}`);
  run('go', ['-C', 'backend', 'build', '-trimpath', '-buildvcs=false', '-ldflags', `-s -w -X github.com/jkfujr/opsd/internal/buildinfo.Version=${version}`, '-o', output, './cmd/opsd'], {
    CGO_ENABLED: '0', GOOS: 'linux', GOARCH: arch,
  });
}
