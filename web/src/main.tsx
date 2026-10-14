import '@fontsource-variable/geist';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { UnauthorizedError } from './api/client';
import { router } from './app';
import './styles/base.css';
import './styles/layout.css';
import './styles/components.css';

// A missing session is not a transient failure: show the signed-out state at once instead of retrying.
const queryClient = new QueryClient({ defaultOptions: { queries: {
  staleTime: 15_000, retry: (failures, error) => !(error instanceof UnauthorizedError) && failures < 1,
} } });
const element = document.getElementById('root');
if (!element) throw new Error('Missing application root');

createRoot(element).render(<StrictMode><QueryClientProvider client={queryClient}>
  <RouterProvider router={router} />
</QueryClientProvider></StrictMode>);
