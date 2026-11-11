import type { QueryClient } from '@tanstack/react-query';
import type { components } from './api/types';

type Operation = components['schemas']['Operation'];
export const operationSubjectNames: Record<Operation['subject'], string> = { task_run: '执行任务', task_save: '保存任务', name: '修改节点名称', config: '修改节点配置', account: '修改节点账号', firewall: '应用防火墙变更', firewall_config: '更新防火墙配置', container: '容器操作', component: '组件操作', compose: 'Compose 项目操作', compose_register: '登记 Compose 项目', database: '数据库变更', database_step: '数据库执行步骤' };

export function operationTarget(operation: Operation, client: QueryClient): string | undefined {
  const { resource_id: id, node_id: node, subject } = operation;
  if (!id) return;
  if (subject === 'task_run' || subject === 'task_save') {
    for (const [, value] of client.getQueriesData<components['schemas']['ScheduledTaskList']>({ queryKey: ['tasks'] })) {
      const task = value?.items?.find(item => item.node_id === node && item.id === id);
      if (task) return task.name;
    }
  }
  if (subject === 'container') {
    for (const [, value] of client.getQueriesData<components['schemas']['ContainerList']>({ queryKey: ['containers'] })) {
      const container = value?.items?.find(item => item.node_id === node && item.container_id === id);
      if (container) return container.name;
    }
  }
  if (subject === 'compose' || subject === 'compose_register') {
    for (const [, value] of client.getQueriesData<components['schemas']['ComposeProjectList']>({ queryKey: ['compose'] })) {
      const project = value?.items?.find(item => item.node_id === node && item.id === id);
      if (project) return project.name;
    }
  }
  if (subject === 'database' || subject === 'database_step') {
    for (const [, value] of client.getQueriesData<components['schemas']['ManagedDatabaseList']>({ queryKey: ['databases', 'list'] })) {
      const cluster = value?.items?.find(item => item.cluster.id === id);
      if (cluster) return cluster.cluster.name;
    }
    const detail = client.getQueryData<components['schemas']['ManagedDatabaseStatus']>(['databases', 'detail', id, 'health']);
    if (detail) return detail.cluster.name;
  }
  return id.length <= 64 && !/^[a-f\d-]{24,}$/i.test(id) ? id : undefined;
}
