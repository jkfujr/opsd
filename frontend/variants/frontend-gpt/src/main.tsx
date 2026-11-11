import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '@appica/ui-react/providers/theme-provider';
import { App } from './App';
import { DemoProvider } from './store';
import { themeOptions } from './theme';
import { usePalette } from './palettes';
import './styles.css';

function Root() {
  usePalette();
  return <ThemeProvider {...themeOptions}><DemoProvider><App/></DemoProvider></ThemeProvider>;
}

createRoot(document.getElementById('root')!).render(<StrictMode><Root/></StrictMode>);
