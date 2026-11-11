import { useRef, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { api, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { useSession } from '../auth';
import { queryClient } from '../state';
import { useOperations } from '../operations';
import {
  BusyButton,
  Button,
  Confirm,
  date,
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Failure,
  LoadingRows,
  PageTitle,
  PageToolbar,
  StatusLabel,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TextField,
} from '../ui';
import { EnrollmentDialog } from '../enrollment';

export const nodesQuery = {
  queryKey: ['nodes'],
  queryFn: async ({ signal }: { signal: AbortSignal }) =>
    unwrap(await api.GET('/nodes', { signal })),
};

export type Node = components['schemas']['Node'];

function NodeTable({
  nodes,
  loading,
  ready,
  actions,
}: {
  nodes: Node[];
  loading: boolean;
  ready: boolean;
  actions?: (node: Node) => ReactNode;
}) {
  return (
    <div aria-busy={loading}>
      <Table className="min-w-96" hoverableRows>
        <TableHeader>
          <TableRow>
            <TableHead>节点</TableHead>
            <TableHead>连接状态</TableHead>
            <TableHead>最近连接</TableHead>
            {actions && <TableHead className="text-right">操作</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <LoadingRows columns={actions ? 4 : 3} />
          ) : (
            nodes.map((node) => (
              <TableRow
                key={node.id}
                className={!node.connected || node.revoked_at ? 'opacity-70' : undefined}
              >
                <TableCell>
                  <div className="flex flex-wrap items-center gap-2">
                    <div
                      className="max-w-56 truncate font-bold text-[var(--ink-strong)]"
                      title={node.name}
                    >
                      <Link to={`/nodes/${node.id}`} className="hover:underline">
                        {node.name}
                      </Link>
                    </div>
                    {node.local && (
                      <span className="mono-num border border-[var(--grid-strong)] bg-[var(--bg-elevated)] px-1.5 py-0.5 text-[11px] font-bold">
                        本机
                      </span>
                    )}
                    {node.region && (
                      <span className="mono-num border border-[var(--grid-rule)] px-1.5 py-0.5 text-[11px] text-[var(--ink-muted)]">
                        {node.region}
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  <StatusLabel
                    state={node.connected && !node.revoked_at ? 'online' : 'offline'}
                  >
                    {node.revoked_at ? '已撤销' : node.connected ? '在线' : '离线'}
                  </StatusLabel>
                  {node.connected && !node.writable && (
                    <div className="mt-1 text-xs text-[var(--ink-muted)]">写入受限</div>
                  )}
                </TableCell>
                <TableCell className="mono-num whitespace-nowrap text-xs text-[var(--ink-muted)]">
                  {date(node.last_seen_at)}
                </TableCell>
                {actions && (
                  <TableCell>
                    <div className="flex justify-end gap-2">{actions(node)}</div>
                  </TableCell>
                )}
              </TableRow>
            ))
          )}
          {ready && !nodes.length && (
            <TableRow>
              <TableCell
                colSpan={actions ? 4 : 3}
                className="py-10 text-center text-[var(--ink-muted)]"
              >
                暂无节点
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}

export function Nodes() {
  const session = useSession();
  const operations = useOperations();
  const query = useQuery(nodesQuery);
  const [rename, setRename] = useState<Node | null>(null);
  const [revoke, setRevoke] = useState<Node | null>(null);
  const [enrollment, setEnrollment] = useState<'add' | 'tokens' | null>(null);
  const addTrigger = useRef<HTMLButtonElement>(null);
  const tokensTrigger = useRef<HTMLButtonElement>(null);
  const header = { 'X-CSRF-Token': session.csrf_token };

  const renamed = useMutation({
    mutationFn: async (name: string) =>
      unwrap(
        await api.PATCH('/nodes/{id}', {
          params: { path: { id: rename!.id }, header },
          body: { name },
        }),
      ),
    onSuccess: (op) => {
      operations.show(op.id);
      setRename(null);
      void queryClient.invalidateQueries({ queryKey: ['nodes'] });
    },
  });

  const revoked = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST('/nodes/{id}/revoke', {
          params: { path: { id: revoke!.id }, header },
        }),
      ),
    onSuccess: () => {
      setRevoke(null);
      void queryClient.invalidateQueries({ queryKey: ['nodes'] });
    },
  });

  return (
    <section className="swiss-panel">
      <PageTitle>节点</PageTitle>
      <PageToolbar>
        <span className="mono-num mr-auto text-sm font-bold text-[var(--ink-muted)]">
          {query.data ? `${query.data.items.length} 个节点` : '—'}
        </span>
        <Button
          ref={tokensTrigger}
          variant="outline"
          onClick={() => setEnrollment('tokens')}
        >
          接入码
        </Button>
        <Button ref={addTrigger} onClick={() => setEnrollment('add')}>
          添加节点
        </Button>
      </PageToolbar>
      <Failure
        error={query.error}
        retry={() => void query.refetch()}
        busy={query.isFetching}
      />
      <NodeTable
        nodes={query.data?.items ?? []}
        loading={query.isPending}
        ready={Boolean(query.data) && !query.isError}
        actions={(node) => (
          <>
            <Link
              to={`/tools?node=${node.id}`}
              className="inline-flex h-8 items-center px-2.5 text-xs font-medium text-[var(--ink-body)] hover:bg-[var(--bg-elevated)] hover:text-[var(--ink-strong)]"
            >
              终端
            </Link>
            <Button
              variant="ghost"
              size="sm"
              disabled={query.isError}
              onClick={() => {
                renamed.reset();
                setRename(node);
              }}
            >
              改名
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={Boolean(node.revoked_at) || query.isError}
              onClick={() => {
                revoked.reset();
                setRevoke(node);
              }}
            >
              撤销
            </Button>
          </>
        )}
      />
      {enrollment && (
        <EnrollmentDialog
          key={enrollment}
          mode={enrollment}
          onClose={() => setEnrollment(null)}
          onManage={() => setEnrollment('tokens')}
          trigger={enrollment === 'add' ? addTrigger : tokensTrigger}
        />
      )}
      <Dialog
        open={rename !== null}
        onOpenChange={(open) => {
          if (!open && !renamed.isPending) setRename(null);
        }}
      >
        <DialogContent className="w-full max-w-md" closeLabel="关闭">
          <DialogHeader>
            <DialogTitle>修改节点名称</DialogTitle>
          </DialogHeader>
          <DialogBody className="overflow-y-auto">
            {rename && (
              <form
                id="rename-node"
                className="space-y-4"
                onSubmit={(event) => {
                  event.preventDefault();
                  renamed.mutate(String(new FormData(event.currentTarget).get('name')));
                }}
              >
                <TextField
                  label="名称"
                  name="name"
                  defaultValue={rename.name}
                  required
                  maxLength={255}
                />
                <Failure error={renamed.error} />
              </form>
            )}
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={renamed.isPending}
              onClick={() => setRename(null)}
            >
              取消
            </Button>
            <BusyButton type="submit" form="rename-node" busy={renamed.isPending}>
              保存
            </BusyButton>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Confirm
        open={revoke !== null}
        title={`撤销节点“${revoke?.name ?? ''}”？`}
        description="将断开连接并阻止再次接入，保留节点历史和本机数据。"
        busy={revoked.isPending}
        error={revoked.error}
        onCancel={() => setRevoke(null)}
        onConfirm={() => revoked.mutate()}
      />
    </section>
  );
}
