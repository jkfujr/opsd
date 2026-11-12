import type { components } from './api/types';

type Read = components['schemas']['ReadStatus'];
type NodeRead = components['schemas']['ResourceNodeRead'] | components['schemas']['DockerNodeState'];
type ResourceList = components['schemas']['ContainerList'] | components['schemas']['ComposeProjectList'] | components['schemas']['ScheduledTaskList'];
const errors: Record<string, string> = { error: '读取失败', timeout: '读取超时', offline: '节点离线', unavailable: '服务不可用', unsupported: '不支持读取', not_installed: '未安装 Docker' };
const time = (value?: string) => value ? Date.parse(value) || 0 : 0;

export function readError(read?: Read) {
  return read?.error?.message || (read?.error ? '读取失败' : read && errors[read.state]);
}

export function reading(read?: Read) {
  return !!read && !readError(read) && ['queued', 'reading', 'stale'].includes(read.state);
}

export function resourceError(node: NodeRead) {
  return readError(node.read) || ('error' in node && node.error?.message) || ('state' in node && errors[node.state]) || undefined;
}

export function resourceReading(nodes?: NodeRead[]) {
  return nodes?.some(node => reading(node.read) && !resourceError(node)) ?? false;
}

export function resourceWaiting(nodes?: NodeRead[]) {
  return nodes?.some(node => !node.read.data_at && reading(node.read) && !resourceError(node)) ?? false;
}

export function resourceEmpty(nodes?: NodeRead[]) {
  return !!nodes && nodes.every(node => !resourceError(node) && (!!node.read.data_at || node.read.state === 'ready'));
}

// 保留同一节点较新的已确认内容；读取状态仍来自本次应答，写入限制不因保留内容而改变。
export function retainResourceData<T extends ResourceList>(previous: T | undefined, incoming: T): T {
  if (!previous) return incoming;
  const retained = new Set<string>();
  incoming.nodes = incoming.nodes.map(node => {
    const old = previous.nodes.find(value => value.node_id === node.node_id);
    if (!old) return node;
    if (node.read.attempted_at && time(old.read.data_at) > time(node.read.data_at) && time(old.read.attempted_at) > time(node.read.attempted_at)) {
      retained.add(node.node_id);
      return old;
    }
    if (!old.read.data_at || time(old.read.data_at) <= time(node.read.data_at)) return node;
    retained.add(node.node_id);
    return { ...old, ...node, read: { ...node.read, data_at: old.read.data_at } };
  }) as T['nodes'];
  if (retained.size) incoming.items = [
    ...incoming.items.filter(item => !retained.has(item.node_id ?? '')),
    ...previous.items.filter(item => retained.has(item.node_id ?? '')),
  ] as T['items'];
  return incoming;
}
