import React from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider } from '@appica/ui-react/providers/theme-provider';
import { App } from './App';
import './styles.css';
import { queryClient } from './state';
import { themeOptions } from './theme';

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider {...themeOptions} disableTransitionOnChange>
        <App />
      </ThemeProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
