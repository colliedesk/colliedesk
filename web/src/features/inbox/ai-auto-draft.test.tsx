import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { AutoDraftCard } from './ai-auto-draft';
import type { AIDraft } from './ai-draft-api';

const conversation = '22222222-2222-4222-8222-222222222222';

function draft(change: Partial<AIDraft> = {}): AIDraft {
  return { id: '55555555-5555-4555-8555-555555555555', conversation_id: conversation, status: 'ready', error_code: null, prompt: '', text: 'Hi Jordan, I refunded INV-20932.',
    missing_info: null, sources: [], warnings: [], model: 'claude-sonnet-5-5', outcome: null, similarity: null, sent_message_id: null, requested_by_agent_id: null,
    automatic: true, answers_message_id: '66666666-6666-4666-8666-666666666666', reply_needed: true, created_at: '2026-10-22T10:31:00Z', ready_at: '2026-10-22T10:31:20Z',
    decided_at: null, ...change };
}

function serve(items: AIDraft[], onDiscard = vi.fn()) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/discard')) { onDiscard(); return new Response(JSON.stringify(items[0]), { status: 200, headers: { 'Content-Type': 'application/json' } }); }
    return new Response(JSON.stringify({ items }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  }));
}

afterEach(() => vi.unstubAllGlobals());

const show = (props: { hasText?: boolean; onUse?: Parameters<typeof AutoDraftCard>[0]['onUse']; canManage?: boolean } = {}) => render(
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <AutoDraftCard conversationId={conversation} customerName="Jordan" timezone="UTC" hasText={props.hasText ?? false} canManage={props.canManage}
      onUse={props.onUse ?? vi.fn()} />
  </QueryClientProvider>);

test('a draft AI wrote for the customer waits above the reply box, and Use draft puts it in', async () => {
  serve([draft()]);
  const onUse = vi.fn();
  show({ onUse });
  expect(await screen.findByText(/Draft for Jordan’s message of/)).toBeInTheDocument();
  expect(screen.getByText('Written automatically · your team sees it')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Use draft' }));
  expect(onUse).toHaveBeenCalledWith('Hi Jordan, I refunded INV-20932.', 'replace', draft().id, []);
  // Once in the reply box it is not offered again.
  expect(screen.getByText('In your reply box. Edit it there, then send it.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Use draft' })).not.toBeInTheDocument();
});

test('Hide folds the draft to one line that still has its buttons; Show draft opens it', async () => {
  serve([draft()]);
  const onUse = vi.fn();
  show({ onUse });
  fireEvent.click(await screen.findByRole('button', { name: 'Hide' }));
  expect(screen.queryByText('Hi Jordan, I refunded INV-20932.')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Show draft' })).toHaveAttribute('aria-expanded', 'false');
  expect(screen.getByRole('button', { name: 'Show draft' })).toHaveFocus();
  fireEvent.click(screen.getByRole('button', { name: 'Show draft' }));
  expect(screen.getByText('Hi Jordan, I refunded INV-20932.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Hide' })).toHaveAttribute('aria-expanded', 'true');
  expect(screen.getByRole('button', { name: 'Hide' })).toHaveFocus();
  fireEvent.click(screen.getByRole('button', { name: 'Hide' }));
  fireEvent.click(screen.getByRole('button', { name: 'Use draft' }));
  expect(onUse).toHaveBeenCalledWith('Hi Jordan, I refunded INV-20932.', 'replace', draft().id, []);
});

test('what AI could not find is shown with the draft', async () => {
  serve([draft({ missing_info: 'Whether the printer is under warranty.' })]);
  show();
  expect(await screen.findByText('Whether the printer is under warranty.')).toBeInTheDocument();
  expect(screen.getByText('Not found by AI:')).toBeInTheDocument();
});

test('a newer draft stopped at a limit leaves the earlier one usable', async () => {
  serve([draft({ id: '77777777-7777-4777-8777-777777777777', status: 'cancelled', error_code: 'conversation_cap', text: null, ready_at: null }), draft()]);
  show();
  expect(await screen.findByText('No automatic draft for this message:')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Use draft' })).toBeInTheDocument();
});

test('a failed draft can be tried again, which opens Draft with AI, or dismissed', async () => {
  const asked = vi.fn();
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === 'POST') { asked(); return new Response(JSON.stringify(draft({ automatic: false, status: 'queued', text: null })), { status: 201, headers: { 'Content-Type': 'application/json' } }); }
    return new Response(JSON.stringify({ items: [draft({ status: 'failed', error_code: 'refused', text: null })] }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  }));
  const onTryAgain = vi.fn();
  const { unmount } = render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <AutoDraftCard conversationId={conversation} timezone="UTC" hasText={false} onUse={vi.fn()} onTryAgain={onTryAgain} /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(onTryAgain).toHaveBeenCalled());
  expect(asked).toHaveBeenCalledTimes(1);
  unmount();
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Dismiss' }));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('with text in the reply box nothing is replaced until a person picks', async () => {
  serve([draft()]);
  const onUse = vi.fn();
  show({ hasText: true, onUse });
  expect(await screen.findByText('Your reply box has text, so nothing was replaced.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Replace my text' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Insert below' }));
  expect(onUse).toHaveBeenCalledWith(expect.any(String), 'below', draft().id, []);
  expect(screen.queryByRole('button', { name: 'Insert below' })).not.toBeInTheDocument();
});

test('while it is written, after the customer wrote again, and when no reply is needed', async () => {
  // The draft for the earlier message stays usable until the new one is written.
  serve([draft({ id: '77777777-7777-4777-8777-777777777777', status: 'queued', text: null, ready_at: null }), draft()]);
  const { unmount } = show();
  expect(await screen.findByText(/Jordan wrote again at/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Use draft' })).toBeInTheDocument();
  unmount();
  serve([draft({ reply_needed: false })]);
  show();
  expect(await screen.findByText('No reply needed')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Show draft' }));
  expect(screen.getByRole('button', { name: 'Use draft' })).toBeInTheDocument();
});

test('paused, capped, or failed: it says why and that Draft with AI still works; quiet when nobody needs it', async () => {
  serve([draft({ status: 'cancelled', error_code: 'allowance_reserved', text: null, ready_at: null })]);
  const { unmount } = show();
  expect(await screen.findByText(/they used their half of this month’s included AI/)).toBeInTheDocument();
  unmount();
  serve([draft({ status: 'failed', error_code: 'auth', text: null, ready_at: null })]);
  const failed = show({ canManage: true });
  expect(await screen.findByText(/the AI provider refused its key/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Open AI settings' })).toBeInTheDocument();
  failed.unmount();
  serve([draft({ status: 'cancelled', error_code: 'answered', text: null, ready_at: null })]);
  const { container } = show();
  await waitFor(() => expect(container.querySelector('.ai-auto-draft')).toBeNull());
});

test('discard takes it away', async () => {
  const onDiscard = vi.fn();
  serve([draft()], onDiscard);
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Discard' }));
  await waitFor(() => expect(onDiscard).toHaveBeenCalled());
});
