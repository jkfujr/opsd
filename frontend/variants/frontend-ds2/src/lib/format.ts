const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];

export function bytes(input?: number): string {
  if (input === undefined || Number.isNaN(input)) return '—';
  const index = input > 0 ? Math.min(Math.floor(Math.log(input) / Math.log(1024)), units.length - 1) : 0;
  const scaled = input / 1024 ** index;
  const digits = index === 0 ? 0 : scaled < 10 ? 1 : 0;
  return `${scaled.toLocaleString('zh-CN', { minimumFractionDigits: digits, maximumFractionDigits: digits })} ${units[index]}`;
}

export function speed(input?: number): string {
  return input === undefined ? '—' : `${bytes(input)}/s`;
}

export function percent(input?: number, digits = 1): string {
  return input === undefined || Number.isNaN(input) ? '—' : `${input.toFixed(digits)}%`;
}

export function ratio(used?: number, total?: number): number | undefined {
  return used !== undefined && total ? (used / total) * 100 : undefined;
}

export function level(input?: number): 'ok' | 'warn' | 'bad' {
  if (input === undefined) return 'ok';
  if (input >= 90) return 'bad';
  if (input >= 75) return 'warn';
  return 'ok';
}

const pad = (n: number) => String(n).padStart(2, '0');

export function clock(input?: string | Date): string {
  if (!input) return '—';
  const d = typeof input === 'string' ? new Date(input) : input;
  if (Number.isNaN(d.getTime())) return '—';
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function hm(input?: string | Date): string {
  if (!input) return '—';
  const d = typeof input === 'string' ? new Date(input) : input;
  if (Number.isNaN(d.getTime())) return '—';
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function stamp(input?: string | Date): string {
  if (!input) return '—';
  const d = typeof input === 'string' ? new Date(input) : input;
  if (Number.isNaN(d.getTime())) return '—';
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function day(input?: string | Date): string {
  if (!input) return '—';
  const d = typeof input === 'string' ? new Date(input) : input;
  if (Number.isNaN(d.getTime())) return '—';
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** 相对时间：刚刚 / 42 秒前 / 3 分钟前 */
export function ago(input?: string | Date): string {
  if (!input) return '—';
  const d = typeof input === 'string' ? new Date(input) : input;
  const seconds = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
  if (seconds < 5) return '刚刚';
  if (seconds < 60) return `${seconds} 秒前`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return `${Math.floor(seconds / 86400)} 天前`;
}

/** 单据编号：由节点 ID 与操作 ID 派生，稳定、可读、不虚构后端字段。 */
export function serial(...parts: string[]): string {
  const raw = parts.filter(Boolean).join(':');
  let hash = 2166136261;
  for (let i = 0; i < raw.length; i += 1) {
    hash ^= raw.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  const hex = (hash >>> 0).toString(16).toUpperCase().padStart(8, '0');
  return `${hex.slice(0, 4)}-${hex.slice(4)}`;
}

export function sequence(index: number): string {
  return String(index + 1).padStart(2, '0');
}

export function duration(seconds?: number): string {
  if (seconds === undefined || seconds < 0) return '—';
  if (seconds < 60) return `${Math.ceil(seconds)} 秒`;
  return `${Math.floor(seconds / 60)} 分 ${Math.floor(seconds % 60)} 秒`;
}
