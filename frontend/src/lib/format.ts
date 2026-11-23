const nf1 = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 });
const nf0 = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 0 });
const rtf = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' });
const dtf = new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
const dtfShort = new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
const dtfFull = new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });

export const n1 = (v: number) => nf1.format(v);
export const n0 = (v: number) => nf0.format(v);

export function bytes(value?: number | null, digits = 1) {
  if (value === undefined || value === null || !Number.isFinite(value)) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let v = Math.abs(value), i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${(value < 0 ? -v : v).toFixed(i === 0 ? 0 : v >= 100 ? 0 : digits)} ${units[i]}`;
}

export function rate(value?: number | null) {
  if (value === undefined || value === null || !Number.isFinite(value)) return '—';
  return `${bytes(value)}/s`;
}

export function pct(value?: number | null, digits = 1) {
  if (value === undefined || value === null || !Number.isFinite(value)) return '—';
  return `${value.toFixed(value >= 99.95 || value === 0 ? 0 : digits)}%`;
}

export function ratio(used?: number | null, total?: number | null) {
  if (!used && used !== 0) return undefined;
  if (!total) return undefined;
  return Math.max(0, Math.min(100, (used / total) * 100));
}

export function ago(iso?: string | null, now = Date.now()) {
  if (!iso) return '—';
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '—';
  const s = Math.round((t - now) / 1000);
  const abs = Math.abs(s);
  if (abs < 5) return '刚刚';
  if (abs < 60) return rtf.format(s, 'second');
  if (abs < 3600) return rtf.format(Math.round(s / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(s / 3600), 'hour');
  if (abs < 86400 * 30) return rtf.format(Math.round(s / 86400), 'day');
  return dtfFull.format(t);
}

export function time(iso?: string | null) {
  if (!iso) return '—';
  const t = Date.parse(iso);
  return Number.isFinite(t) ? dtf.format(t) : '—';
}
export function clock(ms: number) { return dtfShort.format(ms); }
export function fullTime(iso?: string | null) {
  if (!iso) return '—';
  const t = Date.parse(iso);
  return Number.isFinite(t) ? dtfFull.format(t) : '—';
}

export function duration(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 60) return `${Math.round(seconds)} 秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${Math.round(seconds % 60)} 秒`;
  return `${Math.floor(seconds / 3600)} 小时 ${Math.round((seconds % 3600) / 60)} 分`;
}

export function shortId(id?: string, size = 12) {
  if (!id) return '—';
  return id.length > size ? id.slice(0, size) : id;
}

export function initials(name: string) {
  const t = name.trim();
  if (!t) return '?';
  const cjk = t.match(/[\u4e00-\u9fff]/g);
  if (cjk && cjk.length) return cjk.slice(0, 1).join('');
  return t.split(/[\s_\-.]+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join('');
}

export function decodeBase64(data: string) {
  try {
    const bin = atob(data);
    const bytes_ = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes_[i] = bin.charCodeAt(i);
    return bytes_;
  } catch {
    return new Uint8Array();
  }
}

export function encodeBase64(text: string | Uint8Array) {
  const data = typeof text === 'string' ? new TextEncoder().encode(text) : text;
  let bin = '';
  for (let i = 0; i < data.length; i += 0x8000) bin += String.fromCharCode(...data.subarray(i, i + 0x8000));
  return btoa(bin);
}

/** 去掉常见 ANSI 控制序列，日志视图保留纯文本。 */
export function stripAnsi(text: string) {
  // eslint-disable-next-line no-control-regex
  return text.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '').replace(/\x1b\][^\x07]*(\x07|\x1b\\)/g, '').replace(/\r(?!\n)/g, '\n');
}

export function plural(count: number, unit: string) { return `${n0(count)} ${unit}`; }

export async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const area = document.createElement('textarea');
    area.value = text;
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand('copy');
    area.remove();
    return ok;
  }
}

/** 简单模糊匹配：返回得分与命中位置，连续命中和词首加分。 */
export function fuzzy(query: string, text: string): { score: number; hits: number[] } | null {
  const q = query.trim().toLowerCase();
  if (!q) return { score: 0, hits: [] };
  const t = text.toLowerCase();
  const direct = t.indexOf(q);
  if (direct >= 0) return { score: 100 - direct + (direct === 0 ? 40 : 0), hits: Array.from({ length: q.length }, (_, i) => direct + i) };
  let ti = 0, score = 0, streak = 0;
  const hits: number[] = [];
  for (const ch of q) {
    if (ch === ' ') continue;
    const found = t.indexOf(ch, ti);
    if (found < 0) return null;
    streak = found === ti ? streak + 1 : 0;
    score += 1 + streak * 2 + (found === 0 || /[\s/_\-.]/.test(t[found - 1] ?? '') ? 3 : 0);
    hits.push(found);
    ti = found + 1;
  }
  return { score, hits };
}
