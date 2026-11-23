import { useEffect, useRef, useState } from 'react';
import { call, enc, errorText } from '../api/http';
import type { ExecutionLogs, Operation } from '../api/types';
import { stripAnsi } from '../lib/format';
import { isTerminal } from '../lib/status';
import { useVisible } from '../lib/hooks';
import { Button, Spinner } from './kit';
import { ArrowDownToLine, RefreshCw } from 'lucide-react';
import { decodeBase64 } from '../lib/format';

/** 操作执行日志：按字节偏移追加读取，操作未结束时持续跟随。 */
export function OperationLogs({ op, height = 360 }: { op: Operation; height?: number | string }) {
  const [text, setText] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [loading, setLoading] = useState(true);
  const [follow, setFollow] = useState(true);
  const [reload, setReload] = useState(0);
  const offset = useRef(0);
  const chunks = useRef<Uint8Array[]>([]);
  const box = useRef<HTMLPreElement>(null);
  const atBottom = useRef(true);
  const visible = useVisible();
  const done = isTerminal(op);

  useEffect(() => { offset.current = 0; chunks.current = []; setText(''); setError(null); setLoading(true); }, [op.id]);

  useEffect(() => {
    let stop = false;
    let timer = 0;
    const read = async () => {
      try {
        let more = true;
        while (more && !stop) {
          const r = (await call<ExecutionLogs>('GET', `/operations/${enc(op.id)}/logs`, { query: { offset: offset.current, limit: 65536 } })).data;
          const bytes = decodeBase64(r.data);
          if (bytes.length) chunks.current.push(bytes);
          offset.current = r.next_offset;
          setTruncated(r.truncated);
          more = r.has_more;
        }
        const total = chunks.current.reduce((a, c) => a + c.length, 0);
        const merged = new Uint8Array(total);
        let p = 0;
        for (const c of chunks.current) { merged.set(c, p); p += c.length; }
        setText(stripAnsi(new TextDecoder().decode(merged)));
        setError(null);
      } catch (e) {
        setError(errorText(e));
      } finally {
        setLoading(false);
      }
      if (!stop && follow && visible && !done) timer = window.setTimeout(read, 1500);
    };
    void read();
    return () => { stop = true; window.clearTimeout(timer); };
  }, [op.id, follow, visible, done, reload]);

  useEffect(() => { const el = box.current; if (el && atBottom.current) el.scrollTop = el.scrollHeight; }, [text]);

  return (
    <div className="stack" style={{ gap: 10 }}>
      <div className="row">
        {!done && <Button size="sm" variant={follow ? 'primary' : 'default'} icon={<ArrowDownToLine />} onClick={() => setFollow((v) => !v)}>{follow ? '跟随中' : '跟随'}</Button>}
        {done && <Button size="sm" icon={<RefreshCw />} onClick={() => { offset.current = 0; chunks.current = []; setReload((n) => n + 1); }}>重新读取</Button>}
        {loading && <Spinner size={14} />}
        {truncated && <span className="muted" style={{ fontSize: 12 }}>日志已达到保存上限</span>}
        {error && <span className="field-error">{error}</span>}
      </div>
      <pre ref={box} className="logs-view" style={{ height }} onScroll={(e) => { const el = e.currentTarget; atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40; }}>
        {text || <span style={{ opacity: 0.5 }}>{loading ? '正在读取…' : '暂无日志'}</span>}
      </pre>
    </div>
  );
}
