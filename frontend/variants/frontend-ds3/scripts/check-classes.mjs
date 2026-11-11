// 检查页面用到的类名是否都有样式定义。
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const css = ['base', 'shell', 'parts', 'pages'].map((name) => readFileSync(`src/styles/${name}.css`, 'utf8')).join('\n');
const selectors = new Set([...css.matchAll(/\.([a-z][a-z0-9_-]*)/g)].map((match) => match[1]));

const helpers = new Set([
  'stack', 'row', 'spread', 'wrap', 'grow', 'shrink-0', 'num', 'mono', 'muted', 'soft', 'nowrap', 'truncate',
  'sr-only', 'kicker', 'right', 'tight', 'table__name', 'table__serial', 'cell-sub', 'sub', 'in', 'out',
  'label', 'idx', 'count', 't0', 't12', 't24', 'dot',
]);

const used = new Set();
function collect(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) collect(path);
    else if (name.endsWith('.tsx')) {
      const source = readFileSync(path, 'utf8');
      for (const match of source.matchAll(/className="([^"]+)"/g)) {
        match[1].split(/\s+/).forEach((cls) => {
          if (cls && !cls.startsWith('gap-')) used.add(cls);
        });
      }
      for (const match of source.matchAll(/className=\{`([^`]*)`\}/g)) {
        match[1].split(/\s+/).forEach((cls) => {
          if (cls && !cls.includes('$') && !cls.startsWith('gap-')) used.add(cls.trim());
        });
      }
    }
  }
}
collect('src/pages');

const missing = [...used]
  .filter((cls) => !selectors.has(cls) && !helpers.has(cls) && !cls.startsWith('gap-'))
  .sort();
console.log(missing.length ? 'MISSING:\n' + missing.join('\n') : 'none');
