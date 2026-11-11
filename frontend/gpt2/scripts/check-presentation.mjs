import assert from 'node:assert/strict';
import { QueryClient } from '@tanstack/react-query';
import { databaseGroupDisplay, databaseMemberDisplay, databaseMetrics, mergeDatabaseHealth } from '../src/database-state.ts';
import { operationSubjectNames, operationTarget } from '../src/operation-summary.ts';

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
  assert.equal(partial.label, '部分可用'); assert.equal(partial.count, '1 / 2'); assert.equal(partial.detail, '1 个待确认');
  const failed = member('a', 'unavailable'); failed.status.ready = false;
  assert.equal(databaseGroupDisplay([failed], 'galera').label, '不可用');
  assert.equal(databaseGroupDisplay([member('a', 'mismatch')], 'galera').label, '关联不一致');
  assert.equal(databaseGroupDisplay([member('a', 'mismatch'), member('b', 'queued', { state: 'queued' })], 'galera').label, '关联不一致');
});
check('保留数据注明上次结果，只读不改变健康，采集时间取最早成员', () => {
  const stale = member('a', 'stale', { state: 'stale', data_at: at, attempted_at: later });
  const result = databaseGroupDisplay([stale], 'galera');
  assert.equal(result.label, '待更新'); assert.equal(result.previous, '上次可用'); assert.equal(result.count, '— / 1'); assert.equal(result.previousCount, '1 / 1');
  assert.equal(databaseGroupDisplay([stale]).previousCount, undefined, '代理不能用 Galera 字段补造健康');
  const readonly = member('b'); readonly.status.read_only = true; readonly.status.read.data_at = later;
  assert.equal(databaseMemberDisplay(readonly, 'galera').label, '可用');
  assert.equal(databaseGroupDisplay([readonly, member('a')], 'galera').dataAt, at);
  assert.equal(databaseMetrics([stale]).retained, true);
  delete readonly.status.connections; assert.equal(databaseMetrics([readonly, stale]).connections, undefined);
});
check('读取故障保留上次服务结果与完整错误', () => {
  const failed = member('a', 'error', { state: 'error', data_at: at, attempted_at: later, error: { message: '远端读取失败' } });
  const result = databaseMemberDisplay(failed, 'galera');
  assert.equal(result.label, '读取失败'); assert.equal(result.previous, '上次可用'); assert.equal(result.error, '远端读取失败');
});
check('旧响应后到不覆盖较新成员，新读取错误保留较新数值', () => {
  const fresh = member('a'); fresh.status.connections = 9; fresh.status.read = { state: 'ready', data_at: later, attempted_at: later };
  const older = member('a'); older.status.connections = 2;
  assert.equal(mergeDatabaseHealth(value([fresh]), value([older])).members[0].status.connections, 9);
  const fault = member('a', 'error', { state: 'error', data_at: at, attempted_at: latest });
  const merged = mergeDatabaseHealth(value([fresh]), value([fault]));
  assert.equal(merged.members[0].status.connections, 9); assert.equal(merged.members[0].status.read.state, 'error'); assert.equal(merged.members[0].status.read.data_at, later);
  const mismatch = value([member('b', 'reading', { state: 'reading' })]); mismatch.cluster.revision = 2;
  assert.equal(mergeDatabaseHealth(value([fresh]), mismatch).members[0].node_id, 'b');
  assert.equal(mergeDatabaseHealth(mismatch, value([fresh])).cluster.revision, 2);
});
check('操作摘要涵盖全部类型，名称读取现有缓存，内部标识保留在详情', () => {
  assert.equal(Object.keys(operationSubjectNames).length, 13);
  const client = new QueryClient();
  const operation = { subject: 'task_run', node_id: 'node', resource_id: 'recording-move' };
  assert.equal(operationTarget(operation, client), 'recording-move');
  client.setQueryData(['tasks', 'node'], { items: [{ id: 'recording-move', node_id: 'node', name: '迁移录播' }] });
  assert.equal(operationTarget(operation, client), '迁移录播');
  assert.equal(operationTarget({ ...operation, resource_id: undefined }, client), undefined);
  assert.equal(operationTarget({ ...operation, subject: 'container', resource_id: 'a'.repeat(64) }, client), undefined);
  client.setQueryData(['containers', ''], { items: [{ container_id: 'a'.repeat(64), node_id: 'node', name: 'web' }] });
  assert.equal(operationTarget({ ...operation, subject: 'container', resource_id: 'a'.repeat(64) }, client), 'web');
  client.clear();
});
console.log(`通过：${checks.length} 组展示检查`);
