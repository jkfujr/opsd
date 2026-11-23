import { useId, useMemo, useState, type PointerEvent, type ReactNode } from 'react';
import { motion } from 'motion/react';
import { useSize } from '../lib/hooks';

/** 单调三次插值：曲线平滑且不过冲。 */
function smoothPath(points: [number, number][]) {
  if (points.length === 0) return '';
  if (points.length === 1) return `M${points[0]![0]},${points[0]![1]}`;
  const n = points.length;
  const dx: number[] = [], dy: number[] = [], m: number[] = [];
  for (let i = 0; i < n - 1; i++) {
    dx.push(points[i + 1]![0] - points[i]![0]);
    dy.push(points[i + 1]![1] - points[i]![1]);
    m.push(dy[i]! / (dx[i]! || 1));
  }
  const t: number[] = [m[0]!];
  for (let i = 1; i < n - 1; i++) t.push(m[i - 1]! * m[i]! <= 0 ? 0 : (m[i - 1]! + m[i]!) / 2);
  t.push(m[n - 2]!);
  for (let i = 0; i < n - 1; i++) {
    if (m[i] === 0) { t[i] = 0; t[i + 1] = 0; continue; }
    const a = t[i]! / m[i]!, b = t[i + 1]! / m[i]!;
    const h = a * a + b * b;
    if (h > 9) { const k = 3 / Math.sqrt(h); t[i] = k * a * m[i]!; t[i + 1] = k * b * m[i]!; }
  }
  let d = `M${points[0]![0].toFixed(2)},${points[0]![1].toFixed(2)}`;
  for (let i = 0; i < n - 1; i++) {
    const [x0, y0] = points[i]!, [x1, y1] = points[i + 1]!;
    const h = dx[i]! / 3;
    d += `C${(x0 + h).toFixed(2)},${(y0 + t[i]! * h).toFixed(2)} ${(x1 - h).toFixed(2)},${(y1 - t[i + 1]! * h).toFixed(2)} ${x1.toFixed(2)},${y1.toFixed(2)}`;
  }
  return d;
}

/** 把含缺口的序列拆成连续片段；缺口不连线、不补零。 */
function segments(values: (number | null | undefined)[], x: (i: number) => number, y: (v: number) => number) {
  const out: [number, number][][] = [];
  let cur: [number, number][] = [];
  values.forEach((v, i) => {
    if (v === null || v === undefined || !Number.isFinite(v)) { if (cur.length) out.push(cur); cur = []; return; }
    cur.push([x(i), y(v)]);
  });
  if (cur.length) out.push(cur);
  return out;
}

export function Spark({ values, max, min = 0, height = 36, tone = 'var(--accent)', fill = true, strokeWidth = 1.6 }: { values: (number | null | undefined)[]; max?: number; min?: number; height?: number; tone?: string; fill?: boolean; strokeWidth?: number }) {
  const id = useId();
  const [ref, size] = useSize<HTMLDivElement>();
  const w = Math.max(10, size.width);
  const h = height;
  const finite = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v));
  const top = max ?? Math.max(1, ...finite) * 1.15;
  const span = Math.max(1e-9, top - min);
  const step = values.length > 1 ? w / (values.length - 1) : w;
  const segs = segments(values, (i) => i * step, (v) => h - 2 - ((Math.min(top, Math.max(min, v)) - min) / span) * (h - 4));
  const last = segs.at(-1)?.at(-1);
  return (
    <div ref={ref} style={{ width: '100%', height: h, position: 'relative' }}>
      {size.width > 0 && (
        <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} style={{ overflow: 'visible', display: 'block' }} aria-hidden>
          <defs>
            <linearGradient id={`g${id}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0" stopColor={tone} stopOpacity="0.32" />
              <stop offset="1" stopColor={tone} stopOpacity="0" />
            </linearGradient>
          </defs>
          {segs.map((seg, i) => {
            const d = smoothPath(seg);
            return (
              <g key={i}>
                {fill && seg.length > 1 && <path d={`${d}L${seg.at(-1)![0]},${h}L${seg[0]![0]},${h}Z`} fill={`url(#g${id})`} style={{ transition: 'd 600ms cubic-bezier(.22,1,.36,1)' }} />}
                <path d={d} fill="none" stroke={tone} strokeWidth={strokeWidth} strokeLinecap="round" strokeLinejoin="round" style={{ transition: 'd 600ms cubic-bezier(.22,1,.36,1)' }} />
              </g>
            );
          })}
          {last && (
            <>
              <circle cx={last[0]} cy={last[1]} r={5} fill={tone} opacity={0.18}>
                <animate attributeName="r" values="3;7;3" dur="2.4s" repeatCount="indefinite" />
              </circle>
              <circle cx={last[0]} cy={last[1]} r={2.4} fill={tone} />
            </>
          )}
        </svg>
      )}
    </div>
  );
}

export interface Series { name: string; color: string; points: { t: number; v: number | null }[] }

