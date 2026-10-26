import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

export const root = fileURLToPath(new URL('../', import.meta.url));

export function run(command, args, env = {}) {
  const result = spawnSync(command, args, {
    cwd: root,
    env: { ...process.env, ...env },
    stdio: 'inherit',
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`命令执行失败：${command} ${args.join(' ')}`);
}

export function npm(args) {
  const cli = process.env.npm_execpath;
  if (!cli) throw new Error('请使用 npm run 执行构建命令');
  run(process.execPath, [cli, ...args]);
}
