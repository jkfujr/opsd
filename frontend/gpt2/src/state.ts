import { QueryClient } from '@tanstack/react-query';
export const queryClient = new QueryClient({ defaultOptions: {
  queries: { retry: (count, error) => count < 1 && (!('status' in error) || Number(error.status) >= 500), staleTime: 30000, structuralSharing: false },
  mutations: { retry: false },
} });
