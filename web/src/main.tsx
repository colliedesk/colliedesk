import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { router } from './app';
import { createQueryClient } from './query-client';
import { applyStoredTheme } from './lib/theme';
import './styles/base.css';
import './styles/layout.css';
import './styles/components.css';

applyStoredTheme();
const queryClient = createQueryClient();
const element = document.getElementById('root');
if (!element) throw new Error('Missing application root');

createRoot(element).render(<StrictMode><QueryClientProvider client={queryClient}>
  <RouterProvider router={router} />
</QueryClientProvider></StrictMode>);
