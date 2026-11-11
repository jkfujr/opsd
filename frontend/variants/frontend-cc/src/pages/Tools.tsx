import { useEffect, useId, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { api, RequestError, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { useSession } from '../auth';
import { queryClient } from '../state';
import {
  invalidateOperations,
  operationStates,
  useOperations,
} from '../operations';
import {
  BusyButton,
  Button,
  buttonVariants,
  ChoiceField,
  Confirm,
  date,
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Drawer,
  DrawerBody,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Failure,
  Field,
  FieldLabel,
  LoadingRows,
  newRequestKey,
  PageTitle,
  Skeleton,
  StatusLabel,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Textarea,
  TextField,
  useUnsavedChanges,
} from '../ui';
import { nodesQuery } from './Nodes';

type TaskItem = components['schemas']['ScheduledTaskItem'];
type TaskInput = components['schemas']['ScheduledTask'];
type ComposeItem = components['schemas']['ComposeProjectItem'];
type ComposeAction = components['schemas']['ComposeActionRequest']['action'];

const composeActions: Record<ComposeAction, string> = {
  pull: '拉取镜像',
  start: '启动',
  stop: '停止',
  recreate: '重建',
  update: '更新项目',
};

const readStateLabels: Record<string, string> = {
  queued: '等待读取',
  reading: '读取中',
  ready: '可用',
  stale: '待更新',
  timeout: '读取超时',
  error: '读取失败',
  offline: '节点离线',
};

function encodeBase64(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

function decodeBase64(b64: string): string {
  try {
    const binary = atob(b64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return new TextDecoder('utf-8', { fatal: false }).decode(bytes);
  } catch {
    return '';
  }
}

export function Tools() {
  const session = useSession();
  const [params, setParams] = useSearchParams();
  const tab =
    params.get('tab') === 'tasks'
      ? 'tasks'
      : params.get('tab') === 'compose'
        ? 'compose'
        : 'terminal';
  const nodeID = params.get('node') || '';

  const setTab = (nextTab: string) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set('tab', nextTab);
        return next;
      },
      { replace: true },
    );
  };

  const setNode = (nextNode: string) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (nextNode) next.set('node', nextNode);
        else next.delete('node');
        return next;
      },
      { replace: true },
    );
  };

  return (
    <div className="space-y-4">
      <PageTitle>工具</PageTitle>
      <section className="swiss-panel">
        <Tabs variant="line" className="gap-4" value={tab} onValueChange={setTab}>
          <TabsList
            aria-label="运维工具"
            className="max-w-full justify-start overflow-x-auto"
          >
            <TabsTrigger value="terminal">终端</TabsTrigger>
            <TabsTrigger value="tasks">定时任务</TabsTrigger>
            <TabsTrigger value="compose">Compose 项目</TabsTrigger>
          </TabsList>

          <TabsContent value="terminal">
            <TerminalWorkspace
              initialNode={nodeID || session.local_node_id}
              onNodeChange={setNode}
            />
          </TabsContent>

          <TabsContent value="tasks">
            <ScheduledTasksWorkspace
              nodeFilter={nodeID}
              onNodeChange={setNode}
            />
          </TabsContent>

          <TabsContent value="compose">
            <ComposeProjectsWorkspace
              nodeFilter={nodeID}
              onNodeChange={setNode}
            />
          </TabsContent>
        </Tabs>
      </section>
    </div>
  );
}

/* ============================================================================
 * 1. WebSocket PTY Terminal Workspace
 * ========================================================================== */

