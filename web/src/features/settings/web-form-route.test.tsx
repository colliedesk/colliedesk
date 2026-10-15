import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { SettingsRoute } from './settings-route';

const mocks = vi.hoisted(() => {
  class InvalidRequestError extends Error {}
  class PlanLimitError extends Error {}
  const api = {
    me: vi.fn(), sessions: vi.fn(), invites: vi.fn(), emailChannel: vi.fn(), webForm: vi.fn(), saveWebForm: vi.fn(),
  };
  return { api, InvalidRequestError, PlanLimitError };
});
vi.mock('../../api/client', () => ({ api: mocks.api, InvalidRequestError: mocks.InvalidRequestError, PlanLimitError: mocks.PlanLimitError }));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.api.me.mockResolvedValue({
    agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role: 'admin', timezone_override: null },
    workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC',
  });
  mocks.api.sessions.mockResolvedValue({ items: [] });
  mocks.api.invites.mockResolvedValue({ items: [] });
  mocks.api.emailChannel.mockResolvedValue(undefined);
  mocks.api.webForm.mockResolvedValue({ enabled: false, allowed_origins: [], submit_path: '/api/v1/public/forms/acme' });
});

test('a refused web form save shows the server message, not the generic failure', async () => {
  mocks.api.saveWebForm.mockRejectedValue(new mocks.InvalidRequestError('Invalid origin "*": use an exact origin such as https://example.com.'));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><SettingsRoute /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: /save web form/i }));
  await waitFor(() => expect(screen.getByText(/Invalid origin "\*": use an exact origin/)).toBeInTheDocument());
});
