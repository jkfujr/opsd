import assert from 'node:assert/strict';
import { databaseGroupDisplay, databaseMemberDisplay, databaseMetrics, mergeDatabaseHealth } from '../src/database-state.ts';
import { readError, resourceEmpty, resourceWaiting, retainResourceData } from '../src/read-state.ts';
import { appendExecutionLog, cleanLog, executionLogText, logReturnURL, operationHasLogs } from '../src/log-state.ts';
import { operationSubjectNames, operationTarget, operationTitle } from '../src/operation-summary.ts';
import { proxyServerState } from '../src/proxy-routing-state.ts';

const at = '2026-11-12T01:00:00Z', later = '2026-11-12T01:00:10Z', latest = '2026-11-12T01:00:20Z';
const member = (id, state = 'ready', read = { state: 'ready', data_at: at, attempted_at: at }) => ({ node_id: id, instance_id: 'sql', name: id, address: 'localhost', status: { state, read, checked_at: latest, cluster_id: 'galera', primary: true, ready: true, synced: true, read_only: false, connections: 4, receive_queue: 0, send_queue: 0, flow_control: 0 } });
const value = members => ({ cluster: { id: 'business', name: '业务集群', revision: 1 }, members, proxies: [], checked_at: latest });
const checks = [];
function check(name, action) { action(); checks.push(name); console.log(`通过：${name}`); }

