import type { ContainerActionRequest, Operation, OperationState, ReadStatus } from '../api/types';

export type Tone = 'ok' | 'warn' | 'bad' | 'info' | 'idle' | 'accent';

export const terminalStates = new Set<OperationState>(['succeeded', 'rejected', 'failed', 'reverted', 'resolved_manually', 'cancelled']);
export const isTerminal = (op: Pick<Operation, 'state'>) => terminalStates.has(op.state);

export const operationStateLabel: Record<OperationState, string> = {
  accepted: '已受理',
  prepared: '已准备',
  applying: '执行中',
  awaiting_confirmation: '待确认',
  reverting: '回滚中',
  attention_required: '需处理',
  succeeded: '已完成',
  rejected: '已拒绝',
  failed: '失败',
  reverted: '已回滚',
  resolved_manually: '已人工处理',
  cancelled: '已取消',
};

export const operationTone = (state: OperationState): Tone => {
  switch (state) {
    case 'succeeded': case 'resolved_manually': return 'ok';
    case 'failed': case 'rejected': return 'bad';
    case 'awaiting_confirmation': case 'attention_required': return 'warn';
    case 'reverted': case 'cancelled': return 'idle';
    default: return 'info';
  }
};

const subjectLabel: Record<Operation['subject'], string> = {
  firewall: '防火墙变更', component: '组件操作', config: '修改节点配置', name: '修改节点名称', account: '修改节点账号',
  firewall_config: '更新防火墙配置', container: '容器操作', database: '数据库变更', database_step: '数据库步骤',
  task_save: '保存任务', task_run: '执行任务', compose: 'Compose 操作', compose_register: '登记 Compose', shared_initialize: '初始化 OPSD 数据',
  storage_root: '存储根操作',
  storage_pool_create: '创建存储池',
};

export const containerActionLabel: Record<ContainerActionRequest['action'], string> = {
  start: '启动容器', stop: '停止容器', kill: '强制停止容器', restart: '重启容器', remove: '删除容器', update: '更新容器镜像',
};

const actionLabel: Record<string, Record<string, string>> = {
  container: containerActionLabel,
  compose: { pull: '拉取镜像', start: '启动项目', stop: '停止项目', recreate: '重建项目', update: '更新项目' },
  database: {
    create_cluster: '创建数据库集群', import_cluster: '接入数据库集群', add_members: '添加数据库节点', add_proxy: '添加数据库代理',
    create_database: '创建数据库', delete_database: '删除数据库', create_user: '创建账号', password: '修改账号密码',
    delete_user: '删除账号', grant: '授予权限', revoke: '撤销权限', sync_user: '同步代理账号', configure_proxy_routes: '应用代理路由',
  },
  database_step: { deploy_database: '部署数据库', proxy_route: '应用代理路由', proxy_upsert: '同步代理账号', proxy_disable: '停用代理账号', proxy_delete: '删除代理账号' },
  firewall: { add_rule: '添加规则', edit_rule: '修改规则', delete_rule: '删除规则', move_rule: '调整规则顺序', set_default_policy: '修改默认策略', set_enabled: '启停防火墙' },
  firewall_config: { adopt_current: '接纳当前配置', add_rule: '添加规则', edit_rule: '修改规则', delete_rule: '删除规则', move_rule: '调整规则顺序', set_default_policy: '修改默认策略' },
  component: { install: '安装防火墙组件', enable: '启用防火墙' },
  storage_root: { register: '登记存储根', move: '申请组件迁移' },
};

export function operationTitle(op: Operation) {
  return actionLabel[op.subject]?.[op.action ?? ''] ?? subjectLabel[op.subject] ?? op.subject;
}

export const stepStateTone = (s: string): Tone =>
  s === 'completed' || s === 'applied' ? 'ok' : s === 'failed' ? 'bad' : s === 'unknown' ? 'warn' : s === 'pending' ? 'idle' : s === 'reverted' ? 'idle' : 'info';

export function readTone(read?: ReadStatus): Tone {
  if (!read) return 'idle';
  if (read.error || read.state === 'error' || read.state === 'timeout') return 'bad';
  if (read.state === 'offline') return 'idle';
  if (read.state === 'stale') return 'warn';
  if (read.state === 'ready') return 'ok';
  return 'info';
}

export const readLabel: Record<ReadStatus['state'], string> = {
  queued: '排队读取', reading: '读取中', ready: '最新', stale: '数据过期', timeout: '读取超时', error: '读取失败', offline: '节点离线',
};

export function readProblem(read?: ReadStatus) {
  if (!read) return undefined;
  if (read.error) return read.error.message;
  if (read.state === 'error' || read.state === 'timeout' || read.state === 'offline') return readLabel[read.state];
  return undefined;
}

export const containerTone = (state: string, health?: string): Tone => {
  if (health === 'unhealthy') return 'bad';
  switch (state) {
    case 'running': return health === 'starting' ? 'info' : 'ok';
    case 'restarting': return 'warn';
    case 'paused': return 'warn';
    case 'dead': return 'bad';
    case 'exited': case 'created': return 'idle';
    default: return 'idle';
  }
};
export const containerLabel = (state: string) => ({ running: '运行中', exited: '已停止', created: '已创建', restarting: '重启中', paused: '已暂停', dead: '异常', removing: '移除中' } as Record<string, string>)[state] ?? state;

export function usageTone(p?: number): Tone {
  if (p === undefined) return 'idle';
  if (p >= 90) return 'bad';
  if (p >= 75) return 'warn';
  return 'accent';
}

export const backendLabel: Record<string, string> = { ufw: 'UFW', firewalld: 'firewalld', nftables: 'nftables', iptables: 'iptables', none: '未安装', conflict: '多个后端冲突', unknown: '未知' };
