import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { getThemeScript } from '@appica/ui-react/providers/theme-provider';
import { themeOptions } from './src/theme';

function demoApiPlugin(): Plugin {
  const now = new Date();
  const iso = (offsetMinutes = 0) => new Date(now.getTime() + offsetMinutes * 60_000).toISOString();

  const capabilities = {
    read: { supported: true },
    edit_rules: { supported: true },
    reorder: { supported: true },
    default_policy: { supported: true },
    enable: { supported: true },
    install: { supported: true },
    docker_ports: { supported: true },
    peer_trust_host_ipv4: { supported: true },
    peer_trust_host_ipv6: { supported: true },
    peer_trust_docker_ipv4: { supported: true },
    peer_trust_docker_ipv6: { supported: false, reason: 'Docker IPv6 网桥未启用' },
  };

  let nodes = [
    {
      id: 'node-hk-01',
      name: 'hk-edge-01',
      public_key: 'ed25519:8f9d2a1b4c6e7f01928374655647382910abcdef1234567890abcdef12345678',
      connected: true,
      writable: true,
      last_seen_at: iso(-1),
      capabilities,
      local: true,
      region: '香港可用区 A',
      address: 'https://203.0.113.10:8443',
      console_enabled: true,
      storage_address: 'http://10.24.0.10:5984',
    },
    {
      id: 'node-sg-01',
      name: 'sg-core-01',
      public_key: 'ed25519:3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b',
      connected: true,
      writable: true,
      last_seen_at: iso(-1),
      capabilities,
      local: false,
      region: '新加坡可用区 B',
      address: 'https://198.51.100.24:8443',
      console_enabled: true,
      storage_address: 'http://10.24.0.24:5984',
    },
    {
      id: 'node-tyo-01',
      name: 'tyo-db-01',
      public_key: 'ed25519:7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d',
      connected: true,
      writable: true,
      last_seen_at: iso(-2),
      capabilities,
      local: false,
      region: '东京可用区 C',
      address: 'https://192.0.2.88:8443',
      console_enabled: false,
      storage_address: 'http://10.24.0.88:5984',
    },
  ];

  const metricsByNode: Record<string, any> = {
    'node-hk-01': {
      collected_at: iso(-1),
      cpu_cores: 8,
      cpu_usage: 28.4,
      memory_used: 6.2 * 1024 ** 3,
      memory_total: 16 * 1024 ** 3,
      disk_used: 84 * 1024 ** 3,
      disk_total: 200 * 1024 ** 3,
      network_rx: 420 * 1024 ** 2,
      network_tx: 510 * 1024 ** 2,
      errors: [],
    },
    'node-sg-01': {
      collected_at: iso(-1),
      cpu_cores: 16,
      cpu_usage: 44.8,
      memory_used: 18.6 * 1024 ** 3,
      memory_total: 32 * 1024 ** 3,
      disk_used: 210 * 1024 ** 3,
      disk_total: 500 * 1024 ** 3,
      network_rx: 980 * 1024 ** 2,
      network_tx: 860 * 1024 ** 2,
      errors: [],
    },
    'node-tyo-01': {
      collected_at: iso(-2),
      cpu_cores: 8,
      cpu_usage: 35.2,
      memory_used: 11.4 * 1024 ** 3,
      memory_total: 16 * 1024 ** 3,
      disk_used: 156 * 1024 ** 3,
      disk_total: 320 * 1024 ** 3,
      network_rx: 310 * 1024 ** 2,
      network_tx: 295 * 1024 ** 2,
      errors: [],
    },
  };

  let navOrder = ['overview', 'nodes', 'containers', 'databases', 'storage', 'firewall', 'settings'];
  let navRevision = 1;
  let peerTrustEnabled = true;
  let peerTrustCounter = 4;

  const historyPoints = Array.from({ length: 24 }, (_, idx) => {
    const mins = (23 - idx) * 5;
    const wave = Math.sin(idx / 3);
    return {
      collected_at: iso(-mins),
      summary: {
        metrics: {
          collected_at: iso(-mins),
          cpu_cores: 32,
          cpu_usage: Number((36 + wave * 9).toFixed(1)),
          memory_used: (35.5 + wave * 1.8) * 1024 ** 3,
          memory_total: 64 * 1024 ** 3,
          disk_used: 450 * 1024 ** 3,
          disk_total: 1020 * 1024 ** 3,
          network_rx: (1650 + idx * 14) * 1024 ** 2,
          network_tx: (1580 + idx * 12) * 1024 ** 2,
          errors: [],
        },
        valid_nodes: { cpu: 3, memory: 3, disk: 3, network: 3 },
      },
    };
  });

  let firewallRules = [
    {
      id: 'rule-ssh',
      spec: { family: 'ipv4', source: '0.0.0.0/0', protocol: 'tcp', port: '22', action: 'allow', comment: 'SSH 远程管理' },
      chain: 'INPUT',
      priority: 10,
    },
    {
      id: 'rule-https',
      spec: { family: 'ipv4', source: '0.0.0.0/0', protocol: 'tcp', port: '443', action: 'allow', comment: 'HTTPS 入口流量' },
      chain: 'INPUT',
      priority: 20,
    },
    {
      id: 'rule-opsd',
      spec: { family: 'ipv4', source: '198.51.100.0/24', protocol: 'tcp', port: '8443', action: 'allow', comment: 'OPSD 节点互联' },
      chain: 'INPUT',
      priority: 30,
    },
    {
      id: 'rule-galera',
      spec: { family: 'ipv4', source: '10.24.0.0/16', protocol: 'tcp', port: '3306,4567,4568,4444', action: 'allow', comment: 'Galera 集群内网同步' },
      chain: 'INPUT',
      priority: 40,
    },
  ];

  let defaultPolicies = [
    { family: 'ipv4', chain: 'INPUT', action: 'deny' },
    { family: 'ipv6', chain: 'INPUT', action: 'deny' },
  ];

  let operationsList: any[] = [
    {
      id: 'op-fw-sync-01',
      node_id: 'node-hk-01',
      executor: 'node',
      subject: 'firewall_config',
      state: 'succeeded',
      revision: 3,
      result_pending_sync: false,
      created_at: iso(-25),
      updated_at: iso(-24),
      decision: 'commit',
      actor: 'admin',
      steps: [
        { name: '校验 nftables 规则集', state: 'completed', observed_at: iso(-25) },
        { name: '应用防火墙规则并写入持久化配置', state: 'completed', observed_at: iso(-24) },
      ],
    },
    {
      id: 'op-container-restart-02',
      node_id: 'node-sg-01',
      executor: 'node',
      subject: 'container',
      state: 'succeeded',
      revision: 2,
      result_pending_sync: false,
      created_at: iso(-68),
      updated_at: iso(-67),
      decision: 'commit',
      actor: 'admin',
      resource_id: 'api-gateway',
      steps: [
        { name: '重启容器 api-gateway', state: 'completed', observed_at: iso(-67) },
      ],
    },
  ];

  let apiTokens: any[] = [
    {
      id: 'tok-ci-deploy',
      name: 'ci-cd-deployer',
      owner: 'admin',
      enabled: true,
      revision: 1,
      created_at: iso(-1440),
      expires_at: iso(43200),
      source_ips: ['203.0.113.50/32', '198.51.100.0/24'],
      grants: [
        { module: 'monitoring', level: 'read', scope: 'all', resources: [] },
        { module: 'containers', level: 'manage', scope: 'nodes', resources: ['node-hk-01', 'node-sg-01'] },
      ],
    },
    {
      id: 'tok-grafana-ro',
      name: 'prometheus-reader',
      owner: 'admin',
      enabled: true,
      revision: 1,
      created_at: iso(-2880),
      source_ips: [],
      grants: [
        { module: 'monitoring', level: 'read', scope: 'all', resources: [] },
        { module: 'databases', level: 'read', scope: 'all', resources: [] },
      ],
    },
  ];

  const containers = [
    {
      engine_id: 'docker-hk',
      container_id: 'c9f81a72b101',
      name: 'ingress-caddy',
      image: 'caddy:2.8-alpine',
      state: 'running',
      health: 'healthy',
      ports: [
        { host_ip: '0.0.0.0', host_port: 80, container_port: 80, protocol: 'tcp' },
        { host_ip: '0.0.0.0', host_port: 443, container_port: 443, protocol: 'tcp' },
      ],
      project: 'edge-gateway',
      service: 'caddy',
      node_id: 'node-hk-01',
      node_name: 'hk-edge-01',
    },
    {
      engine_id: 'docker-hk',
      container_id: 'c9f81a72b102',
      name: 'proxysql-edge',
      image: 'proxysql/proxysql:2.6.3',
      state: 'running',
      health: 'healthy',
      ports: [
        { host_ip: '10.24.0.10', host_port: 6033, container_port: 6033, protocol: 'tcp' },
        { host_ip: '127.0.0.1', host_port: 6032, container_port: 6032, protocol: 'tcp' },
      ],
      project: 'edge-gateway',
      service: 'proxysql',
      node_id: 'node-hk-01',
      node_name: 'hk-edge-01',
    },
    {
      engine_id: 'docker-sg',
      container_id: 'a4b29c18d201',
      name: 'api-gateway',
      image: 'ghcr.io/1server/api-gateway:v2.4.1',
      state: 'running',
      health: 'healthy',
      ports: [{ host_ip: '0.0.0.0', host_port: 8080, container_port: 8080, protocol: 'tcp' }],
      project: 'core-services',
      service: 'api',
      node_id: 'node-sg-01',
      node_name: 'sg-core-01',
    },
    {
      engine_id: 'docker-sg',
      container_id: 'a4b29c18d202',
      name: 'redis-cache',
      image: 'redis:7.2-alpine',
      state: 'running',
      health: 'healthy',
      ports: [{ host_ip: '127.0.0.1', host_port: 6379, container_port: 6379, protocol: 'tcp' }],
      project: 'core-services',
      service: 'redis',
      node_id: 'node-sg-01',
      node_name: 'sg-core-01',
    },
    {
      engine_id: 'docker-tyo',
      container_id: 'e7d34f90a301',
      name: 'mariadb-galera-01',
      image: 'mariadb:11.4',
      state: 'running',
      health: 'healthy',
      ports: [{ host_ip: '10.24.0.88', host_port: 3306, container_port: 3306, protocol: 'tcp' }],
      project: 'galera-cluster',
      service: 'mariadb',
      node_id: 'node-tyo-01',
      node_name: 'tyo-db-01',
    },
  ];

  return {
    name: 'opsd-demo-api',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        if (!req.url?.startsWith('/api/v1')) return next();

        const url = new URL(req.url, 'http://127.0.0.1');
        const path = url.pathname.replace(/^\/api\/v1/, '') || '/';
        const method = (req.method || 'GET').toUpperCase();

        if (path === '/events' && method === 'GET') {
          res.writeHead(200, {
            'Content-Type': 'text/event-stream',
            'Cache-Control': 'no-cache',
            Connection: 'keep-alive',
          });
          res.write(': connected\n\n');
          return;
        }

        const sendJson = (status: number, data: unknown) => {
          res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' });
          res.end(JSON.stringify(data));
        };

        const readBody = (): Promise<any> =>
          new Promise((resolve) => {
            let raw = '';
            req.on('data', (chunk) => {
              raw += chunk;
            });
            req.on('end', () => {
              try {
                resolve(raw ? JSON.parse(raw) : {});
              } catch {
                resolve({});
              }
            });
          });

        void (async () => {
          const summaryMetrics = {
            collected_at: iso(0),
            cpu_cores: 32,
            cpu_usage: 37.8,
            memory_used: 36.2 * 1024 ** 3,
            memory_total: 64 * 1024 ** 3,
            disk_used: 450 * 1024 ** 3,
            disk_total: 1020 * 1024 ** 3,
            network_rx: 1710 * 1024 ** 2,
            network_tx: 1665 * 1024 ** 2,
            errors: [],
          };

          if (path === '/auth/session' && method === 'GET') {
            return sendJson(200, {
              session: {
                id: 'sess-demo-01',
                username: 'admin',
                created_at: iso(-180),
                expires_at: iso(720),
                last_seen_at: iso(0),
                remote_address: '127.0.0.1',
                current: true,
              },
              csrf_token: 'csrf-demo-token',
              login_address: 'http://127.0.0.1:5173/admin',
              local_node_id: 'node-hk-01',
            });
          }

          if (path === '/auth/sessions' && method === 'GET') {
            return sendJson(200, {
              items: [
                {
                  id: 'sess-demo-01',
                  username: 'admin',
                  created_at: iso(-180),
                  expires_at: iso(720),
                  last_seen_at: iso(0),
                  remote_address: '127.0.0.1',
                  current: true,
                },
                {
                  id: 'sess-demo-02',
                  username: 'admin',
                  created_at: iso(-720),
                  expires_at: iso(360),
                  last_seen_at: iso(-42),
                  remote_address: '203.0.113.50',
                  current: false,
                },
              ],
            });
          }

          if (path === '/version' || path === '/settings/version') {
            return sendJson(200, {
              frontend_version: 'v0.9.4-ui2',
              node_version: 'v0.9.4',
            });
          }

          if (path === '/preferences/navigation' || path === '/settings/navigation') {
            if (method === 'GET') {
              return sendJson(200, { order: navOrder, revision: navRevision });
            }
            if (method === 'PUT' || method === 'PATCH') {
              const body = await readBody();
              if (Array.isArray(body.order)) navOrder = body.order;
              navRevision += 1;
              return sendJson(200, { order: navOrder, revision: navRevision });
            }
          }

          if (path === '/settings/account') {
            if (method === 'GET') {
              return sendJson(200, {
                username: 'admin',
                entrance: '/admin',
                revision: 1,
                last_synced_at: iso(-10),
              });
            }
            if (method === 'PATCH') {
              return sendJson(200, {
                login_address: 'http://127.0.0.1:5173/admin',
              });
            }
          }

          if (path === '/auth/session' && method === 'DELETE') {
            res.writeHead(204);
            return res.end();
          }

          if (/^\/auth\/sessions\/[^/]+$/.test(path) && method === 'DELETE') {
            res.writeHead(204);
            return res.end();
          }

          if (path === '/overview' && method === 'GET') {
            return sendJson(200, {
              updated_at: iso(0),
              summary: {
                metrics: summaryMetrics,
                valid_nodes: { cpu: 3, memory: 3, disk: 3, network: 3 },
              },
              nodes: nodes.map((node) => ({
                node,
                reading: {
                  node_id: node.id,
                  state: 'ready',
                  metrics: metricsByNode[node.id],
                  firewall: 'enabled',
                  read: { state: 'ready', data_at: iso(-1), attempted_at: iso(0) },
                },
              })),
              pending_operations: [],
            });
          }

          if (path === '/monitoring/current' && method === 'GET') {
            return sendJson(200, {
              checked_at: iso(0),
              summary: {
                metrics: summaryMetrics,
                valid_nodes: { cpu: 3, memory: 3, disk: 3, network: 3 },
              },
              items: nodes.map((node) => ({
                node_id: node.id,
                name: node.name,
                region: node.region || '',
                state: 'ready',
                checked_at: iso(0),
                metrics: metricsByNode[node.id],
                read: { state: 'ready', data_at: iso(-1), attempted_at: iso(0) },
              })),
            });
          }

          if (path === '/monitoring/history' && method === 'GET') {
            return sendJson(200, {
              interval_minutes: 5,
              checked_at: iso(0),
              items: historyPoints,
              nodes: nodes.map((node) => ({
                node_id: node.id,
                items: historyPoints.map((pt, i) => ({
                  ...metricsByNode[node.id],
                  collected_at: pt.collected_at,
                  cpu_usage: Number(
                    Math.max(8, Math.min(92, metricsByNode[node.id].cpu_usage + Math.sin(i / 2.5) * 7)).toFixed(1),
                  ),
                })),
              })),
            });
          }

          if (path === '/nodes' && method === 'GET') {
            return sendJson(200, { items: nodes });
          }

          const nodeMatch = path.match(/^\/nodes\/([^/]+)$/);
          if (nodeMatch && method === 'GET') {
            const id = decodeURIComponent(nodeMatch[1]);
            const found = nodes.find((n) => n.id === id) || nodes[0];
            return sendJson(200, found);
          }
          if (nodeMatch && method === 'PATCH') {
            const id = decodeURIComponent(nodeMatch[1]);
            const body = await readBody();
            nodes = nodes.map((n) => (n.id === id ? { ...n, name: body.name || n.name } : n));
            const updated = nodes.find((n) => n.id === id) || nodes[0];
            return sendJson(200, updated);
          }

          const pubAddrMatch = path.match(/^\/nodes\/([^/]+)\/public-addresses(\/refresh)?$/);
          if (pubAddrMatch) {
            const id = decodeURIComponent(pubAddrMatch[1]);
            const ipMap: Record<string, { v4: string; v6: string }> = {
              'node-hk-01': { v4: '203.0.113.10', v6: '2001:db8:hk::10' },
              'node-sg-01': { v4: '198.51.100.24', v6: '2001:db8:sg::24' },
              'node-tyo-01': { v4: '192.0.2.88', v6: '2001:db8:tyo::88' },
            };
            const addr = ipMap[id] || ipMap['node-hk-01'];
            return sendJson(200, {
              node_id: id,
              revision: 2,
              ipv4: {
                mode: 'auto',
                addresses: [addr.v4],
                state: 'confirmed',
                updated_at: iso(-5),
                observations: [
                  { source: 'peer', address: addr.v4, observed_at: iso(-5) },
                  { source: 'ipify', address: addr.v4, observed_at: iso(-5) },
                ],
              },
              ipv6: {
                mode: 'auto',
                addresses: [addr.v6],
                state: 'confirmed',
                updated_at: iso(-5),
                observations: [{ source: 'local', address: addr.v6, observed_at: iso(-5) }],
              },
            });
          }

          const nodeSettingsMatch = path.match(/^\/nodes\/([^/]+)\/settings$/);
          if (nodeSettingsMatch && method === 'GET') {
            const id = decodeURIComponent(nodeSettingsMatch[1]);
            const node = nodes.find((n) => n.id === id) || nodes[0];
            return sendJson(200, {
              revision: 4,
              name: node.name,
              address: node.address || 'https://203.0.113.10:8443',
              region: node.region || '香港可用区 A',
              listen: '0.0.0.0:8443',
              console: Boolean(node.console_enabled),
              console_listen: '127.0.0.1:9443',
              preferred_storage: 'node-hk-01',
              interval_minutes: 5,
              retention_days: 7,
              storage: {
                enabled: true,
                address: '10.24.0.10',
                port: 5984,
              },
              address_mode: 'auto',
              api: true,
              last_synced_at: iso(-2),
            });
          }
          if (nodeSettingsMatch && method === 'PATCH') {
            const id = decodeURIComponent(nodeSettingsMatch[1]);
            const op = {
              id: `op-settings-${Math.random().toString(16).slice(2, 6)}`,
              node_id: id,
              kind: 'node_settings_apply',
              state: 'succeeded',
              revision: 1,
              created_at: iso(0),
              updated_at: iso(0),
              finished_at: iso(0),
              steps: [
                {
                  name: '应用节点配置并同步状态',
                  state: 'succeeded',
                  started_at: iso(0),
                  finished_at: iso(0),
                },
              ],
            };
            operationsList = [op, ...operationsList];
            return sendJson(202, op);
          }

          const nodeRevokeMatch = path.match(/^\/nodes\/([^/]+)\/revoke$/);
          if (nodeRevokeMatch && method === 'POST') {
            const id = decodeURIComponent(nodeRevokeMatch[1]);
            nodes = nodes.map((n) => (n.id === id ? { ...n, state: 'revoked' } : n));
            return sendJson(200, nodes.find((n) => n.id === id) || nodes[0]);
          }

          const nodeStorageMatch = path.match(/^\/nodes\/([^/]+)\/storage$/);
          if (nodeStorageMatch && method === 'GET') {
            return sendJson(200, {
              enabled: true,
              selected: 'node-hk-01',
              pending: 0,
              state: 'ready',
              replications: [
                { id: 'rep-hk-to-sg', state: 'running', error_count: 0 },
                { id: 'rep-hk-to-tyo', state: 'running', error_count: 0 },
              ],
            });
          }

          const nodeAccountMatch = path.match(/^\/nodes\/([^/]+)\/account$/);
          if (nodeAccountMatch && method === 'GET') {
            return sendJson(200, {
              username: 'admin',
              entrance: '/admin',
              revision: 1,
              last_synced_at: iso(-10),
            });
          }
          if (nodeAccountMatch && method === 'PATCH') {
            const id = decodeURIComponent(nodeAccountMatch[1]);
            const op = {
              id: `op-account-${Math.random().toString(16).slice(2, 6)}`,
              node_id: id,
              kind: 'node_account_update',
              state: 'succeeded',
              revision: 1,
              created_at: iso(0),
              updated_at: iso(0),
              finished_at: iso(0),
              steps: [
                {
                  name: '更新节点登录账号与入口',
                  state: 'succeeded',
                  started_at: iso(0),
                  finished_at: iso(0),
                },
              ],
            };
            operationsList = [op, ...operationsList];
            return sendJson(202, op);
          }

          const nodePasswordMatch = path.match(/^\/nodes\/([^/]+)\/account\/password$/);
          if (nodePasswordMatch && method === 'POST') {
            return sendJson(200, {
              username: 'admin',
              entrance: '/admin',
              password: 'Opsd#Demo9421!Secure',
            });
          }

          const peerTrustRules = [
            {
              address: '203.0.113.10',
              family: 'ipv4',
              members: [{ id: 'node-hk-01', name: 'hk-edge-01' }],
              state: 'in_sync',
            },
            {
              address: '198.51.100.24',
              family: 'ipv4',
              members: [{ id: 'node-sg-01', name: 'sg-core-01' }],
              state: 'in_sync',
            },
            {
              address: '192.0.2.88',
              family: 'ipv4',
              members: [{ id: 'node-tyo-01', name: 'tyo-db-01' }],
              state: 'in_sync',
            },
          ];

          if (path === '/peer-trust' && method === 'GET') {
            return sendJson(200, {
              version: { counter: peerTrustCounter, node_id: 'node-hk-01' },
              enabled: peerTrustEnabled,
              updated_at: iso(-10),
            });
          }

          if (path === '/peer-trust' && method === 'PATCH') {
            const body = await readBody();
            peerTrustEnabled = Boolean(body.enabled);
            peerTrustCounter += 1;
            return sendJson(200, {
              version: { counter: peerTrustCounter, node_id: 'node-hk-01' },
              enabled: peerTrustEnabled,
              updated_at: iso(0),
            });
          }

          if (path === '/peer-trust/status' && method === 'GET') {
            return sendJson(200, {
              policy: {
                version: { counter: peerTrustCounter, node_id: 'node-hk-01' },
                enabled: peerTrustEnabled,
                updated_at: iso(-10),
              },
              nodes: nodes.map((node) => ({
                node_id: node.id,
                enabled: peerTrustEnabled,
                policy_version: { counter: peerTrustCounter, node_id: 'node-hk-01' },
                target_revision: peerTrustCounter,
                applied_revision: peerTrustCounter,
                state: peerTrustEnabled ? 'in_sync' : 'disabled',
                checked_at: iso(-2),
                rules: peerTrustRules,
                results: [
                  { family: 'ipv4', component: 'host', scope: 'both', state: 'in_sync', addresses: ['203.0.113.10', '198.51.100.24', '192.0.2.88'] },
                  { family: 'ipv4', component: 'docker', scope: 'runtime', state: 'in_sync', addresses: ['203.0.113.10', '198.51.100.24', '192.0.2.88'] },
                ],
              })),
            });
          }

          const nodePeerTrustMatch = path.match(/^\/nodes\/([^/]+)\/firewall\/peer-trust$/);
          if (nodePeerTrustMatch && method === 'GET') {
            const nodeId = decodeURIComponent(nodePeerTrustMatch[1]);
            return sendJson(200, {
              node_id: nodeId,
              enabled: peerTrustEnabled,
              policy_version: { counter: peerTrustCounter, node_id: 'node-hk-01' },
              target_revision: peerTrustCounter,
              applied_revision: peerTrustCounter,
              state: peerTrustEnabled ? 'in_sync' : 'disabled',
              checked_at: iso(-2),
              rules: peerTrustRules,
              results: [
                { family: 'ipv4', component: 'host', scope: 'both', state: 'in_sync', addresses: ['203.0.113.10', '198.51.100.24', '192.0.2.88'] },
                { family: 'ipv4', component: 'docker', scope: 'runtime', state: 'in_sync', addresses: ['203.0.113.10', '198.51.100.24', '192.0.2.88'] },
              ],
            });
          }

          if (path === '/enrollment-tokens' && method === 'GET') {
            return sendJson(200, {
              items: [
                {
                  id: 'tok-enroll-98a1',
                  created_at: iso(-90),
                  expires_at: iso(60),
                },
                {
                  id: 'tok-enroll-73c4',
                  created_at: iso(-1200),
                  expires_at: iso(-1100),
                  consumed_at: iso(-1185),
                },
              ],
            });
          }

          if (path === '/enrollment-tokens' && method === 'POST') {
            return sendJson(201, {
              token: 'opsd_enroll_9f8e7d6c5b4a3f2e1d0c9b8a7f6e5d4c',
              record: {
                id: `tok-enroll-${Math.random().toString(16).slice(2, 6)}`,
                created_at: iso(0),
                expires_at: iso(30),
              },
              peer_address: 'https://203.0.113.10:8443',
              peer_public_key: nodes[0].public_key,
              version: 'v0.9.4',
              installer_url: 'https://get.1server.dev/opsd-install.sh',
            });
          }

          if (/^\/enrollment-tokens\/[^/]+$/.test(path) && method === 'DELETE') {
            res.writeHead(204);
            return res.end();
          }

          if (path === '/containers' && method === 'GET') {
            return sendJson(200, {
              items: containers,
              nodes: nodes.map((node) => ({
                node_id: node.id,
                node_name: node.name,
                engine_id: `docker-${node.id.split('-')[1]}`,
                version: '27.3.1',
                state: 'ready',
                checked_at: iso(-1),
                read: { state: 'ready', data_at: iso(-1), attempted_at: iso(0) },
              })),
            });
          }

          const containerDetailMatch = path.match(/^\/nodes\/([^/]+)\/containers\/([^/]+)$/);
          if (containerDetailMatch && method === 'GET') {
            const containerId = decodeURIComponent(containerDetailMatch[2]);
            const found = containers.find((c) => c.container_id === containerId) || containers[0];
            return sendJson(200, {
              ...found,
              created_at: iso(-4320),
              started_at: iso(-1440),
              restart_policy: 'unless-stopped',
              restart_count: 0,
              tty: false,
              checked_at: iso(0),
              mounts: [
                { type: 'bind', source: '/etc/opsd/service.conf', destination: '/etc/service/config.yml', read_only: true },
                { type: 'volume', source: `${found.name}-data`, destination: '/var/lib/data', read_only: false },
              ],
              networks: [
                { name: `${found.project}_default`, ipv4: '172.22.0.14', ipv6: '' },
              ],
            });
          }

          const containerLogsMatch = path.match(/^\/nodes\/([^/]+)\/containers\/([^/]+)\/logs$/);
          if (containerLogsMatch && method === 'GET') {
            const containerId = decodeURIComponent(containerLogsMatch[2]);
            const found = containers.find((c) => c.container_id === containerId) || containers[0];
            return sendJson(200, {
              engine_id: found.engine_id,
              container_id: found.container_id,
              truncated: false,
              checked_at: iso(0),
              text: [
                `${iso(-15)} INFO  [${found.name}] Starting service worker (version=2.4.1, pid=1)`,
                `${iso(-14)} INFO  [${found.name}] Listening on configured sockets`,
                `${iso(-8)} INFO  [${found.name}] Health check passed (latency=1.2ms)`,
                `${iso(-2)} INFO  [${found.name}] Upstream sync completed: 0 errors`,
              ].join('\n'),
            });
          }

          const containerActionMatch = path.match(/^\/nodes\/([^/]+)\/containers\/([^/]+)\/actions$/);
          if (containerActionMatch && method === 'POST') {
            const nodeId = decodeURIComponent(containerActionMatch[1]);
            const op = {
              id: `op-container-${Math.random().toString(16).slice(2, 6)}`,
              node_id: nodeId,
              kind: 'container_action',
              state: 'succeeded',
              revision: 1,
              created_at: iso(0),
              updated_at: iso(0),
              finished_at: iso(0),
              steps: [
                {
                  name: '执行容器操作',
                  state: 'succeeded',
                  started_at: iso(0),
                  finished_at: iso(0),
                },
              ],
            };
            operationsList = [op, ...operationsList];
            return sendJson(202, op);
          }

          const galeraClusterStatus = {
            checked_at: iso(-1),
            cluster: {
              id: 'cluster-prod-galera',
              name: 'prod-galera-core',
              coordinator: 'node-hk-01',
              cluster_id: 'bb82f190-64c1-11ef-9a12-0242ac160002',
              revision: 6,
              workbench_url: 'https://db-admin.internal.1server.dev',
              deleted: false,
              updated_at: iso(-60),
              members: [
                { node_id: 'node-hk-01', instance_id: 'galera-hk', name: 'mariadb-hk-01', address: '10.24.0.10:3306' },
                { node_id: 'node-sg-01', instance_id: 'galera-sg', name: 'mariadb-sg-01', address: '10.24.0.24:3306' },
                { node_id: 'node-tyo-01', instance_id: 'galera-tyo', name: 'mariadb-tyo-01', address: '10.24.0.88:3306' },
              ],
              proxies: [
                { node_id: 'node-hk-01', instance_id: 'proxysql-hk', name: 'proxysql-edge-hk', address: '10.24.0.10:6033' },
                { node_id: 'node-sg-01', instance_id: 'proxysql-sg', name: 'proxysql-edge-sg', address: '10.24.0.24:6033' },
              ],
            },
            members: [
              {
                node_id: 'node-hk-01',
                instance_id: 'galera-hk',
                name: 'mariadb-hk-01',
                address: '10.24.0.10:3306',
                status: {
                  state: 'ready',
                  version: '11.4.3-MariaDB-log',
                  cluster_id: 'bb82f190-64c1-11ef-9a12-0242ac160002',
                  cluster_name: 'prod-galera-core',
                  cluster_size: 3,
                  primary: true,
                  ready: true,
                  synced: true,
                  read_only: false,
                  connections: 42,
                  receive_queue: 0,
                  send_queue: 0,
                  flow_control: 0,
                  checked_at: iso(-1),
                  read: { state: 'ready', data_at: iso(-1), attempted_at: iso(0) },
                  databases: [
                    { name: 'app_production', charset: 'utf8mb4', collation: 'utf8mb4_unicode_ci', bytes: 4.8 * 1024 ** 3, protected: false },
                    { name: 'billing_ledger', charset: 'utf8mb4', collation: 'utf8mb4_unicode_ci', bytes: 1.9 * 1024 ** 3, protected: false },
                    { name: 'ops_audit', charset: 'utf8mb4', collation: 'utf8mb4_general_ci', bytes: 420 * 1024 ** 2, protected: false },
                    { name: 'mysql', charset: 'utf8mb4', collation: 'utf8mb4_general_ci', bytes: 18 * 1024 ** 2, protected: true },
                  ],
                  users: [
                    {
                      name: 'app_writer',
                      host: '%',
                      protected: false,
                      grants: [
                        { database: 'app_production', privileges: ['SELECT', 'INSERT', 'UPDATE', 'DELETE'] },
                        { database: 'billing_ledger', privileges: ['SELECT', 'INSERT', 'UPDATE'] },
                      ],
                    },
                    {
                      name: 'bi_readonly',
                      host: '10.24.%',
                      protected: false,
                      grants: [
                        { database: 'app_production', privileges: ['SELECT'] },
                        { database: 'ops_audit', privileges: ['SELECT'] },
                      ],
                    },
                    {
                      name: 'root',
                      host: 'localhost',
                      protected: true,
                      grants: [{ database: '*', privileges: ['ALL PRIVILEGES'] }],
                    },
                  ],
                },
              },
              {
                node_id: 'node-sg-01',
                instance_id: 'galera-sg',
                name: 'mariadb-sg-01',
                address: '10.24.0.24:3306',
                status: {
                  state: 'ready',
                  version: '11.4.3-MariaDB-log',
                  cluster_id: 'bb82f190-64c1-11ef-9a12-0242ac160002',
                  cluster_name: 'prod-galera-core',
                  cluster_size: 3,
                  primary: true,
                  ready: true,
                  synced: true,
                  read_only: false,
                  connections: 31,
                  receive_queue: 0,
                  send_queue: 0,
                  flow_control: 0,
                  checked_at: iso(-1),
                  read: { state: 'ready', data_at: iso(-1), attempted_at: iso(0) },
                },
              },
              {
                node_id: 'node-tyo-01',
                instance_id: 'galera-tyo',
                name: 'mariadb-tyo-01',
                address: '10.24.0.88:3306',
                status: {
                  state: 'ready',
                  version: '11.4.3-MariaDB-log',
                  cluster_id: 'bb82f190-64c1-11ef-9a12-0242ac160002',
                  cluster_name: 'prod-galera-core',
                  cluster_size: 3,
                  primary: true,
                  ready: true,
                  synced: true,
                  read_only: false,
                  connections: 19,
                  receive_queue: 0,
                  send_queue: 0,
                  flow_control: 0,
                  checked_at: iso(-1),
                  read: { state: 'ready', data_at: iso(-1), attempted_at: iso(0) },
                },
              },
            ],
            proxies: [
              {
                node_id: 'node-hk-01',
                instance_id: 'proxysql-hk',
                name: 'proxysql-edge-hk',
                address: '10.24.0.10:6033',
                status: {
                  state: 'ready',
                  version: '2.6.3-percona',
                  checked_at: iso(-1),
                  read: { state: 'ready', data_at: iso(-1), attempted_at: iso(0) },
                  pools: [
                    { hostgroup: 10, host: '10.24.0.10', port: 3306, used: 12, free: 28, errors: 0 },
                    { hostgroup: 20, host: '10.24.0.24', port: 3306, used: 8, free: 24, errors: 0 },
                    { hostgroup: 20, host: '10.24.0.88', port: 3306, used: 5, free: 19, errors: 0 },
                  ],
                  proxy_users: [
                    { name: 'app_writer', active: true, hostgroup: 10, state: 'in_sync', protected: false },
                    { name: 'bi_readonly', active: true, hostgroup: 20, state: 'in_sync', protected: false },
                  ],
                },
              },
              {
                node_id: 'node-sg-01',
                instance_id: 'proxysql-sg',
                name: 'proxysql-edge-sg',
                address: '10.24.0.24:6033',
                status: {
                  state: 'ready',
                  version: '2.6.3-percona',
                  checked_at: iso(-1),
                  read: { state: 'ready', data_at: iso(-1), attempted_at: iso(0) },
                  pools: [
                    { hostgroup: 10, host: '10.24.0.24', port: 3306, used: 14, free: 26, errors: 0 },
                    { hostgroup: 20, host: '10.24.0.10', port: 3306, used: 6, free: 22, errors: 0 },
                  ],
                  proxy_users: [
                    { name: 'app_writer', active: true, hostgroup: 10, state: 'in_sync', protected: false },
                    { name: 'bi_readonly', active: true, hostgroup: 20, state: 'in_sync', protected: false },
                  ],
                },
              },
            ],
          };

          if ((path === '/databases' || path === '/databases/clusters') && method === 'GET') {
            return sendJson(200, {
              items: [galeraClusterStatus],
            });
          }

          if (path === '/databases/clusters' && method === 'POST') {
            const body = await readBody();
            return sendJson(201, {
              ...galeraClusterStatus.cluster,
              ...body,
              id: body.id || 'cluster-prod-galera',
              revision: galeraClusterStatus.cluster.revision + 1,
              deleted: false,
              updated_at: iso(0),
            });
          }

          const dbClusterMatch = path.match(/^\/databases\/clusters\/([^/]+)$/);
          if (dbClusterMatch && method === 'GET') {
            return sendJson(200, galeraClusterStatus);
          }
          if (dbClusterMatch && method === 'PATCH') {
            const body = await readBody();
            return sendJson(200, {
              ...galeraClusterStatus.cluster,
              ...body,
              revision: galeraClusterStatus.cluster.revision + 1,
              deleted: false,
              updated_at: iso(0),
            });
          }
          if (dbClusterMatch && method === 'DELETE') {
            res.writeHead(204);
            return res.end();
          }

          const dbChangesMatch = path.match(/^\/databases\/clusters\/([^/]+)\/changes$/);
          if (dbChangesMatch && method === 'POST') {
            const op = {
              id: `op-db-${Math.random().toString(16).slice(2, 6)}`,
              node_id: 'node-hk-01',
              kind: 'database_change',
              state: 'succeeded',
              revision: 1,
              created_at: iso(0),
              updated_at: iso(0),
              finished_at: iso(0),
              steps: [
                {
                  name: '应用数据库与 ProxySQL 变更',
                  state: 'succeeded',
                  started_at: iso(0),
                  finished_at: iso(0),
                },
              ],
            };
            operationsList = [op, ...operationsList];
            return sendJson(202, op);
          }

          const dbBackupsMatch = path.match(/^\/databases\/clusters\/([^/]+)\/backups$/);
          if (dbBackupsMatch && method === 'GET') {
            return sendJson(200, {
              checked_at: iso(-5),
              items: [
                {
                  node_id: 'node-hk-01',
                  instance_id: 'galera-hk',
                  items: [
                    {
                      name: 'daily-mariabackup-full',
                      schedule: '0 3 * * *',
                      state: 'succeeded',
                      last_run: iso(-210),
                      last_success: iso(-210),
                      artifacts: ['mariabackup-2026-11-10T03-00Z.xbstream.zst (2.1 GB)'],
                    },
                  ],
                },
                {
                  node_id: 'node-tyo-01',
                  instance_id: 'galera-tyo',
                  items: [
                    {
                      name: 'hourly-incremental',
                      schedule: '15 * * * *',
                      state: 'succeeded',
                      last_run: iso(-35),
                      last_success: iso(-35),
                      artifacts: ['inc-2026-11-10T09-15Z.xbstream.zst (184 MB)'],
                    },
                  ],
                },
              ],
            });
          }

          const discoverDbMatch = path.match(/^\/nodes\/([^/]+)\/databases\/discover$/);
          if (discoverDbMatch && method === 'POST') {
            const nodeId = decodeURIComponent(discoverDbMatch[1]);
            return sendJson(200, {
              items: [
                {
                  id: `galera-${nodeId.split('-')[1]}`,
                  node_id: nodeId,
                  name: `mariadb-${nodeId.split('-')[1]}-01`,
                  kind: 'galera',
                  address: '127.0.0.1:3306',
                  username: 'root',
                  credentials_available: true,
                  running: true,
                  status: {
                    state: 'ready',
                    cluster_id: 'bb82f190-64c1-11ef-9a12-0242ac160002',
                    checked_at: iso(0),
                    read: { state: 'ready', data_at: iso(0), attempted_at: iso(0) },
                  },
                },
              ],
            });
          }

          const dbInstancePutMatch = path.match(/^\/nodes\/([^/]+)\/databases\/instances\/([^/]+)$/);
          if (dbInstancePutMatch && method === 'PUT') {
            const nodeId = decodeURIComponent(dbInstancePutMatch[1]);
            const instanceId = decodeURIComponent(dbInstancePutMatch[2]);
            const body = await readBody();
            return sendJson(200, {
              id: instanceId,
              node_id: nodeId,
              name: body.name || instanceId,
              kind: body.kind || 'galera',
              address: body.address || '127.0.0.1:3306',
              username: body.username || 'root',
              credentials_available: true,
              running: true,
              status: {
                state: 'ready',
                cluster_id: 'bb82f190-64c1-11ef-9a12-0242ac160002',
                checked_at: iso(0),
                read: { state: 'ready', data_at: iso(0), attempted_at: iso(0) },
              },
            });
          }

          const fwConfigMatch = path.match(/^\/nodes\/([^/]+)\/firewall(?:\/config)?$/);
          if (fwConfigMatch && method === 'GET') {
            return sendJson(200, {
              backend: 'nftables',
              revision: 5,
              confirmed_revision: 5,
              state: 'in_sync',
              service_active: true,
              checked_at: iso(-1),
              capabilities,
              rules: firewallRules,
              default_policies: defaultPolicies,
            });
          }
          if (fwConfigMatch && method === 'PATCH') {
            const nodeId = decodeURIComponent(fwConfigMatch[1]);
            const body = await readBody();
            const change = body.change || {};
            if (change.kind === 'add_rule' && change.rule) {
              firewallRules = [
                ...firewallRules,
                {
                  id: `rule-${Math.random().toString(16).slice(2, 6)}`,
                  chain: change.chain || 'input',
                  priority: change.priority ?? firewallRules.length + 1,
                  spec: change.rule,
                },
              ];
            } else if (change.kind === 'edit_rule' && change.rule_id && change.rule) {
              firewallRules = firewallRules.map((r) => (r.id === change.rule_id ? { ...r, spec: change.rule } : r));
            } else if (change.kind === 'delete_rule' && change.rule_id) {
              firewallRules = firewallRules.filter((r) => r.id !== change.rule_id);
            }
            const opId = `op-fw-${Math.random().toString(16).slice(2, 6)}`;
            const op = {
              id: opId,
              node_id: nodeId,
              kind: 'firewall_apply',
              state: 'succeeded',
              revision: 1,
              created_at: iso(0),
              updated_at: iso(0),
              finished_at: iso(0),
              steps: [
                {
                  name: '应用并确认 nftables 规则集',
                  state: 'succeeded',
                  started_at: iso(0),
                  finished_at: iso(0),
                },
              ],
            };
            operationsList = [op, ...operationsList];
            return sendJson(202, {
              operation_id: opId,
              configuration: {
                backend: 'nftables',
                revision: 6,
                confirmed_revision: 6,
                state: 'in_sync',
                service_active: true,
                checked_at: iso(0),
                capabilities,
                rules: firewallRules,
                default_policies: defaultPolicies,
              },
            });
          }

          const fwActionMatch = path.match(/^\/nodes\/([^/]+)\/firewall\/(sync|install)$/);
          if (fwActionMatch && method === 'POST') {
            const nodeId = decodeURIComponent(fwActionMatch[1]);
            const op = {
              id: `op-fw-${Math.random().toString(16).slice(2, 6)}`,
              node_id: nodeId,
              kind: `firewall_${fwActionMatch[2]}`,
              state: 'succeeded',
              revision: 1,
              created_at: iso(0),
              updated_at: iso(0),
              finished_at: iso(0),
              steps: [
                {
                  name: '同步防火墙规则状态',
                  state: 'succeeded',
                  started_at: iso(0),
                  finished_at: iso(0),
                },
              ],
            };
            operationsList = [op, ...operationsList];
            return sendJson(202, op);
          }

          const fwStateMatch = path.match(/^\/nodes\/([^/]+)\/firewall\/state$/);
          if (fwStateMatch && method === 'POST') {
            return sendJson(200, {
              collected_at: iso(0),
              complete: true,
              backend: 'nftables',
              service_active: true,
              capabilities,
              rules: firewallRules.map((r, i) => ({
                id: r.id,
                spec: r.spec,
                scope: 'both',
                native: { chain: r.chain, position: i + 1, priority: r.priority },
                native_expression: `ip saddr ${r.spec.source} ${r.spec.protocol} dport { ${r.spec.port} } accept`,
                editable: true,
                system_default: false,
                origin: 'native',
              })),
              default_policies: defaultPolicies.map((p) => ({
                ...p,
                scope: 'both',
                editable: true,
              })),
              docker_ports: [
                {
                  policy: {
                    target: { host_address: '0.0.0.0', protocol: 'tcp', port: 443 },
                    mode: 'unrestricted',
                    sources: [],
                  },
                  container_ids: ['ingress-caddy'],
                  state: 'applied',
                  coverage: [],
                  peer_trust_exception: false,
                },
                {
                  policy: {
                    target: { host_address: '10.24.0.10', protocol: 'tcp', port: 6033 },
                    mode: 'allow_sources',
                    sources: ['10.24.0.0/16'],
                  },
                  container_ids: ['proxysql-edge'],
                  state: 'applied',
                  coverage: [],
                  peer_trust_exception: true,
                },
              ],
              errors: [],
            });
          }

          if (path === '/operations/pending' && method === 'GET') {
            return sendJson(200, { items: [] });
          }

          if (path === '/operations' && method === 'GET') {
            const nodeId = url.searchParams.get('node_id');
            const items = nodeId ? operationsList.filter((o) => o.node_id === nodeId) : operationsList;
            return sendJson(200, { items });
          }

          const opDetailMatch = path.match(/^\/operations\/([^/]+)$/);
          if (opDetailMatch && method === 'GET') {
            const opId = decodeURIComponent(opDetailMatch[1]);
            const found = operationsList.find((o) => o.id === opId) || operationsList[0];
            return sendJson(200, found);
          }

          const opActionMatch = path.match(/^\/operations\/([^/]+)\/(confirm|recover|resolve|cancel)$/);
          if (opActionMatch && method === 'POST') {
            const opId = decodeURIComponent(opActionMatch[1]);
            const found = operationsList.find((o) => o.id === opId) || operationsList[0];
            return sendJson(200, { ...found, state: 'succeeded', updated_at: iso(0), finished_at: iso(0) });
          }

          if (path === '/api-tokens' && method === 'GET') {
            return sendJson(200, { items: apiTokens });
          }

          if (path === '/api-tokens/verify' && method === 'GET') {
            return sendJson(200, apiTokens[0]);
          }

          if (path === '/api-tokens' && method === 'POST') {
            const body = await readBody();
            const created = {
              ...body,
              id: `tok-${Math.random().toString(16).slice(2, 8)}`,
              owner: 'admin',
              revision: 1,
              created_at: iso(0),
            };
            apiTokens = [created, ...apiTokens];
            return sendJson(201, {
              token: created,
              value: `opsd_pat_${Math.random().toString(36).slice(2)}${Math.random().toString(36).slice(2)}`,
            });
          }

          const apiTokenRegenMatch = path.match(/^\/api-tokens\/([^/]+)\/regenerate$/);
          if (apiTokenRegenMatch && method === 'POST') {
            const id = decodeURIComponent(apiTokenRegenMatch[1]);
            apiTokens = apiTokens.map((t) => (t.id === id ? { ...t, revision: t.revision + 1 } : t));
            const updated = apiTokens.find((t) => t.id === id) || apiTokens[0];
            return sendJson(200, {
              token: updated,
              value: `opsd_pat_${Math.random().toString(36).slice(2)}${Math.random().toString(36).slice(2)}`,
            });
          }

          const apiTokenMatch = path.match(/^\/api-tokens\/([^/]+)$/);
          if (apiTokenMatch && (method === 'PUT' || method === 'PATCH')) {
            const id = decodeURIComponent(apiTokenMatch[1]);
            const body = await readBody();
            apiTokens = apiTokens.map((t) => (t.id === id ? { ...t, ...body, revision: t.revision + 1 } : t));
            return sendJson(200, apiTokens.find((t) => t.id === id) || apiTokens[0]);
          }
          if (apiTokenMatch && method === 'DELETE') {
            const id = decodeURIComponent(apiTokenMatch[1]);
            apiTokens = apiTokens.filter((t) => t.id !== id);
            res.writeHead(204);
            return res.end();
          }

          return sendJson(200, {});
        })();
      });
    },
  };
}

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    demoApiPlugin(),
    {
      name: 'opsd-theme',
      transformIndexHtml() {
        return [{ tag: 'script', children: getThemeScript(themeOptions), injectTo: 'head-prepend' }];
      },
    },
  ],
  server: { host: '127.0.0.1' },
});

