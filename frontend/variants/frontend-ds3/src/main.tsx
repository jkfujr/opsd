import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { queryClient } from './state';
import { apply } from './lib/world';
import { App } from './App';
import './styles/base.css';
import './styles/shell.css';
import './styles/parts.css';
import './styles/pages.css';

apply();

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
);
