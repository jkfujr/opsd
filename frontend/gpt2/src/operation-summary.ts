import type { components } from './api/types';

type Operation = components['schemas']['Operation'];
export const operationSubjectNames: Record<Operation['subject'], string> = { task_run: '执行任务', task_save: '保存任务', name: '修改节点名称', config: '修改节点配置', account: '修改节点账号', firewall: '应用防火墙变更', firewall_config: '更新防火墙配置', container: '容器操作', component: '组件操作', compose: 'Compose 项目操作', compose_register: '登记 Compose 项目', database: '数据库变更', database_step: '数据库执行步骤' };

export function operationTitle(operation: Operation) {
  const names: Record<string, Record<string, string>> = {
    container: { start: '启动容器', stop: '停止容器', restart: '重启容器', update: '更新容器镜像' },
    compose: { pull: '拉取项目镜像', start: '启动 Compose 项目', stop: '停止 Compose 项目', recreate: '重建 Compose 项目', update: '更新 Compose 项目' },
    database: { create_database: '创建数据库', delete_database: '删除数据库', create_user: '创建数据库账号', password: '修改数据库密码', delete_user: '删除数据库账号', grant: '授予库权限', revoke: '撤销库权限', sync_user: '同步代理账号', configure_proxy_routes: '应用代理路由' },
    database_step: { proxy_route: '应用代理路由', proxy_upsert: '同步代理账号', proxy_disable: '停用代理账号', proxy_delete: '删除代理账号' },
    firewall_config: { adopt_current: '接纳当前防火墙配置' }, component: { install: '安装组件' },
    firewall: { add_rule: '添加防火墙规则', edit_rule: '修改防火墙规则', delete_rule: '删除防火墙规则', move_rule: '调整防火墙顺序', set_default_policy: '修改默认入站策略', set_enabled: '启停防火墙' },
  };
  return names[operation.subject]?.[operation.action ?? ''] ?? operationSubjectNames[operation.subject];
}

export function operationTarget(operation: Operation): string | undefined {
  return operation.target_name || undefined;
}