export function AreaChart({ series, height = 240, format, yMax, timeFormat, empty }: { series: Series[]; height?: number; format: (v: number) => string; yMax?: number; timeFormat: (t: number) => string; empty?: ReactNode }) {
  const id = useId();
  const [ref, size] = useSize<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const pad = { l: 52, r: 14, t: 12, b: 26 };
  const w = Math.max(100, size.width);
  const h = height;
  const iw = w - pad.l - pad.r, ih = h - pad.t - pad.b;

  const { t0, t1, top, times } = useMemo(() => {
    const all = series.flatMap((s) => s.points);
    const ts = Array.from(new Set(all.map((p) => p.t))).sort((a, b) => a - b);
    const vals = all.map((p) => p.v).filter((v): v is number => v !== null && Number.isFinite(v));
    const rawTop = yMax ?? (vals.length ? Math.max(...vals) : 1);
    return { t0: ts[0] ?? 0, t1: ts.at(-1) ?? 1, top: niceCeil(rawTop || 1), times: ts };
  }, [series, yMax]);

  const x = (t: number) => pad.l + ((t - t0) / Math.max(1, t1 - t0)) * iw;
  const y = (v: number) => pad.t + ih - (Math.max(0, Math.min(top, v)) / top) * ih;
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * top);
  const xticks = times.length ? [0, 0.25, 0.5, 0.75, 1].map((f) => t0 + f * (t1 - t0)) : [];
  const hasData = series.some((s) => s.points.some((p) => p.v !== null));

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - rect.left;
    const t = t0 + ((px - pad.l) / iw) * (t1 - t0);
    let best = 0, dist = Infinity;
    times.forEach((tt, i) => { const d = Math.abs(tt - t); if (d < dist) { dist = d; best = i; } });
    setHover(times.length ? best : null);
  };
  const ht = hover !== null ? times[hover] : undefined;

  return (
    <div ref={ref} style={{ position: 'relative', width: '100%', height: h }}>
      {!hasData && empty && <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center' }}>{empty}</div>}
      {size.width > 0 && (
        <svg width={w} height={h} onPointerMove={onMove} onPointerLeave={() => setHover(null)} style={{ display: 'block', touchAction: 'none' }}>
          <defs>
            {series.map((s, i) => (
              <linearGradient key={i} id={`a${id}${i}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor={s.color} stopOpacity={series.length > 1 ? 0.16 : 0.3} />
                <stop offset="1" stopColor={s.color} stopOpacity="0" />
              </linearGradient>
            ))}
          </defs>
          {ticks.map((v, i) => (
            <g key={i}>
              <line x1={pad.l} x2={w - pad.r} y1={y(v)} y2={y(v)} stroke="var(--grid-line)" strokeDasharray={i === 0 ? undefined : '3 4'} />
              <text x={pad.l - 10} y={y(v)} dy="0.32em" textAnchor="end" fontSize="11" fill="var(--text-4)" style={{ fontVariantNumeric: 'tabular-nums' }}>{format(v)}</text>
            </g>
          ))}
          {xticks.map((t, i) => (
            <text key={i} x={x(t)} y={h - 6} textAnchor={i === 0 ? 'start' : i === xticks.length - 1 ? 'end' : 'middle'} fontSize="11" fill="var(--text-4)">{timeFormat(t)}</text>
          ))}
          {series.map((s, si) => {
            const sorted = [...s.points].sort((a, b) => a.t - b.t);
            const segs = segments(sorted.map((p) => p.v), (i) => x(sorted[i]!.t), y);
            return (
              <motion.g key={s.name} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.5, delay: si * 0.06 }}>
                {segs.map((seg, i) => {
                  const d = smoothPath(seg);
                  return (
                    <g key={i}>
                      {seg.length > 1 && <path d={`${d}L${seg.at(-1)![0]},${pad.t + ih}L${seg[0]![0]},${pad.t + ih}Z`} fill={`url(#a${id}${si})`} />}
                      <path d={d} fill="none" stroke={s.color} strokeWidth={1.8} strokeLinejoin="round" strokeLinecap="round" />
                    </g>
                  );
                })}
              </motion.g>
            );
          })}
          {ht !== undefined && (
            <g pointerEvents="none">
              <line x1={x(ht)} x2={x(ht)} y1={pad.t} y2={pad.t + ih} stroke="var(--line-strong)" />
              {series.map((s) => {
                const p = s.points.find((pp) => pp.t === ht);
                return p && p.v !== null ? <circle key={s.name} cx={x(ht)} cy={y(p.v)} r={4} fill="var(--surface)" stroke={s.color} strokeWidth={2} /> : null;
              })}
            </g>
          )}
        </svg>
      )}
      {ht !== undefined && (
        <div className="tip" style={{ position: 'absolute', left: Math.min(w - 180, Math.max(0, x(ht) + 12)), top: 8, whiteSpace: 'normal', minWidth: 150 }}>
          <div className="dim" style={{ marginBottom: 4 }}>{timeFormat(ht)}</div>
          {series.map((s) => {
            const p = s.points.find((pp) => pp.t === ht);
            return (
              <div key={s.name} className="row" style={{ gap: 8 }}>
                <span style={{ width: 8, height: 8, borderRadius: 2, background: s.color, flex: 'none' }} />
                <span className="grow ellipsis">{s.name}</span>
                <span className="num strong">{p && p.v !== null ? format(p.v) : '—'}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function niceCeil(v: number) {
  if (v <= 0) return 1;
  const exp = Math.pow(10, Math.floor(Math.log10(v)));
  const f = v / exp;
  const nice = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10;
  return nice * exp;
}

export const palette = ['var(--accent)', 'oklch(0.74 0.15 250)', 'oklch(0.78 0.15 60)', 'oklch(0.72 0.17 330)', 'oklch(0.8 0.14 110)', 'oklch(0.7 0.16 25)', 'oklch(0.75 0.12 200)', 'oklch(0.7 0.14 290)'];
