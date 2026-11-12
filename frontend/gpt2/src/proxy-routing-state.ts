import type { components } from './api/types';

export function proxyServerState(server: components['schemas']['DatabaseProxyServer'], all: components['schemas']['DatabaseProxyServer'][], groups?: components['schemas']['ProxyHostgroups']) {
  if (groups) {
    if (server.hostgroup === groups.offline) return '离线';
    if (server.hostgroup === groups.backup_writer && server.state === 'ONLINE') return '备用写入';
    if (server.hostgroup === groups.writer && server.state === 'SHUNNED' && all.some(candidate => candidate.hostgroup === groups.backup_writer && candidate.host === server.host && candidate.port === server.port && candidate.state === 'ONLINE')) return '备用写入';
    if (server.hostgroup === groups.writer && server.state === 'ONLINE') return '当前写入';
  }
  return ({ ONLINE: '在线', SHUNNED: '暂不可用', OFFLINE_SOFT: '停止接入', OFFLINE_HARD: '离线' } as Record<string, string>)[server.state] ?? server.state;
}
