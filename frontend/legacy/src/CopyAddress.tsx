import { useEffect, useRef, useState } from 'react';

export function CopyAddress({ value }: { value: string }) {
  const ref = useRef<HTMLButtonElement>(null);
  const [result, setResult] = useState('');
  useEffect(() => setResult(''), [value]);
  const copy = async () => {
    try {
      try {
        if (!navigator.clipboard) throw new Error();
        await navigator.clipboard.writeText(value);
      } catch {
        const selection = window.getSelection();
        if (!selection || !ref.current) throw new Error();
        const range = document.createRange();
        range.selectNodeContents(ref.current);
        selection.removeAllRanges();
        selection.addRange(range);
        if (!document.execCommand('copy')) throw new Error();
      }
      setResult('已复制');
    } catch { setResult('请选择地址复制'); }
  };
  return <span className="inline-flex max-w-full flex-wrap items-center gap-x-2 gap-y-1"><button ref={ref} type="button" aria-label={`复制 ${value}`} className="max-w-full cursor-pointer select-text break-all text-left font-mono text-xs hover:text-foreground-intense" onClick={() => void copy()}>{value}</button>{result && <span role="status" className="text-xs text-foreground-muted">{result}</span>}</span>;
}
