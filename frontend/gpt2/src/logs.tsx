import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Navigate, useLocation, useNavigate, useSearchParams } from 'react-router';
import { ArrowDownToLine, ArrowLeft, ChevronDown, ChevronUp, Copy, RefreshCw } from 'lucide-react';
import { api, unwrap, useRetryAfter } from './api/client';
import { BusyButton, date, Failure } from './business-ui';
import { Btn, Choice, Empty, Heading, IconButton, SearchBox, Status } from './ui';
import { useUI } from './store';
import { queryClient } from './state';
import { usePageVisible } from './read-polling';
import { appendExecutionLog, cleanLog, executionLogText, logReturnURL, operationHasLogs, type ExecutionLogData } from './log-state';
import { operationStates, useOperations } from './operations';
import { operationTitle, operationTarget } from './operation-summary';

export function ToolsIndex() {
  const [params] = useSearchParams();
  return params.get('view') === 'logs' ? <OperationLogPage key={params.get('operation')}/> : <Navigate to="terminal" replace/>;
}

function LogReadFailure({ error, busy, retry }: { error: unknown; busy: boolean; retry: () => void }) {
  const seconds = useRetryAfter(error);
  if (!error) return null;
  return <div className="log-failure"><div><p role="alert">日志读取失败</p><details><summary>错误详情</summary><Failure error={error}/></details></div><BusyButton busy={busy} disabled={seconds > 0} onClick={retry}>重试</BusyButton></div>;
}

function LogReader({ text, loading, error, busy, refresh, auto, setAuto, more, truncated, controls }: {
  text: string; loading: boolean; error: unknown; busy: boolean; refresh: () => void; auto: boolean; setAuto: (value: boolean) => void; more?: boolean; truncated?: boolean; controls?: ReactNode;
}) {
  const content = useMemo(() => cleanLog(text), [text]), { setToast } = useUI();
  const [wrap, setWrap] = useState(true), [search, setSearch] = useState(''), [match, setMatch] = useState(0);
  const view = useRef<HTMLDivElement>(null), follow = useRef(true), seconds = useRetryAfter(error);
  const matches = useMemo(() => {
    const found: number[] = []; if (!search) return found;
    const haystack = content.toLocaleLowerCase(), needle = search.toLocaleLowerCase();
    for (let at = haystack.indexOf(needle); at >= 0; at = haystack.indexOf(needle, at + needle.length)) found.push(at);
    return found;
  }, [content, search]);
  const active = matches.length ? Math.min(match, matches.length - 1) : 0;
  const selected = matches[active];
  useLayoutEffect(() => { if (follow.current && view.current) view.current.scrollTop = view.current.scrollHeight; }, [content]);
  useEffect(() => { if (selected !== undefined) view.current?.querySelector('mark')?.scrollIntoView({ block: 'nearest', inline: 'nearest' }); }, [selected, search]);
  const step = (direction: number) => { if (matches.length) { follow.current = false; setMatch((active + direction + matches.length) % matches.length); } };
  const copy = async () => {
    try { await navigator.clipboard.writeText(content); setToast('已复制已加载日志'); }
    catch { const pre = view.current?.querySelector('pre'); if (pre) { const range = document.createRange(); range.selectNodeContents(pre); const selection = window.getSelection(); selection?.removeAllRanges(); selection?.addRange(range); } setToast('请按 Ctrl+C 复制已选日志'); }
  };
  return <section className="log-reader" aria-label="日志阅读器"><div className="log-toolbar"><div className="log-search" onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); step(event.shiftKey ? -1 : 1); } }}><SearchBox label="搜索已加载日志" value={search} onChange={value => { follow.current = false; setSearch(value); setMatch(0); }}/>{search && <><span className="log-match" role="status">{search ? `${matches.length ? active + 1 : 0} / ${matches.length}` : ''}</span><IconButton icon={ChevronUp} label="上一个匹配" disabled={!matches.length} onClick={() => step(-1)}/><IconButton icon={ChevronDown} label="下一个匹配" disabled={!matches.length} onClick={() => step(1)}/></>}</div><div className="log-actions">{controls}<Btn aria-pressed={wrap} onClick={() => setWrap(value => !value)}>自动换行</Btn><Btn aria-pressed={auto} onClick={() => setAuto(!auto)}>自动刷新</Btn><IconButton icon={Copy} label="复制已加载日志" disabled={!content} onClick={() => void copy()}/><IconButton icon={RefreshCw} label="刷新日志" disabled={busy || seconds > 0} aria-busy={busy} onClick={refresh}/><IconButton icon={ArrowDownToLine} label="跳到末尾" onClick={() => { follow.current = true; if (view.current) view.current.scrollTop = view.current.scrollHeight; }}/></div></div><LogReadFailure error={error} busy={busy} retry={refresh}/><div ref={view} className={`log-document ${wrap ? 'log-wrap' : ''}`} role="region" aria-label="日志正文" tabIndex={0} onScroll={event => { const el = event.currentTarget; follow.current = el.scrollHeight - el.scrollTop - el.clientHeight < 32; }}>
    {content ? <pre>{selected === undefined ? content : <>{content.slice(0, selected)}<mark>{content.slice(selected, selected + search.length)}</mark>{content.slice(selected + search.length)}</>}</pre> : loading ? <p role="status">正在读取日志…</p> : !error ? <Empty title="暂无日志"/> : null}
  </div>{(more || truncated) && <div className="log-footer">{truncated && <span className="meta">日志已截断</span>}{more && <BusyButton busy={busy} disabled={seconds > 0} onClick={refresh}>加载更多</BusyButton>}</div>}</section>;
}

