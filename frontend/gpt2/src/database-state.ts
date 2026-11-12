import type { components } from './api/types';
import type { Tone } from './ui';
import { readError, reading } from './read-state.ts';

type Member = components['schemas']['ManagedDatabaseMember'];
type Value = components['schemas']['ManagedDatabaseStatus'];
type Read = components['schemas']['ReadStatus'];
type Service = 'ready' | 'unavailable' | 'mismatch';
export const databaseStateNames: Record<string, string> = { ready: '可用', partial: '部分可用', unavailable: '不可用', mismatch: '关联不一致', timeout: '读取超时', error: '读取失败', offline: '节点离线', unknown: '—' };
const services = new Set(['ready', 'unavailable', 'mismatch']);
const time = (value?: string) => value && Number.isFinite(Date.parse(value)) ? Date.parse(value) : 0;
const identity = (member: Member) => `${member.node_id}/${member.instance_id}`;

function service(member: Member, clusterID?: string): Service | undefined {
  const { status } = member;
  if (services.has(status.state) && (status.read.state === 'ready' || status.read.data_at)) return status.state as Service;
  if (status.read.data_at && clusterID && status.cluster_id && status.cluster_id !== clusterID) return 'mismatch';
  // 读取层覆盖 state 后，只能从完整的 Galera 结果确认上次健康；代理不补造结果。
  if (status.read.data_at && clusterID && status.cluster_id === clusterID && [status.primary, status.ready, status.synced].every(value => typeof value === 'boolean')) return status.primary && status.ready && status.synced ? 'ready' : 'unavailable';
}

export function databaseMemberDisplay(member: Member, clusterID?: string) {
  const read = member.status.read, known = service(member, clusterID), failure = readError(read);
  const state = failure ? ['offline', 'timeout'].includes(read.state) ? read.state : 'error' : known ?? 'unknown';
  const error = failure || member.status.error?.message || (known === 'mismatch' ? '配置关联与实际 Galera 集群身份不同' : undefined);
  const tone: Tone = state === 'ready' ? 'success' : ['unavailable', 'mismatch', 'timeout', 'error', 'offline'].includes(state) ? 'error' : 'neutral';
  return { state, label: databaseStateNames[state], tone, service: known, dataAt: read.data_at, error };
}

export function databaseGroupDisplay(members: Member[], clusterID?: string) {
  const values = members.map(member => databaseMemberDisplay(member, clusterID));
  const known = values.filter(value => value.service !== undefined), healthy = known.filter(value => value.service === 'ready').length;
  const unknown = values.length - known.length, abnormal = known.length - healthy;
  const failure = ['offline', 'timeout', 'error'].find(state => values.some(value => value.state === state));
  const state = failure ?? (known.some(value => value.service === 'mismatch') ? 'mismatch' : abnormal ? healthy > 0 ? 'partial' : 'unavailable' : unknown ? 'unknown' : healthy > 0 ? 'ready' : 'unknown');
  const tone: Tone = state === 'ready' ? 'success' : ['unavailable', 'mismatch', 'timeout', 'error', 'offline'].includes(state) ? 'error' : state === 'partial' ? 'warning' : 'neutral';
  const detail = abnormal > 0 ? `${abnormal} 个异常` : '';
  const dates = values.map(value => value.dataAt).filter((value): value is string => time(value) > 0).sort((a, b) => time(a) - time(b));
  return { state, label: databaseStateNames[state], tone, detail, dataAt: dates[0], count: `${unknown ? '—' : healthy} / ${values.length}` };
}

export function databaseMetrics(members: Member[]) {
  const available = members.filter(member => !!member.status.read.data_at);
  const sum = (field: 'connections' | 'receive_queue' | 'send_queue') => available.length === members.length && members.length > 0 && available.every(member => typeof member.status[field] === 'number') ? available.reduce((total, member) => total + member.status[field]!, 0) : undefined;
  const receive = sum('receive_queue'), send = sum('send_queue');
  return { connections: sum('connections'), queue: receive === undefined || send === undefined ? undefined : receive + send,
    flow: available.length === members.length && members.length > 0 && available.every(member => typeof member.status.flow_control === 'number') ? Math.max(...available.map(member => member.status.flow_control!)) * 100 : undefined };
}

function mergeMember(previous: Member | undefined, incoming: Member): Member {
  if (!previous) return incoming;
  const oldRead = previous.status.read, nextRead = incoming.status.read;
  const newerData = time(nextRead.data_at) >= time(oldRead.data_at);
  const newerRead = !nextRead.attempted_at && (reading(nextRead) || !!readError(nextRead)) || time(nextRead.attempted_at) > time(oldRead.attempted_at) || time(nextRead.attempted_at) === time(oldRead.attempted_at) && newerData;
  if (!newerData && !newerRead) return previous;
  const data = newerData ? incoming : previous, read = newerRead ? nextRead : oldRead;
  const state = read.state !== 'ready' && nextRead.data_at === oldRead.data_at && services.has(previous.status.state) ? previous.status.state : data.status.state;
  return { ...data, status: { ...data.status, state, read: { ...read, data_at: data.status.read.data_at } } };
}

export function mergeDatabaseHealth(previous: Value | undefined, incoming: Value): Value {
  if (!previous) return incoming;
  const definition = incoming.cluster.revision >= previous.cluster.revision ? incoming : previous;
  const merge = (field: 'members' | 'proxies') => definition[field].map(member => {
    const next = incoming[field].find(value => identity(value) === identity(member));
    const old = previous[field].find(value => identity(value) === identity(member));
    return next ? mergeMember(old, next) : member;
  });
  return { ...definition, members: merge('members'), proxies: merge('proxies') };
}

export function databaseReading(value: Value, view = 'health') {
  const reads: Read[] = (view === 'proxies' ? value.proxies : view === 'health' ? [...value.members, ...value.proxies] : value.members).map(member => member.status.read);
  return !reads.some(readError) && reads.some(reading);
}