function TerminalWorkspace({
  initialNode,
  onNodeChange,
}: {
  initialNode: string;
  onNodeChange: (id: string) => void;
}) {
  const nodes = useQuery(nodesQuery);
  const [targetNode, setTargetNode] = useState(initialNode);
  const [status, setStatus] = useState<
    'idle' | 'connecting' | 'connected' | 'closed' | 'error'
  >('idle');
  const [errorMsg, setErrorMsg] = useState('');
  const [buffer, setBuffer] = useState('');
  const [cmdInput, setCmdInput] = useState('');
  const [cols, setCols] = useState(120);
  const [rows, setRows] = useState(32);
  const wsRef = useRef<WebSocket | null>(null);
  const screenRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (screenRef.current) {
      screenRef.current.scrollTop = screenRef.current.scrollHeight;
    }
  }, [buffer]);

  useEffect(() => {
    return () => {
      if (wsRef.current) {
        try {
          wsRef.current.send(JSON.stringify({ type: 'exit' }));
          wsRef.current.close();
        } catch {
          /* ignore */
        }
      }
    };
  }, []);

  const connect = useMutation({
    mutationFn: async () => {
      if (wsRef.current) {
        try {
          wsRef.current.close();
        } catch {
          /* ignore */
        }
        wsRef.current = null;
      }
      setStatus('connecting');
      setErrorMsg('');
      const ticket = unwrap(
        await api.POST('/nodes/{id}/terminal', {
          params: { path: { id: targetNode } },
        }),
      );
      return ticket;
    },
    onSuccess: (ticket) => {
      const wsUrl = ticket.url.startsWith('ws')
        ? ticket.url
        : `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${
            window.location.host
          }${ticket.url.startsWith('/') ? '' : '/'}${ticket.url}`;
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        setStatus('connected');
        ws.send(JSON.stringify({ type: 'resize', cols, rows }));
      };

      ws.onmessage = (ev) => {
        try {
          const frame = JSON.parse(String(ev.data)) as components['schemas']['TerminalFrame'];
          if (frame.type === 'output' && frame.data) {
            const text = decodeBase64(frame.data);
            setBuffer((prev) => (prev + text).slice(-131072));
          } else if (frame.type === 'exit') {
            setStatus('closed');
            if (frame.message) {
              setBuffer((prev) => `${prev}\n[会话结束: ${frame.message}]\n`);
            }
          }
        } catch {
          /* ignore malformed frame */
        }
      };

      ws.onerror = () => {
        setStatus('error');
        setErrorMsg('终端连接异常中断');
      };

      ws.onclose = () => {
        setStatus((curr) => (curr === 'error' ? 'error' : 'closed'));
      };
    },
    onError: (err) => {
      setStatus('error');
      setErrorMsg(err instanceof Error ? err.message : '创建终端票据失败');
    },
  });

  const sendRaw = (raw: string) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(
      JSON.stringify({ type: 'input', data: encodeBase64(raw) }),
    );
  };

  const disconnect = () => {
    if (wsRef.current) {
      try {
        wsRef.current.send(JSON.stringify({ type: 'exit' }));
        wsRef.current.close();
      } catch {
        /* ignore */
      }
      wsRef.current = null;
    }
    setStatus('closed');
  };

  const applyResize = (nextCols: number, nextRows: number) => {
    const c = Math.max(2, Math.min(500, nextCols));
    const r = Math.max(2, Math.min(300, nextRows));
    setCols(c);
    setRows(r);
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'resize', cols: c, rows: r }));
    }
  };

  return (
    <div className="space-y-4 pt-2">
      <div className="flex flex-wrap items-end gap-4 border-b border-[var(--grid-rule)] pb-4">
        <div className="w-full sm:w-64">
          <ChoiceField
            label="目标节点"
            value={targetNode}
            options={
              nodes.data?.items
                .filter((n) => !n.revoked_at)
                .map((n) => [n.id, n.name] as const) ?? [[targetNode, '本机']]
            }
            disabled={status === 'connecting' || status === 'connected'}
            onChange={(id) => {
              setTargetNode(id);
              onNodeChange(id);
            }}
          />
        </div>
        <div className="w-24">
          <TextField
            label="列 (Cols)"
            type="number"
            min={2}
            max={500}
            value={cols}
            onChange={(e) => applyResize(Number(e.target.value), rows)}
            className="mono-num"
          />
        </div>
        <div className="w-24">
          <TextField
            label="行 (Rows)"
            type="number"
            min={2}
            max={300}
            value={rows}
            onChange={(e) => applyResize(cols, Number(e.target.value))}
            className="mono-num"
          />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {status === 'connected' ? (
            <>
              <Button
                variant="outline"
                onClick={() => sendRaw('\x03')}
              >
                发送 Ctrl+C
              </Button>
              <Button
                variant="destructive"
                onClick={disconnect}
              >
                断开连接
              </Button>
            </>
          ) : (
            <BusyButton
              busy={connect.isPending || status === 'connecting'}
              onClick={() => connect.mutate()}
            >
              连接终端
            </BusyButton>
          )}
          <Button
            variant="ghost"
            onClick={() => setBuffer('')}
            disabled={!buffer}
          >
            清屏
          </Button>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <StatusLabel
            state={
              status === 'connected'
                ? 'online'
                : status === 'connecting'
                  ? 'applying'
                  : status === 'error'
                    ? 'failed'
                    : 'offline'
            }
          >
            {status === 'connected'
              ? '已连接 PTY'
              : status === 'connecting'
                ? '连接中'
                : status === 'error'
                  ? '连接错误'
                  : '未连接'}
          </StatusLabel>
        </div>
      </div>

      {errorMsg && <Failure error={new Error(errorMsg)} />}

      <div className="border border-[var(--grid-strong)] bg-[#090b0e]">
        <div className="flex items-center justify-between border-b border-[#262b33] bg-[#12161c] px-4 py-2 text-xs text-[#94a3b8]">
          <span className="mono-num font-bold text-[#e2e8f0]">
            ROOT PTY ·{' '}
            {nodes.data?.items.find((n) => n.id === targetNode)?.name ??
              targetNode}
          </span>
          <span className="mono-num">
            {cols}×{rows} · 帧上限 32 KiB
          </span>
        </div>
        <pre
          ref={screenRef}
          tabIndex={0}
          aria-label="终端输出"
          onKeyDown={(e) => {
            if (status !== 'connected') return;
            if (e.ctrlKey && e.key.toLowerCase() === 'c') {
              e.preventDefault();
              sendRaw('\x03');
            } else if (e.key === 'Enter') {
              e.preventDefault();
              sendRaw('\r');
            } else if (e.key === 'Backspace') {
              e.preventDefault();
              sendRaw('\x7f');
            } else if (e.key === 'Tab') {
              e.preventDefault();
              sendRaw('\t');
            } else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
              e.preventDefault();
              sendRaw(e.key);
            }
          }}
          className="mono-num h-96 w-full overflow-auto whitespace-pre-wrap break-all p-4 text-xs leading-relaxed text-[#e5e7eb] focus:outline-none"
        >
          {buffer ||
            (status === 'connected'
              ? '终端已就绪，可直接按键或使用下方命令栏发送输入...\n'
              : '点击“连接终端”获取单次 30 秒票据并建立加密 PTY 会话。')}
        </pre>
        <form
          className="flex border-t border-[#262b33] bg-[#12161c]"
          onSubmit={(e) => {
            e.preventDefault();
            if (!cmdInput) {
              sendRaw('\n');
              return;
            }
            sendRaw(`${cmdInput}\n`);
            setCmdInput('');
          }}
        >
          <span className="mono-num flex items-center px-3 text-xs font-bold text-[var(--signal-red)]">
            root#
          </span>
          <input
            type="text"
            aria-label="终端命令输入"
            disabled={status !== 'connected'}
            value={cmdInput}
            onChange={(e) => setCmdInput(e.target.value)}
            placeholder={
              status === 'connected' ? '输入命令并回车发送...' : '终端未连接'
            }
            className="mono-num h-10 flex-1 bg-transparent px-2 text-xs text-white placeholder:text-[#64748b] focus:outline-none disabled:cursor-not-allowed"
          />
          <Button
            type="submit"
            size="sm"
            disabled={status !== 'connected'}
            className="m-1"
          >
            发送
          </Button>
        </form>
      </div>
    </div>
  );
}