check('首次读取与未知结果不计为零个可用', () => {
  for (const state of ['queued', 'reading', 'offline', 'timeout', 'error']) {
    const result = databaseGroupDisplay([member('a', state, { state })], 'galera');
    assert.equal(result.count, '— / 1'); assert.notEqual(result.label, '不可用'); assert.equal(result.dataAt, undefined);
  }
});
check('全部有效、部分待确认、真实不可用与关联错误分别显示', () => {
  assert.equal(databaseGroupDisplay([member('a'), member('b')], 'galera').label, '可用');
  const partial = databaseGroupDisplay([member('a'), member('b', 'reading', { state: 'reading' })], 'galera');
  assert.equal(partial.label, '—'); assert.equal(partial.count, '— / 2'); assert.equal(partial.detail, '');
  const failed = member('a', 'unavailable'); failed.status.ready = false;
  assert.equal(databaseGroupDisplay([failed], 'galera').label, '不可用');
  assert.equal(databaseGroupDisplay([member('a', 'mismatch')], 'galera').label, '关联不一致');
  assert.equal(databaseGroupDisplay([member('a', 'mismatch'), member('b', 'queued', { state: 'queued' })], 'galera').label, '关联不一致');
});
check('正常刷新保留确认健康和数量，只读不改变健康，采集时间取最早成员', () => {
  const stale = member('a', 'stale', { state: 'stale', data_at: at, attempted_at: later });
  const result = databaseGroupDisplay([stale], 'galera');
  assert.equal(result.label, '可用'); assert.equal(result.count, '1 / 1');
  assert.equal(databaseGroupDisplay([stale]).count, '— / 1', '代理不能用 Galera 字段补造健康');
  const readonly = member('b'); readonly.status.read_only = true; readonly.status.read.data_at = later;
  assert.equal(databaseMemberDisplay(readonly, 'galera').label, '可用');
  assert.equal(databaseGroupDisplay([readonly, member('a')], 'galera').dataAt, at);
  assert.equal(databaseMetrics([stale]).connections, 4);
  delete readonly.status.connections; assert.equal(databaseMetrics([readonly, stale]).connections, undefined);
});
check('读取故障保留上次服务结果与完整错误', () => {
  const failed = member('a', 'error', { state: 'error', data_at: at, attempted_at: later, error: { message: '远端读取失败' } });
  const result = databaseMemberDisplay(failed, 'galera');
  assert.equal(result.label, '读取失败'); assert.equal(result.service, 'ready'); assert.equal(result.error, '远端读取失败');
});
check('旧响应后到不覆盖较新成员，新读取错误保留较新数值', () => {
  const fresh = member('a'); fresh.status.connections = 9; fresh.status.read = { state: 'ready', data_at: later, attempted_at: later };
  const older = member('a'); older.status.connections = 2;
  assert.equal(mergeDatabaseHealth(value([fresh]), value([older])).members[0].status.connections, 9);
  const fault = member('a', 'error', { state: 'error', data_at: at, attempted_at: latest });
  const merged = mergeDatabaseHealth(value([fresh]), value([fault]));
  assert.equal(merged.members[0].status.connections, 9); assert.equal(merged.members[0].status.read.state, 'error'); assert.equal(merged.members[0].status.read.data_at, later);
  const cold = mergeDatabaseHealth(value([fresh]), value([member('a', 'queued', { state: 'queued' })]));
  assert.equal(cold.members[0].status.connections, 9); assert.equal(cold.members[0].status.read.state, 'queued');
  const mismatch = value([member('b', 'reading', { state: 'reading' })]); mismatch.cluster.revision = 2;
  assert.equal(mergeDatabaseHealth(value([fresh]), mismatch).members[0].node_id, 'b');
  assert.equal(mergeDatabaseHealth(mismatch, value([fresh])).cluster.revision, 2);
});
check('操作摘要涵盖全部类型，旧记录缺失名称保持未知', () => {
  assert.equal(Object.keys(operationSubjectNames).length, 13);
  const operation = { subject: 'task_run', node_id: 'node', resource_id: 'recording-move' };
  assert.equal(operationTarget(operation), undefined);
  assert.equal(operationTarget({ ...operation, target_name: '受理时任务名称' }), '受理时任务名称');
  assert.equal(operationTarget({ ...operation, subject: 'container', resource_id: 'a'.repeat(64) }), undefined);
});
check('正常等待、成功空结果、stale 错误和较旧资源结果分开处理', () => {
  const node = read => ({ node_id: 'a', node_name: '节点 A', read });
  assert(resourceWaiting([node({ state: 'queued' })]));
  assert(!resourceEmpty([node({ state: 'queued' })]));
  assert(resourceEmpty([node({ state: 'ready', data_at: at })]));
  assert.equal(readError({ state: 'stale', error: { message: '实际读取超时' } }), '实际读取超时');
  assert(!resourceWaiting([node({ state: 'stale', error: { message: '读取失败' } })]));
  const previous = { nodes: [node({ state: 'ready', data_at: later, attempted_at: later })], items: [{ node_id: 'a', id: 'new' }] };
  const old = retainResourceData(previous, { nodes: [node({ state: 'ready', data_at: at, attempted_at: at })], items: [{ node_id: 'a', id: 'old' }] });
  assert.equal(old.items[0].id, 'new'); assert.equal(old.nodes[0].read.data_at, later);
  const failed = retainResourceData(previous, { nodes: [node({ state: 'stale', attempted_at: latest, error: { message: '读取失败' } })], items: [] });
  assert.equal(failed.items[0].id, 'new'); assert.equal(readError(failed.nodes[0].read), '读取失败');
});
check('日志能力、ANSI、中文跨字节分页与连续刷新不重复', () => {
  for (const subject of ['task_run', 'container', 'compose']) assert(operationHasLogs(subject));
  for (const subject of ['config', 'name', 'task_save', 'database']) assert(!operationHasLogs(subject));
  assert.equal(cleanLog('a\x1b[31m中文\x1b[0m\r\n\x1b]0;标题\x07b\x1b[2K\x1b[38;'), 'a中文\nb');
  const bytes = Buffer.from('日志中文完成\n'), chunk = (start, end) => ({ offset: start, next_offset: end, data: bytes.subarray(start,end).toString('base64'), has_more: end < bytes.length, truncated: false });
  let value = appendExecutionLog(undefined, chunk(0,4));
  assert.equal(executionLogText(value), '日');
  value = appendExecutionLog(value, chunk(4,bytes.length));
  assert.equal(executionLogText(value), '日志中文完成\n');
  value = appendExecutionLog(value, chunk(bytes.length, bytes.length));
  assert.equal(executionLogText(value), '日志中文完成\n');
  assert.throws(() => appendExecutionLog(value, chunk(0,4)), /分页位置/);
  assert.equal(logReturnURL('/containers?q=web&state=running', '/'), '/containers?q=web&state=running');
  assert.equal(logReturnURL('//example.com', '/containers'), '/containers');
});
check('操作使用受理时名称、具体动作与日志能力', () => {
  const record = { subject: 'container', action: 'restart', target_name: '原容器名称', supports_logs: false, resource: { kind: 'container', node_id: 'node', engine_id: 'original-engine', id: 'a'.repeat(64) } };
  assert.equal(operationTitle(record), '重启容器'); assert.equal(operationTarget(record), '原容器名称'); assert.equal(operationHasLogs(record), false);
  assert.equal(operationTitle({ subject: 'database', action: 'configure_proxy_routes' }), '应用代理路由');
});
check('Galera 正常备用写节点与实际故障分别呈现', () => {
  const groups = { writer: 10, backup_writer: 20, reader: 30, offline: 40 }, old = { hostgroup: 10, host: '10.0.0.1', port: 3306, state: 'SHUNNED' }, backup = { ...old, hostgroup: 20, state: 'ONLINE' };
  assert.equal(proxyServerState(old, [old, backup], groups), '备用写入'); assert.equal(proxyServerState(old, [old], groups), '暂不可用'); assert.equal(proxyServerState({ ...old, state: 'ONLINE' }, [], groups), '当前写入');
});
console.log(`通过：${checks.length} 组展示检查`);
