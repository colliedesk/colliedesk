import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { StartConversationWindow } from './new-conversation-dialog';

const mocks = vi.hoisted(() => ({
  api: { me: vi.fn(), config: vi.fn() },
  channelsApi: { list: vi.fn(), email: vi.fn() },
}));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
vi.mock('../../api/client', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../api/client')>()), api: mocks.api }));
vi.mock('../../api/channels', () => ({ channelsApi: mocks.channelsApi }));

const EMAIL = '55555555-5555-4555-8555-555555555555';
const MiB = 1024 * 1024;
const microsoftConnection = { provider: 'microsoft', address: 'support@example.com', status: 'connected', error_code: null, error: null, failed_at: null, connected_at: '2026-10-25T00:00:00Z' };

test('a new conversation that leaves through Microsoft 365 warns about large files, like the reply box', async () => {
  mocks.api.me.mockResolvedValue({ plan: { plan: 'team' } });
  mocks.api.config.mockResolvedValue({ limits: { outgoing_attachments_bytes: 25 * MiB, outgoing_attachments_files: 10 } });
  mocks.channelsApi.list.mockResolvedValue({ items: [{ id: EMAIL, type: 'email', name: 'Email', enabled: true, deleted_at: null }],
    types: [{ type: 'email', default_channel_id: EMAIL }] });
  mocks.channelsApi.email.mockResolvedValue({ channel: { support_address: 'support@example.com', domain_verified_at: null },
    microsoft: { large_attachment_bytes: 3 * MiB, connection: microsoftConnection } });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <StartConversationWindow onClose={vi.fn()} onStarted={vi.fn()} /></QueryClientProvider>);
  const input = await screen.findByLabelText('Attach files');
  await vi.waitFor(() => expect(mocks.channelsApi.email).toHaveBeenCalledWith(EMAIL));
  fireEvent.change(input, { target: { files: [new File([new Uint8Array(4 * MiB)], 'large.pdf')] } });
  expect(await screen.findByRole('status')).toHaveTextContent('Microsoft 365 may refuse attachments this large');
});
