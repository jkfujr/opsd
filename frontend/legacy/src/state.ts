import { QueryClient } from '@tanstack/react-query';

export const queryClient = new QueryClient({
  defaultOptions: {
    // 保留响应对象，使配置 ETag 始终对应该次读取。
    queries: { retry: 1, staleTime: 30000, structuralSharing: false },
    mutations: { retry: false },
  },
});
