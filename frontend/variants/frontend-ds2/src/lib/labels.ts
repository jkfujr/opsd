import type { Schema } from '../api/client';

export const operationStates: Record<Schema['OperationState'], string> = {
  accepted: '已受理',
  prepared: '准备执行',
  applying: '应用中',
  awaiting_confirmation: '待签收',
  reverting: '撤销中',
  attention_required: '需处理',
  succeeded: '已成功',
  rejected: '未执行',
  failed: '未完成',
  cancelled: '已取消',
  reverted: '已撤销',
  resolved_manually: '人工处理',
};

export const operationSubjects: Record<Schema['Operation']['subject'], string> = {
  firewall: '防火墙规则',
  component: '防火墙组件',
  config: '节点配置',
  name: '节点名称',
  account: '账号',
  firewall_config: '防火墙配置',
  container: '容器',
  database: '数据库',
  database_step: '数据库步骤',
  task_save: '定时任务',
  task_run: '任务执行',
  compose: 'Compose 项目',
  compose_register: 'Compose 登记',
};

export const stepStates: Record<Schema['OperationStep']['state'], string> = {
  pending: '待执行',
  applying: '应用',
  applied: '已读回',
  reverting: '撤销中',
  reverted: '已撤销',
  completed: '完成',
  failed: '未完成',
  unknown: '待核对',
};

export const readingStates: Record<Schema['MonitoringReading']['state'], string> = {
  ready: '正常',
  partial: '采集不完整',
  error: '采集失败',
  offline: '离线',
  unsupported: '无法采集',
  queued: '等待读取',
  reading: '读取中',
  stale: '待更新',
  timeout: '读取超时',
};

export const firewallStates: Record<Schema['MonitoringReading']['firewall'], string> = {
  enabled: '已启用',
  disabled: '已停用',
  none: '未安装',
  unknown: '未知',
};

export const firewallNames: Record<Schema['Backend'], string> = {
  ufw: 'UFW',
  firewalld: 'firewalld',
  nftables: 'nftables',
  iptables: 'iptables',
  none: '未安装',
  conflict: '冲突',
  unknown: '未知',
};

export const managedFirewallStates: Record<Schema['ManagedFirewallState']['state'], string> = {
  importing: '导入中',
  unavailable: '不可用',
  pending: '待应用',
  applying: '应用中',
  awaiting_confirmation: '待确认',
  in_sync: '已同步',
  failed: '失败',
  unknown: '未知',
};

export const dockerStates: Record<Schema['DockerNodeState']['state'], string> = {
  ready: '可用',
  not_installed: '未安装',
  unavailable: '不可用',
  unsupported: '不支持',
  offline: '离线',
  queued: '等待',
  reading: '读取中',
  stale: '待更新',
  timeout: '超时',
  error: '失败',
};

export const databaseStates: Record<Schema['DatabaseStatus']['state'], string> = {
  ready: '正常',
  unavailable: '不可用',
  mismatch: '成员不一致',
  queued: '等待读取',
  reading: '读取中',
  stale: '待更新',
  timeout: '超时',
  error: '失败',
  offline: '离线',
};

export const peerTrustStates: Record<Schema['PeerTrustNodeStatus']['state'], string> = {
  pending: '待应用',
  in_sync: '已同步',
  failed: '失败',
  disabled: '已停用',
  offline: '离线',
};

export const readStates: Record<Schema['ReadStatus']['state'], string> = {
  queued: '等待读取',
  reading: '读取中',
  ready: '已读取',
  stale: '待更新',
  timeout: '读取超时',
  error: '读取失败',
  offline: '离线',
};

export type Tone = 'ok' | 'warn' | 'bad' | 'info' | 'mute';

const ok = new Set([
  'ready',
  'online',
  'succeeded',
  'confirmed',
  'in_sync',
  'enabled',
  'completed',
  'applied',
  'kept',
]);
const warn = new Set([
  'pending',
  'applying',
  'accepted',
  'prepared',
  'reverting',
  'awaiting_confirmation',
  'attention_required',
  'partial',
  'stale',
  'queued',
  'reading',
  'importing',
  'pending_apply',
  'changed',
  'unknown',
  'conflict',
  'unsupported',
]);
const bad = new Set(['failed', 'error', 'rejected', 'reverted', 'offline', 'unavailable', 'timeout', 'disabled']);
const info = new Set(['running', 'exited', 'paused', 'created', 'restarting', 'removing', 'degraded']);

export function tone(state: string): Tone {
  if (ok.has(state)) return 'ok';
  if (bad.has(state)) return 'bad';
  if (warn.has(state)) return 'warn';
  if (info.has(state)) return 'info';
  return 'mute';
}

export const terminalStates = new Set<Schema['OperationState']>([
  'succeeded',
  'cancelled',
  'rejected',
  'failed',
  'reverted',
  'resolved_manually',
]);
