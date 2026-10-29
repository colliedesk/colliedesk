import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { useBillingShown } from './billing-shown';

const { api } = vi.hoisted(() => ({ api: { config: vi.fn(), billing: vi.fn() } }));
vi.mock('../../api/client', () => ({ api }));

function Shown({ admin }: { admin: boolean }) {
  return <p>{useBillingShown(admin) ? 'shown' : 'hidden'}</p>;
}

function show(admin: boolean, billing: { enabled: boolean; switched_off: boolean }, status?: string) {
  api.config.mockResolvedValue({ billing });
  api.billing.mockResolvedValue({ status: status ?? 'none' });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><Shown admin={admin} /></QueryClientProvider>);
}

test('billing on: shown to an admin', async () => {
  show(true, { enabled: true, switched_off: false });
  expect(await screen.findByText('shown')).toBeInTheDocument();
});

test('billing switched off: shown only to a workspace that has or had a subscription, so it can be cancelled', async () => {
  show(true, { enabled: false, switched_off: true }, 'canceled');
  expect(await screen.findByText('shown')).toBeInTheDocument();
});

test('billing switched off and never paid, or billing not set up: hidden, and the plan is not asked for without a switch', async () => {
  show(true, { enabled: false, switched_off: true }, 'none');
  await vi.waitFor(() => expect(api.billing).toHaveBeenCalled());
  expect(screen.getByText('hidden')).toBeInTheDocument();
  api.billing.mockClear();
  show(true, { enabled: false, switched_off: false });
  expect(screen.getAllByText('hidden').length).toBeGreaterThan(0);
  expect(api.billing).not.toHaveBeenCalled();
});