/* ============================================================================
 * 2. Scheduled Shell Tasks Workspace
 * ========================================================================== */

function ScheduledTasksWorkspace({
  nodeFilter,
  onNodeChange,
}: {
  nodeFilter: string;
  onNodeChange: (id: string) => void;
}) {
  const nodes = useQuery(nodesQuery);
  const operations = useOperations();
  const query = useQuery({
    queryKey: ['tasks', 'list', nodeFilter],
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/tasks', {
          signal,
          params: { query: { node_id: nodeFilter || undefined } },
        }),
      ),
    refetchInterval: 5000,
  });

  const [editing, setEditing] = useState<
    { nodeID: string; task?: TaskInput } | null
  >(null);
  const [runsTask, setRunsTask] = useState<TaskItem | null>(null);
  const [logTarget, setLogTarget] = useState<{
    nodeID: string;
    nodeName: string;
    operationID: string;
  } | null>(null);
  const [runUncertain, setRunUncertain] = useState<{
    task: TaskItem;
    key: string;
  } | null>(null);

  const runTask = useMutation({
    mutationFn: async ({ task, key }: { task: TaskItem; key: string }) =>
      unwrap(
        await api.POST('/nodes/{id}/tasks/{task_id}/run', {
          params: {
            path: { id: task.node_id, task_id: task.id },
            header: { 'X-CSRF-Token': '', 'Idempotency-Key': key },
          },
        }),
      ),
    onSuccess: (op) => {
      setRunUncertain(null);
      invalidateOperations();
      void queryClient.invalidateQueries({ queryKey: ['tasks'] });
      operations.show(op.id);
    },
    onError: (err, vars) => {
      if (
        !(err instanceof RequestError && [403, 409, 422].includes(err.status))
      ) {
        setRunUncertain(vars);
      }
    },
  });

  const locateRun = useMutation({
    mutationFn: async () => {
      const result = unwrap(
        await api.GET('/operations', {
          params: {
            query: {
              node_id: runUncertain!.task.node_id,
              idempotency_key: runUncertain!.key,
            },
          },
        }),
      );
      if (result.unavailable?.length) throw new Error('节点暂不可达');
      return result.items[0];
    },
    onSuccess: (op) => {
      setRunUncertain(null);
      runTask.reset();
      if (op) operations.show(op.id);
    },
  });

  const session = useSession();

  return (
    <div className="space-y-4 pt-2">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="w-full sm:w-64">
          <ChoiceField
            label="节点"
            value={nodeFilter}
            options={[
              ['', '全部节点'],
              ...(nodes.data?.items
                .filter((n) => !n.revoked_at)
                .map((n) => [n.id, n.name] as const) ?? []),
            ]}
            onChange={onNodeChange}
          />
        </div>
        <div className="flex flex-wrap gap-2">
          <BusyButton
            variant="outline"
            busy={query.isFetching}
            onClick={() => void query.refetch()}
          >
            刷新
          </BusyButton>
          <Button
            onClick={() =>
              setEditing({
                nodeID: nodeFilter || session.local_node_id,
              })
            }
          >
            新建任务
          </Button>
        </div>
      </div>

      <Failure
        error={query.error ?? runTask.error ?? locateRun.error}
        retry={() => void query.refetch()}
        busy={query.isFetching}
      />

      {runUncertain && (
        <div className="flex flex-wrap items-center gap-3 border border-[var(--signal-amber)] bg-[var(--signal-amber-bg)] p-3">
          <span className="text-sm font-medium text-[var(--signal-amber-ink)]">
            {runUncertain.task.node_name} · 手动执行 {runUncertain.task.name} · 应答未知
          </span>
          <BusyButton
            size="sm"
            variant="outline"
            busy={locateRun.isPending}
            onClick={() => locateRun.mutate()}
          >
            查询原操作
          </BusyButton>
        </div>
      )}

      {Boolean(query.data?.unavailable?.length) && (
        <ul className="space-y-1 border border-[var(--grid-rule)] bg-[var(--bg-elevated)] p-3 text-xs">
          {query.data?.unavailable.map((u) => (
            <li key={u.node_id}>
              <strong className="text-[var(--ink-strong)]">{u.node_name}</strong>
              ：{u.message}
            </li>
          ))}
        </ul>
      )}

      <div aria-busy={query.isPending}>
        <Table className="min-w-180" hoverableRows>
          <TableHeader>
            <TableRow>
              <TableHead>任务名称</TableHead>
              {!nodeFilter && <TableHead>节点</TableHead>}
              <TableHead>Cron 计划 / 时区</TableHead>
              <TableHead>工作目录</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>下次执行</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.isPending ? (
              <LoadingRows columns={nodeFilter ? 6 : 7} />
            ) : (
              query.data?.items.map((task) => (
                <TableRow key={`${task.node_id}/${task.id}`}>
                  <TableCell className="font-bold text-[var(--ink-strong)]">
                    {task.name}
                  </TableCell>
                  {!nodeFilter && (
                    <TableCell className="text-sm">{task.node_name}</TableCell>
                  )}
                  <TableCell className="mono-num text-xs">
                    <span className="font-semibold text-[var(--ink-strong)]">
                      {task.schedule}
                    </span>
                    <span className="block text-[var(--ink-muted)]">
                      {task.timezone}
                    </span>
                  </TableCell>
                  <TableCell className="mono-num max-w-48 break-all text-xs">
                    {task.directory}
                  </TableCell>
                  <TableCell>
                    <StatusLabel state={task.enabled ? 'ready' : 'disabled'}>
                      {task.enabled ? '已启用' : '已停用'}
                    </StatusLabel>
                  </TableCell>
                  <TableCell className="mono-num whitespace-nowrap text-xs">
                    {date(task.next_at)}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <BusyButton
                        variant="ghost"
                        size="sm"
                        busy={
                          runTask.isPending &&
                          runTask.variables?.task.id === task.id
                        }
                        onClick={() =>
                          runTask.mutate({ task, key: newRequestKey() })
                        }
                      >
                        执行
                      </BusyButton>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setRunsTask(task)}
                      >
                        记录
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() =>
                          setEditing({ nodeID: task.node_id, task })
                        }
                      >
                        编辑
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
            {!query.isPending &&
              !query.isError &&
              query.data?.items.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={nodeFilter ? 6 : 7}
                    className="py-8 text-center text-[var(--ink-muted)]"
                  >
                    暂无定时任务
                  </TableCell>
                </TableRow>
              )}
          </TableBody>
        </Table>
      </div>

      {editing && (
        <TaskFormDialog
          nodeID={editing.nodeID}
          initial={editing.task}
          onClose={() => setEditing(null)}
        />
      )}

      {runsTask && (
        <TaskRunsDrawer
          task={runsTask}
          onClose={() => setRunsTask(null)}
          onViewLogs={(opID) =>
            setLogTarget({
              nodeID: runsTask.node_id,
              nodeName: runsTask.node_name,
              operationID: opID,
            })
          }
        />
      )}

      {logTarget && (
        <ExecutionLogsDialog
          nodeID={logTarget.nodeID}
          nodeName={logTarget.nodeName}
          operationID={logTarget.operationID}
          onClose={() => setLogTarget(null)}
        />
      )}
    </div>
  );
}

