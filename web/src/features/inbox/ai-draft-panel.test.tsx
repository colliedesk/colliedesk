import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { aiDraftApi } from './ai-draft-api';
import type { AIDraft } from './ai-draft-api';
import { AIDraftPanel } from './ai-draft-panel';

const conversation = '22222222-2222-4222-8222-222222222222';
const draft: AIDraft = {
  id: '11111111-1111-4111-8111-111111111111', conversation_id: conversation, message_id: null, status: 'ready', action: 'answer',
  body: 'Refundable within 30 days.', confidence: 'high', confidence_reasons: [], handoff_reason: null, topic_article_id: null,
  sources: [], triage: null, triage_applied_at: null, outcome: null, similarity: null, auto_send_at: null, error_code: null, model: null,
  created_at: '2026-10-22T10:00:00Z', ready_at: '2026-10-22T10:00:04Z',
};

function show(latestMessageId = 'm1') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><AIDraftPanel conversationId={conversation} latestMessageId={latestMessageId} timezone="UTC" /></QueryClientProvider>);
  return { client, ...view };
}

afterEach(() => vi.restoreAllMocks());

test('shows nothing on a server without AI', async () => {
  const get = vi.spyOn(aiDraftApi, 'get').mockResolvedValue({ draft: null, available: false, drafts_enabled: true });
  const { container } = show();
  await waitFor(() => expect(get).toHaveBeenCalled());
  expect(container).toBeEmptyDOMElement();
});

test('a ready draft is sent as a reply that names it, and the timeline is read again', async () => {
  vi.spyOn(aiDraftApi, 'get').mockResolvedValue({ draft, available: true, drafts_enabled: true });
  const send = vi.spyOn(aiDraftApi, 'send').mockResolvedValue({} as never);
  const { client } = show();
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  fireEvent.click(await screen.findByRole('button', { name: 'Send' }));
  await waitFor(() => expect(send).toHaveBeenCalledWith(conversation, draft.id, 'Refundable within 30 days.'));
  await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['messages', conversation] }));
});

test('a draft already sent or replaced is not shown, and a person can ask for one', async () => {
  vi.spyOn(aiDraftApi, 'get').mockResolvedValue({ draft: { ...draft, status: 'used' }, available: true, drafts_enabled: true });
  const ask = vi.spyOn(aiDraftApi, 'requestNew').mockResolvedValue({ draft: { ...draft, status: 'queued' } });
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Ask AI for a draft' }));
  await waitFor(() => expect(ask).toHaveBeenCalledWith(conversation));
  expect(screen.queryByText('Refundable within 30 days.')).not.toBeInTheDocument();
});
