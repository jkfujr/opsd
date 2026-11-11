import type { components } from './api/types';
import type { Tone } from './ui';

type Member = components['schemas']['ManagedDatabaseMember'];
type Value = components['schemas']['ManagedDatabaseStatus'];
type Read = components['schemas']['ReadStatus'];
type Service = 'ready' | 'unavailable' | 'mismatch';
export const databaseStateNames: Record<string, string> = { ready: '可用', partial: '部分可用', unavailable: '不可用', mismatch: '关联不一致', queued: '等待读取', reading: '读取中', stale: '待更新', timeout: '读取超时', error: '读取失败', offline: '节点离线', unknown: '待确认' };
const services = new Set(['ready', 'unavailable', 'mismatch']);
const faults = new Set(['timeout', 'error', 'offline']);
const time = (value?: string) => value && Number.isFinite(Date.parse(value)) ? Date.parse(value) : 0;
const identity = (member: Member) => `${member.node_id}/${member.instance_id}`;

function service(member: Member, clusterID?: string): Service | undefined {
  const { status } = member;
  if (services.has(status.state) && (status.read.state === 'ready' || status.read.data_at)) return status.state as Service;
  // 读取层覆盖 state 后，只能从完整的 Galera 结果确认上次健康；代理不补造结果。
  if (status.read.data_at && clusterID && status.cluster_id === clusterID && [status.primary, status.ready, status.synced].every(value => typeof value === 'boolean')) return status.primary && status.ready && status.synced ? 'ready' : 'unavailable';
}

export function databaseMemberDisplay(member: Member, clusterID?: string) {
  const read = member.status.read, known = service(member, clusterID), current = read.state === 'ready' && !!known;
  const state = current ? known : read.state === 'ready' ? 'unknown' : read.state;
  const tone: Tone = state === 'ready' ? 'success' : ['unavailable', 'mismatch', 'timeout', 'error'].includes(state) ? 'error' : 'neutral';
  return { state, label: databaseStateNames[state], tone, current, service: known, retained: !!read.data_at && !current, previous: !current && known ? `上次${databaseStateNames[known]}` : undefined, dataAt: read.data_at, error: read.error?.message ?? member.status.error?.message };
}

export function databaseGroupDisplay(members: Member[], clusterID?: string) {
  const values = members.map(member => databaseMemberDisplay(member, clusterID));
  const current = values.filter(value => value.current), healthy = current.filter(value => value.service === 'ready').length;
  const unknown = values.length - current.length, abnormal = current.length - healthy;
  const retained = values.some(value => value.retained);
  const previous = values.filter(value => value.service !== undefined).length === values.length && values.length > 0;
  const previousHealthy = values.filter(value => value.service === 'ready').length;
  let state = healthy === values.length && healthy > 0 ? 'ready' : healthy > 0 ? 'partial' : current.some(value => value.service === 'mismatch') ? 'mismatch' : unknown === 0 && current.length > 0 ? 'unavailable' : 'unknown';
  if (!healthy && unknown && state !== 'mismatch') state = ['offline', 'timeout', 'error', 'reading', 'queued', 'stale'].find(state => values.some(value => value.state === state)) ?? state;
  const tone: Tone = state === 'ready' ? 'success' : ['unavailable', 'mismatch', 'timeout', 'error'].includes(state) ? 'error' : state === 'partial' ? 'warning' : 'neutral';
  const detail = [abnormal > 0 && `${abnormal} 个异常`, unknown > 0 && `${unknown} 个待确认`].filter(Boolean).join('，');
  const dates = values.map(value => value.dataAt).filter((value): value is string => time(value) > 0).sort((a, b) => time(a) - time(b));
  return { state, label: databaseStateNames[state], tone, detail, retained, dataAt: dates[0], previous: retained && previous ? `上次${previousHealthy === values.length ? '可用' : previousHealthy ? '部分可用' : '不可用'}` : undefined,
    count: `${unknown ? healthy || '—' : healthy} / ${values.length}`, previousCount: retained && previous ? `${previousHealthy} / ${values.length}` : undefined };
}

export function databaseMetrics(members: Member[]) {
  const available = members.filter(member => !!member.status.read.data_at);
  const sum = (field: 'connections' | 'receive_queue' | 'send_queue') => available.length === members.length && members.length > 0 && available.every(member => typeof member.status[field] === 'number') ? available.reduce((total, member) => total + member.status[field]!, 0) : undefined;
  const receive = sum('receive_queue'), send = sum('send_queue');
  return { connections: sum('connections'), queue: receive === undefined || send === undefined ? undefined : receive + send,
    flow: available.length === members.length && members.length > 0 && available.every(member => typeof member.status.flow_control === 'number') ? Math.max(...available.map(member => member.status.flow_control!)) * 100 : undefined,
    retained: available.some(member => member.status.read.state !== 'ready') };
}

function mergeMember(previous: Member | undefined, incoming: Member): Member {
  if (!previous) return incoming;
  const oldRead = previous.status.read, nextRead = incoming.status.read;
  const newerData = time(nextRead.data_at) >= time(oldRead.data_at);
  const newerRead = time(nextRead.attempted_at) > time(oldRead.attempted_at) || time(nextRead.attempted_at) === time(oldRead.attempted_at) && newerData;
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
  return !reads.some(read => faults.has(read.state)) && reads.some(read => ['queued', 'reading', 'stale'].includes(read.state));
}