function TaskFormDialog({
  nodeID: initialNodeID,
  initial,
  onClose,
}: {
  nodeID: string;
  initial?: TaskInput;
  onClose: () => void;
}) {
  const nodes = useQuery(nodesQuery);
  const operations = useOperations();
  const [nodeID, setNodeID] = useState(initialNodeID);
  const generatedID = useRef(initial?.id ?? newRequestKey());
  const requestKey = useRef(newRequestKey());
  const [draft, setDraft] = useState<TaskInput>({
    id: initial?.id ?? generatedID.current,
    revision: initial?.revision ?? 0,
    name: initial?.name ?? '',
    schedule: initial?.schedule ?? '0 * * * *',
    timezone: initial?.timezone ?? 'Asia/Shanghai',
    directory: initial?.directory ?? '/root',
    script: initial?.script ?? '#!/usr/bin/env bash\nset -euo pipefail\n',
    enabled: initial?.enabled ?? true,
  });
  const [dirty, setDirty] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  useUnsavedChanges(dirty);
  const scriptID = useId();
  const enabledID = useId();

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST('/nodes/{id}/tasks', {
          params: {
            path: { id: nodeID },
            header: {
              'X-CSRF-Token': '',
              'Idempotency-Key': requestKey.current,
            },
          },
          body: draft,
        }),
      ),
    onSuccess: (op) => {
      setDirty(false);
      invalidateOperations();
      void queryClient.invalidateQueries({ queryKey: ['tasks'] });
      onClose();
      operations.show(op.id);
    },
    onError: (err) => {
      if (
        !(err instanceof RequestError && [403, 409, 422].includes(err.status))
      ) {
        setUncertain(true);
      }
    },
  });

  const locate = useMutation({
    mutationFn: async () => {
      const result = unwrap(
        await api.GET('/operations', {
          params: {
            query: { node_id: nodeID, idempotency_key: requestKey.current },
          },
        }),
      );
      if (result.unavailable?.length) throw new Error('节点暂不可达');
      return result.items[0];
    },
    onSuccess: (op) => {
      if (op) {
        setDirty(false);
        onClose();
        operations.show(op.id);
      } else {
        setUncertain(false);
        save.reset();
      }
    },
  });

  const change = (patch: Partial<TaskInput>) => {
    setDraft({ ...draft, ...patch });
    setDirty(true);
    requestKey.current = newRequestKey();
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !save.isPending) onClose();
      }}
    >
      <DialogContent className="w-full max-w-2xl" closeLabel="关闭任务表单">
        <DialogHeader>
          <DialogTitle>{initial ? '编辑定时任务' : '新建定时任务'}</DialogTitle>
        </DialogHeader>
        <DialogBody className="space-y-4 overflow-y-auto">
          <form
            id="task-form"
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate();
            }}
          >
            <fieldset
              disabled={save.isPending || uncertain}
              className="space-y-4"
            >
              <div className="grid gap-4 sm:grid-cols-2">
                <ChoiceField
                  label="目标节点"
                  value={nodeID}
                  disabled={Boolean(initial)}
                  options={
                    nodes.data?.items
                      .filter((n) => !n.revoked_at)
                      .map((n) => [n.id, n.name] as const) ?? [[nodeID, '本机']]
                  }
                  onChange={setNodeID}
                />
                <TextField
                  label="任务名称"
                  required
                  value={draft.name}
                  onChange={(e) => change({ name: e.target.value })}
                />
                <TextField
                  label="五字段 Cron"
                  required
                  value={draft.schedule}
                  placeholder="0 * * * *"
                  onChange={(e) => change({ schedule: e.target.value })}
                  className="mono-num"
                />
                <TextField
                  label="IANA 时区"
                  required
                  value={draft.timezone}
                  placeholder="Asia/Shanghai"
                  onChange={(e) => change({ timezone: e.target.value })}
                  className="mono-num"
                />
              </div>
              <TextField
                label="工作目录（绝对路径）"
                required
                value={draft.directory}
                onChange={(e) => change({ directory: e.target.value })}
                className="mono-num"
              />
              <Field>
                <FieldLabel htmlFor={scriptID}>
                  Shell 脚本（经 /bin/bash -lc 执行）
                </FieldLabel>
                <Textarea
                  id={scriptID}
                  rows={6}
                  required
                  value={draft.script}
                  onChange={(e) => change({ script: e.target.value })}
                  className="mono-num text-xs"
                />
              </Field>
              <div className="flex items-center gap-3">
                <label htmlFor={enabledID} className="text-sm font-bold">
                  启用计划调度
                </label>
                <Switch
                  id={enabledID}
                  checked={draft.enabled}
                  onCheckedChange={(enabled) => change({ enabled })}
                />
              </div>
            </fieldset>
            <Failure error={save.error ?? locate.error} />
          </form>
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" disabled={save.isPending} onClick={onClose}>
            取消
          </Button>
          {uncertain ? (
            <BusyButton busy={locate.isPending} onClick={() => locate.mutate()}>
              查询原操作
            </BusyButton>
          ) : (
            <BusyButton type="submit" form="task-form" busy={save.isPending}>
              保存任务
            </BusyButton>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TaskRunsDrawer({
  task,
  onClose,
  onViewLogs,
}: {
  task: TaskItem;
  onClose: () => void;
  onViewLogs: (operationID: string) => void;
}) {
  const operations = useOperations();
  const query = useQuery({
    queryKey: ['tasks', 'runs', task.node_id, task.id],
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/nodes/{id}/tasks/{task_id}/runs', {
          signal,
          params: { path: { id: task.node_id, task_id: task.id } },
        }),
      ),
    refetchInterval: 3000,
  });

  const cancelOp = useMutation({
    mutationFn: async (opID: string) =>
      unwrap(
        await api.POST('/operations/{id}/cancel', {
          params: { path: { id: opID }, header: { 'X-CSRF-Token': '' } },
        }),
      ),
    onSuccess: () => {
      void query.refetch();
      invalidateOperations();
    },
  });

  return (
    <Drawer
      side="right"
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DrawerContent
        closeLabel="关闭运行记录"
        className="h-full w-full max-w-2xl"
      >
        <DrawerHeader>
          <DrawerTitle>运行记录 · {task.name}</DrawerTitle>
        </DrawerHeader>
        <DrawerBody className="space-y-4 overflow-y-auto">
          <Failure
            error={query.error ?? cancelOp.error}
            retry={() => void query.refetch()}
            busy={query.isFetching}
          />
          <Table className="min-w-128">
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.isPending ? (
                <LoadingRows columns={3} />
              ) : (
                query.data?.items.map((op) => (
                  <TableRow key={op.id}>
                    <TableCell className="mono-num text-xs">
                      {date(op.created_at)}
                    </TableCell>
                    <TableCell>
                      <StatusLabel state={op.state}>
                        {operationStates[op.state] ?? op.state}
                      </StatusLabel>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => onViewLogs(op.id)}
                        >
                          日志
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => operations.show(op.id)}
                        >
                          操作详情
                        </Button>
                        {['accepted', 'prepared', 'applying'].includes(
                          op.state,
                        ) && (
                          <BusyButton
                            variant="outline"
                            size="sm"
                            busy={
                              cancelOp.isPending &&
                              cancelOp.variables === op.id
                            }
                            onClick={() => cancelOp.mutate(op.id)}
                          >
                            取消
                          </BusyButton>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
              {!query.isPending && query.data?.items.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={3}
                    className="py-8 text-center text-[var(--ink-muted)]"
                  >
                    暂无运行记录
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </DrawerBody>
      </DrawerContent>
    </Drawer>
  );
}

/* ============================================================================
 * 3. Docker Compose Projects Workspace
 * ========================================================================== */

function ComposeProjectsWorkspace({
  nodeFilter,
  onNodeChange,
}: {
  nodeFilter: string;
  onNodeChange: (id: string) => void;
}) {
  const nodes = useQuery(nodesQuery);
  const operations = useOperations();
  const query = useQuery({
    queryKey: ['compose', 'list', nodeFilter],
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/compose-projects', {
          signal,
          params: { query: { node_id: nodeFilter || undefined } },
        }),
      ),
    refetchInterval: 5000,
  });

  const [uncertain, setUncertain] = useState<{
    nodeID: string;
    label: string;
    key: string;
  } | null>(null);
  const [logTarget, setLogTarget] = useState<{
    nodeID: string;
    nodeName: string;
    operationID: string;
  } | null>(null);

  const register = useMutation({
    mutationFn: async ({
      project,
      key,
    }: {
      project: ComposeItem;
      key: string;
    }) =>
      unwrap(
        await api.POST('/nodes/{id}/compose-projects/{project_id}/register', {
          params: {
            path: { id: project.node_id, project_id: project.id },
            header: { 'X-CSRF-Token': '', 'Idempotency-Key': key },
          },
          body: { engine_id: project.engine_id },
        }),
      ),
    onSuccess: (op) => {
      setUncertain(null);
      invalidateOperations();
      void queryClient.invalidateQueries({ queryKey: ['compose'] });
      operations.show(op.id);
    },
    onError: (err, vars) => {
      if (
        !(err instanceof RequestError && [403, 409, 422].includes(err.status))
      ) {
        setUncertain({
          nodeID: vars.project.node_id,
          label: `登记 ${vars.project.name}`,
          key: vars.key,
        });
      }
    },
  });

  const runAction = useMutation({
    mutationFn: async ({
      project,
      action,
      key,
    }: {
      project: ComposeItem;
      action: ComposeAction;
      key: string;
    }) =>
      unwrap(
        await api.POST('/nodes/{id}/compose-projects/{project_id}/actions', {
          params: {
            path: { id: project.node_id, project_id: project.id },
            header: { 'X-CSRF-Token': '', 'Idempotency-Key': key },
          },
          body: { engine_id: project.engine_id, action },
        }),
      ),
    onSuccess: (op, vars) => {
      setUncertain(null);
      invalidateOperations();
      void queryClient.invalidateQueries({ queryKey: ['compose'] });
      operations.show(op.id);
      setLogTarget({
        nodeID: vars.project.node_id,
        nodeName: vars.project.node_name,
        operationID: op.id,
      });
    },
    onError: (err, vars) => {
      if (
        !(err instanceof RequestError && [403, 409, 422].includes(err.status))
      ) {
        setUncertain({
          nodeID: vars.project.node_id,
          label: `${composeActions[vars.action]} ${vars.project.name}`,
          key: vars.key,
        });
      }
    },
  });

  const locate = useMutation({
    mutationFn: async () => {
      const result = unwrap(
        await api.GET('/operations', {
          params: {
            query: {
              node_id: uncertain!.nodeID,
              idempotency_key: uncertain!.key,
            },
          },
        }),
      );
      if (result.unavailable?.length) throw new Error('节点暂不可达');
      return result.items[0];
    },
    onSuccess: (op) => {
      setUncertain(null);
      register.reset();
      runAction.reset();
      if (op) operations.show(op.id);
    },
  });

  const nonReadyNodes =
    query.data?.nodes.filter((n) => n.read.state !== 'ready') ?? [];

  return (
    <div className="space-y-4 pt-2">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="w-full sm:w-64">
          <ChoiceField
            label="节点"
            value={nodeFilter}
            options={[
              ['', '全部节点'],
              ...(nodes.data?.items
                .filter((n) => !n.revoked_at)
                .map((n) => [n.id, n.name] as const) ?? []),
            ]}
            onChange={onNodeChange}
          />
        </div>
        <BusyButton
          variant="outline"
          busy={query.isFetching}
          onClick={() => void query.refetch()}
        >
          刷新
        </BusyButton>
      </div>

      <Failure
        error={
          query.error ?? register.error ?? runAction.error ?? locate.error
        }
        retry={() => void query.refetch()}
        busy={query.isFetching}
      />

      {uncertain && (
        <div className="flex flex-wrap items-center gap-3 border border-[var(--signal-amber)] bg-[var(--signal-amber-bg)] p-3">
          <span className="text-sm font-medium text-[var(--signal-amber-ink)]">
            {uncertain.label} · 受理结果未知
          </span>
          <BusyButton
            size="sm"
            variant="outline"
            busy={locate.isPending}
            onClick={() => locate.mutate()}
          >
            查询原操作
          </BusyButton>
        </div>
      )}

      {nonReadyNodes.length > 0 && (
        <ul className="space-y-1 border border-[var(--grid-rule)] bg-[var(--bg-elevated)] p-3 text-xs">
          {nonReadyNodes.map((n) => (
            <li key={n.node_id} className="flex items-center gap-2">
              <strong className="text-[var(--ink-strong)]">{n.node_name}</strong>
              <StatusLabel state={n.read.state}>
                {readStateLabels[n.read.state] ?? n.read.state}
              </StatusLabel>
              {n.read.error && (
                <span className="text-[var(--ink-muted)]">
                  {n.read.error.message}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}

      <div aria-busy={query.isPending}>
        <Table className="min-w-180" hoverableRows>
          <TableHeader>
            <TableRow>
              <TableHead>项目</TableHead>
              {!nodeFilter && <TableHead>节点</TableHead>}
              <TableHead>定义目录 / 文件</TableHead>
              <TableHead>服务容器</TableHead>
              <TableHead>状态</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.isPending ? (
              <LoadingRows columns={nodeFilter ? 5 : 6} />
            ) : (
              query.data?.items.map((project) => (
                <TableRow key={`${project.node_id}/${project.id}`}>
                  <TableCell className="font-bold text-[var(--ink-strong)]">
                    {project.name}
                  </TableCell>
                  {!nodeFilter && (
                    <TableCell className="text-sm">
                      {project.node_name}
                    </TableCell>
                  )}
                  <TableCell className="mono-num max-w-64 break-all text-xs">
                    <div>{project.directory}</div>
                    <div className="mt-0.5 text-[var(--ink-muted)]">
                      {project.files.join(', ')}
                    </div>
                  </TableCell>
                  <TableCell className="mono-num text-xs">
                    {project.services.filter((s) => s.state === 'running').length}{' '}
                    / {project.services.length} 运行中
                  </TableCell>
                  <TableCell>
                    <StatusLabel
                      state={project.registered ? 'ready' : 'pending'}
                    >
                      {project.registered ? '已登记' : '待登记'}
                    </StatusLabel>
                    {project.read_only_reason && (
                      <span className="mt-1 block text-xs text-[var(--ink-muted)]">
                        {project.read_only_reason}
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-2">
                      {!project.registered && !project.read_only_reason && (
                        <BusyButton
                          size="sm"
                          variant="outline"
                          busy={
                            register.isPending &&
                            register.variables?.project.id === project.id
                          }
                          onClick={() =>
                            register.mutate({
                              project,
                              key: newRequestKey(),
                            })
                          }
                        >
                          登记项目
                        </BusyButton>
                      )}
                      {project.registered && !project.read_only_reason && (
                        <DropdownMenu>
                          <DropdownMenuTrigger
                            className={buttonVariants({
                              variant: 'ghost',
                              size: 'sm',
                            })}
                            disabled={runAction.isPending}
                          >
                            项目操作
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            {(Object.keys(composeActions) as ComposeAction[]).map(
                              (action) => (
                                <DropdownMenuItem
                                  key={action}
                                  onClick={() =>
                                    runAction.mutate({
                                      project,
                                      action,
                                      key: newRequestKey(),
                                    })
                                  }
                                >
                                  {composeActions[action]}
                                </DropdownMenuItem>
                              ),
                            )}
                          </DropdownMenuContent>
                        </DropdownMenu>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
            {!query.isPending &&
              !query.isError &&
              query.data?.items.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={nodeFilter ? 5 : 6}
                    className="py-8 text-center text-[var(--ink-muted)]"
                  >
                    暂无 Compose 项目
                  </TableCell>
                </TableRow>
              )}
          </TableBody>
        </Table>
      </div>

      {logTarget && (
        <ExecutionLogsDialog
          nodeID={logTarget.nodeID}
          nodeName={logTarget.nodeName}
          operationID={logTarget.operationID}
          onClose={() => setLogTarget(null)}
        />
      )}
    </div>
  );
}

/* ============================================================================
 * 4. Shared Execution Logs Viewer (/nodes/{id}/executions/{operation_id}/logs)
 * ========================================================================== */

function ExecutionLogsDialog({
  nodeID,
  nodeName,
  operationID,
  onClose,
}: {
  nodeID: string;
  nodeName: string;
  operationID: string;
  onClose: () => void;
}) {
  const [offset, setOffset] = useState(0);
  const [chunks, setChunks] = useState<string>('');
  const query = useQuery({
    queryKey: ['execution-logs', nodeID, operationID, offset],
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/nodes/{id}/executions/{operation_id}/logs', {
          signal,
          params: {
            path: { id: nodeID, operation_id: operationID },
            query: { offset, limit: 65536 },
          },
        }),
      ),
    refetchInterval: 2000,
  });

  useEffect(() => {
    if (query.data?.data) {
      const decoded = decodeBase64(query.data.data);
      if (decoded) {
        setChunks((prev) => (offset === 0 ? decoded : prev + decoded));
      }
    }
  }, [query.data, offset]);

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="w-full max-w-4xl" closeLabel="关闭执行日志">
        <DialogHeader>
          <DialogTitle>执行日志 · {nodeName}</DialogTitle>
        </DialogHeader>
        <DialogBody className="space-y-4 overflow-y-auto">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="mono-num text-xs text-[var(--ink-muted)]">
              操作 ID: {operationID}
            </span>
            <div className="flex gap-2">
              {query.data?.has_more && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setOffset(query.data!.next_offset)}
                >
                  继续读取
                </Button>
              )}
              <BusyButton
                size="sm"
                variant="outline"
                busy={query.isFetching}
                onClick={() => void query.refetch()}
              >
                刷新
              </BusyButton>
            </div>
          </div>
          <Failure
            error={query.error}
            retry={() => void query.refetch()}
            busy={query.isFetching}
          />
          {query.data?.truncated && (
            <p role="status" className="text-xs text-[var(--ink-muted)]">
              日志已达到 16 MiB 保存上限并截断
            </p>
          )}
          {query.isPending && !chunks ? (
            <Skeleton className="h-64 w-full" />
          ) : (
            <pre
              aria-label="执行日志"
              className="mono-num max-h-128 min-h-48 overflow-auto whitespace-pre-wrap break-all border border-[var(--grid-strong)] bg-[#090b0e] p-4 text-xs text-[#e5e7eb]"
            >
              {chunks || '暂无输出'}
            </pre>
          )}
        </DialogBody>
      </DialogContent>
    </Dialog>
  );
}