function OperationLogContent({ id }: { id: string }) {
  const visible = usePageVisible(), [auto, setAuto] = useState(true);
  const key = ['operations', id, 'logs'];
  const query = useQuery({ queryKey: key, queryFn: async ({ signal }) => {
    const previous = queryClient.getQueryData<ExecutionLogData>(key);
    const chunk = unwrap(await api.GET('/operations/{id}/logs', { signal, params: { path: { id }, query: { offset: previous?.next_offset ?? 0, limit: 65536 } } }));
    return appendExecutionLog(previous, chunk);
  }, refetchInterval: query => visible && auto && !query.state.data?.has_more ? 3000 : false });
  return <LogReader text={executionLogText(query.data)} loading={query.isPending} error={query.error} busy={query.isFetching} refresh={() => void query.refetch({ cancelRefetch: false })} auto={auto} setAuto={setAuto} more={query.data?.has_more} truncated={query.data?.truncated}/>;
}
function OperationLogPage() {
  const [params] = useSearchParams(), location = useLocation(), navigate = useNavigate(), context = useOperations();
  const id = params.get('operation') ?? '';
  const operation = useQuery({ queryKey: ['operations', id], enabled: !!id, queryFn: async ({ signal }) => unwrap(await api.GET('/operations/{id}', { signal, params: { path: { id } } })), refetchInterval: 3000 });
  const value = operation.data, target = value && operationTarget(value, queryClient);
  const back = () => navigate(logReturnURL(params.get('return_to'), '/tools/tasks'), { state: { ...location.state, operation: id }, replace: true });
  return <div className="log-page"><Heading title="执行日志" back={<IconButton icon={ArrowLeft} label="返回操作记录" onClick={back}/>} meta={value && <><span>{operationTitle(value)} · {context.nodeName(value.node_id)}{target ? ` · ${target}` : ''}</span><Status tone={value.state === 'succeeded' ? 'success' : ['failed', 'rejected'].includes(value.state) ? 'error' : 'neutral'}>{operationStates[value.state]}</Status></>}/><Failure error={operation.error} retry={() => void operation.refetch()}/>{!id ? <Empty title="缺少操作记录"/> : value ? operationHasLogs(value) ? <OperationLogContent id={id}/> : <Empty title="此操作没有执行日志"/> : operation.isPending && <p role="status">正在读取操作…</p>}</div>;
}

export function ContainerLogPage() {
  const [params] = useSearchParams();
  const path = { node_id: params.get('node_id') ?? '', engine_id: params.get('engine_id') ?? '', container_id: params.get('container_id') ?? '' };
  return <ContainerLogContent key={Object.values(path).join('/')} path={path}/>;
}
function ContainerLogContent({ path }: { path: { node_id: string; engine_id: string; container_id: string } }) {
  const [params] = useSearchParams(), location = useLocation(), navigate = useNavigate(), visible = usePageVisible();
  const [tail, setTail] = useState('200'), [auto, setAuto] = useState(true), valid = Object.values(path).every(Boolean);
  const detail = useQuery({ queryKey: ['containers', path, 'detail'], enabled: valid, queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{node_id}/engines/{engine_id}/containers/{container_id}', { signal, params: { path } })) });
  const query = useQuery({ queryKey: ['containers', path, 'logs', tail], enabled: valid, queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{node_id}/engines/{engine_id}/containers/{container_id}/logs', { signal, params: { path, query: { tail: +tail } } })), refetchInterval: visible && auto ? 3000 : false });
  const fallback = '/containers?' + new URLSearchParams({ node_id: path.node_id, selected: path.container_id, engine_id: path.engine_id });
  const back = () => navigate(logReturnURL(params.get('return_to'), fallback), { state: { container: location.state?.container }, replace: true });
  return <div className="log-page"><Heading title="容器日志" back={<IconButton icon={ArrowLeft} label="返回容器详情" onClick={back}/>} meta={<>{detail.data?.name ?? location.state?.container?.name ?? path.container_id}{query.data && <span>采集于 {date(query.data.checked_at)}</span>}</>}/>{valid ? <LogReader text={query.data?.text ?? ''} loading={query.isPending} error={query.error} busy={query.isFetching} refresh={() => void query.refetch({ cancelRefetch: false })} auto={auto} setAuto={setAuto} truncated={query.data?.truncated} controls={<Choice label="日志行数" value={tail} onChange={setTail} options={[["200", "最近 200 行"], ["1000", "最近 1000 行"]]}/>}/> : <Empty title="缺少容器信息"/>}</div>;
}
