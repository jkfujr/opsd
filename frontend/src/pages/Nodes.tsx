import { useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { Button } from '@appica/ui-react/button';
import { Dialog, DialogContent, DialogTitle, DialogFooter } from '@appica/ui-react/dialog';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { api, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { useSession } from '../auth';
import { queryClient } from '../state';
import { Confirm, date, Failure, PageTitle, TextField } from '../ui';
import { pendingQuery, useOperations } from '../operations';

export const nodesQuery = { queryKey: ['nodes'], queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/nodes', { signal })) };
type Node = components['schemas']['Node'];

function NodeTable({ nodes, actions }: { nodes: Node[]; actions?: (node: Node) => ReactNode }) {
  if (!nodes.length) return <p className="py-10 text-foreground-muted">暂无节点</p>;
  return <div className="max-w-full overflow-x-auto"><Table className="min-w-96" hoverableRows>
    <TableHeader><TableRow><TableHead>节点</TableHead><TableHead>连接状态</TableHead><TableHead>最近连接</TableHead>{actions && <TableHead className="text-right">操作</TableHead>}</TableRow></TableHeader>
    <TableBody>{nodes.map((node) => <TableRow key={node.id}>
      <TableCell><div className="max-w-56 truncate font-medium text-foreground-intense" title={node.name}>{node.name}</div>{node.local && <span className="text-xs text-foreground-muted">本机</span>}</TableCell>
      <TableCell><span className={node.connected ? 'text-foreground-strong' : 'text-foreground-muted'}>{node.revoked_at ? '已撤销' : node.connected ? '在线' : '离线'}</span>{node.connected && !node.writable && <div className="text-xs text-foreground-muted">写入受限</div>}</TableCell>
      <TableCell className="whitespace-nowrap text-sm text-foreground-muted">{date(node.last_seen_at)}</TableCell>
      {actions && <TableCell><div className="flex justify-end gap-2">{actions(node)}</div></TableCell>}
    </TableRow>)}</TableBody>
  </Table></div>;
}

export function Home() {
  const query = useQuery(nodesQuery);
  const pending = useQuery(pendingQuery);
  const operations = useOperations();
  const nodes = query.data?.items.filter((node) => !node.revoked_at) ?? [];
  const online = nodes.filter((node) => node.connected).length;
  return <>
    <PageTitle>首页</PageTitle>
    <Failure error={query.error} />
    <Failure error={pending.error} />
    {pending.data && pending.data.items.length > 0 && <Button variant="outline" className="mb-6" onClick={() => operations.show()}>未决操作 {pending.data.items.length}</Button>}
    {query.isPending ? <p className="text-foreground-muted">加载中…</p> : query.data && <>
      <dl aria-label="节点概览" className="mb-12 grid grid-cols-3 border-y border-border py-6">
        {[['节点', nodes.length], ['在线', online], ['离线', nodes.length - online]].map(([label, count]) => <div key={label} className="border-l border-border px-4 first:border-l-0 first:pl-0 sm:px-8"><dt className="mb-3 text-sm text-foreground-muted">{label}</dt><dd className="text-3xl font-semibold tabular-nums text-foreground-intense sm:text-4xl">{count}</dd></div>)}
      </dl>
      <div className="mb-5 flex items-center justify-between gap-4"><h2 className="text-lg font-semibold">节点</h2><Link to="/settings/nodes" className="text-sm text-primary underline-offset-4 hover:underline">管理节点</Link></div>
      <NodeTable nodes={nodes} />
    </>}
  </>;
}

export function Nodes() {
  const session = useSession();
  const query = useQuery(nodesQuery);
  const [rename, setRename] = useState<Node | null>(null);
  const [revoke, setRevoke] = useState<Node | null>(null);
  const header = { 'X-CSRF-Token': session.csrf_token };
  const renamed = useMutation({ mutationFn: async (name: string) => unwrap(await api.PATCH('/nodes/{id}', { params: { path: { id: rename!.id }, header }, body: { name } })), onSuccess: () => { setRename(null); void queryClient.invalidateQueries({ queryKey: ['nodes'] }); } });
  const revoked = useMutation({ mutationFn: async () => unwrap(await api.POST('/nodes/{id}/revoke', { params: { path: { id: revoke!.id }, header } })), onSuccess: () => { setRevoke(null); void queryClient.invalidateQueries({ queryKey: ['nodes'] }); } });
  return <>
    <PageTitle action={<Link to="/settings/tokens" className="text-sm text-primary underline-offset-4 hover:underline">添加节点</Link>}>节点</PageTitle>
    <Failure error={query.error} />
    {query.isPending ? <p className="text-foreground-muted">加载中…</p> : query.data && <NodeTable nodes={query.data.items} actions={(node) => <><Button variant="ghost" size="sm" onClick={() => { renamed.reset(); setRename(node); }}>改名</Button><Button variant="ghost" size="sm" disabled={Boolean(node.revoked_at)} onClick={() => { revoked.reset(); setRevoke(node); }}>撤销</Button></>} />}
    <Dialog open={rename !== null} onOpenChange={(open) => { if (!open && !renamed.isPending) setRename(null); }}>
      <DialogContent className="w-full max-w-md" closeLabel="关闭">
        <DialogTitle>修改节点名称</DialogTitle>
        {rename && <form className="space-y-6" onSubmit={(event) => { event.preventDefault(); renamed.mutate(String(new FormData(event.currentTarget).get('name'))); }}>
          <TextField label="名称" name="name" defaultValue={rename.name} required maxLength={255} />
          <Failure error={renamed.error} />
          <DialogFooter><Button variant="outline" disabled={renamed.isPending} onClick={() => setRename(null)}>取消</Button><Button type="submit" disabled={renamed.isPending}>保存</Button></DialogFooter>
        </form>}
      </DialogContent>
    </Dialog>
    <Confirm open={revoke !== null} title={`撤销节点“${revoke?.name ?? ''}”？`} description="将断开连接并阻止再次接入，保留节点历史和本机数据。" busy={revoked.isPending} error={revoked.error} onCancel={() => setRevoke(null)} onConfirm={() => revoked.mutate()} />
  </>;
}
