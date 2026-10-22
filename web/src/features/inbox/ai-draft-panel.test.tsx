import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { AIDraftPanel } from './ai-draft-panel';
import type { AIDraft } from './ai-draft-api';
import { ReplyComposer } from './reply-composer';

const conversation = '22222222-2222-4222-8222-222222222222';
const agent = '33333333-3333-4333-8333-333333333333';
const article = '44444444-4444-4444-8444-444444444444';

function draft(change: Partial<AIDraft> = {}): AIDraft {
  return { id: '55555555-5555-4555-8555-555555555555', conversation_id: conversation, status: 'ready', error_code: null, prompt: '', text: 'Hi Ada, open tray two.',
    missing_info: 'Which printer model?', sources: [{ kind: 'kb_article', id: article, number: 4, title: 'Printer paper jams' }],
    warnings: [{ kind: 'email', value: 'grace@other.example' }], model: 'claude-sonnet-5-5', outcome: null, similarity: null, sent_message_id: null,
    requested_by_agent_id: agent, created_at: '2026-10-22T10:00:00Z', ready_at: '2026-10-22T10:00:05Z', decided_at: null, ...change };
}

type Route = (url: string, init?: RequestInit) => unknown;

function serve(route: Route) {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    const body = route(url, init);
    if (body instanceof Response) return body;
    return new Response(JSON.stringify(body), { status: url.endsWith('/ai-drafts') && init?.method === 'POST' ? 202 : 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

afterEach(() => vi.unstubAllGlobals());

const wrap = (node: React.ReactNode) => <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>;

test('a ready draft shows what to check, what is missing and its sources, and goes into the box', async () => {
  serve(() => ({ items: [draft()] }));
  const onUse = vi.fn();
  render(wrap(<AIDraftPanel conversationId={conversation} agentId={agent} hasText onUse={onUse} onClose={vi.fn()} />));
  expect(await screen.findByText('Hi Ada, open tray two.')).toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent('Check before sending');
  expect(screen.getByRole('alert')).toHaveTextContent('grace@other.example');
  expect(screen.getByText(/Which printer model\?/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: /Printer paper jams/ })).toHaveAttribute('href', `/knowledge/articles/${article}`);
  fireEvent.click(screen.getByRole('button', { name: 'Insert below' }));
  expect(onUse).toHaveBeenCalledWith('Hi Ada, open tray two.', 'below', draft().id, ['grace@other.example']);
  fireEvent.click(screen.getByRole('button', { name: 'Replace' }));
  expect(onUse).toHaveBeenLastCalledWith('Hi Ada, open tray two.', 'replace', draft().id, ['grace@other.example']);
});

test('asking for a draft sends the prompt and shows it being written until it is ready', async () => {
  let polls = 0;
  const fetch = serve((url, init) => {
    if (init?.method === 'POST') return draft({ status: 'queued', text: null, prompt: 'Offer a refund' });
    if (url.includes('?wait=')) return ++polls > 1 ? draft({ text: 'We refunded you.' }) : draft({ status: 'running', text: null });
    return { items: [] };
  });
  render(wrap(<AIDraftPanel conversationId={conversation} agentId={agent} hasText={false} onUse={vi.fn()} onClose={vi.fn()} />));
  fireEvent.change(await screen.findByLabelText('What should the reply say? (optional)'), { target: { value: 'Offer a refund' } });
  fireEvent.click(screen.getByRole('button', { name: 'Write draft' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Drafting a reply');
  expect(await screen.findByText('We refunded you.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Use draft' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Insert below' })).not.toBeInTheDocument();
  const create = fetch.mock.calls.find(([, init]) => init?.method === 'POST');
  expect(create?.[0]).toBe(`/api/v1/conversations/${conversation}/ai-drafts`);
  expect(JSON.parse(String(create?.[1]?.body))).toEqual({ prompt: 'Offer a refund' });
});

test('a draft that failed says why and can be asked for again; a refusal at once says why', async () => {
  serve((_url, init) => init?.method === 'POST'
    ? new Response(JSON.stringify({ error: { code: 'ai_feature_off', message: 'Reply drafts are off in this workspace.' } }), { status: 409 })
    : { items: [draft({ status: 'failed', text: null, error_code: 'refused', prompt: 'Be rude' })] });
  render(wrap(<AIDraftPanel conversationId={conversation} agentId={agent} hasText={false} onUse={vi.fn()} onClose={vi.fn()} />));
  // A failed draft is not picked up again on opening: the form is shown.
  fireEvent.click(await screen.findByRole('button', { name: 'Write draft' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Reply drafts are off in this workspace.');
});

test('the reply box offers Draft with AI only where drafts are on, and a reply from a draft names it', async () => {
  serve(() => ({ items: [draft()] }));
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  const { rerender } = render(wrap(<ReplyComposer onSubmit={onSubmit} />));
  expect(screen.queryByRole('button', { name: 'Draft with AI' })).not.toBeInTheDocument();
  rerender(wrap(<ReplyComposer onSubmit={onSubmit} aiDraftsFor={conversation} agentId={agent} />));
  fireEvent.click(screen.getByRole('button', { name: /^Draft with AI/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use draft' }));
  expect(screen.getByLabelText('Message body')).toHaveValue('Hi Ada, open tray two.');
  // What to check stays above the box until the reply is sent.
  expect(screen.getByRole('alert')).toHaveTextContent('grace@other.example');
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Hi Ada, open tray two please.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Hi Ada, open tray two please.', [], [], undefined, draft().id);
});

test('a reply whose draft is gone meanwhile is sent without it', async () => {
  serve(() => ({ items: [draft()] }));
  const { ConflictError } = await import('../../api/client');
  const onSubmit = vi.fn().mockRejectedValueOnce(new ConflictError('gone', { code: 'ai_draft_not_ready' })).mockResolvedValue(undefined);
  render(wrap(<ReplyComposer onSubmit={onSubmit} aiDraftsFor={conversation} agentId={agent} />));
  fireEvent.click(screen.getByRole('button', { name: /^Draft with AI/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use draft' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  await vi.waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2));
  expect(onSubmit).toHaveBeenLastCalledWith('outgoing', 'Hi Ada, open tray two.');
  await vi.waitFor(() => expect(screen.getByLabelText('Message body')).toHaveValue(''));
});

test('a draft refused because the included AI is used up says so in the panel, not as a locked workspace', async () => {
  serve((_url, init) => init?.method === 'POST'
    ? new Response(JSON.stringify({ error: { code: 'ai_allowance_reached', message: 'This month’s included AI is used up.' } }), { status: 402 })
    : { items: [] });
  render(wrap(<AIDraftPanel conversationId={conversation} agentId={agent} hasText={false} onUse={vi.fn()} onClose={vi.fn()} />));
  fireEvent.click(await screen.findByRole('button', { name: 'Write draft' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('This month’s included AI is used up.');
});

test('a draft stopped because the feature was turned off says why', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const stopped = draft({ status: 'cancelled', text: null, error_code: 'feature_off' });
  client.setQueryData(['ai-draft', conversation, stopped.id], stopped);
  serve((url) => (url.includes(stopped.id) ? stopped : { items: [{ ...stopped, status: 'queued' }] }));
  render(<QueryClientProvider client={client}><AIDraftPanel conversationId={conversation} agentId={agent} hasText={false} onUse={vi.fn()} onClose={vi.fn()} /></QueryClientProvider>);
  expect(await screen.findByRole('alert')).toHaveTextContent('Reply drafts were turned off');
});

test('a draft whose reading fails stops asking instead of asking in a loop', async () => {
  const fetch = serve((url) => (url.includes('?wait=') || url.endsWith(draft().id)
    ? new Response(JSON.stringify({ error: { code: 'internal', message: 'boom' } }), { status: 500 })
    : { items: [draft({ status: 'running', text: null })] }));
  render(wrap(<AIDraftPanel conversationId={conversation} agentId={agent} hasText={false} onUse={vi.fn()} onClose={vi.fn()} />));
  await screen.findByRole('status');
  await new Promise((resolve) => setTimeout(resolve, 400));
  const reads = fetch.mock.calls.filter(([url]) => String(url).includes(draft().id)).length;
  expect(reads).toBeLessThanOrEqual(2);
  expect(await screen.findByRole('alert')).toHaveTextContent('The draft could not be loaded');
});

test('the button of the closed panel says a draft is ready', async () => {
  serve(() => ({ items: [draft()] }));
  render(wrap(<ReplyComposer onSubmit={vi.fn()} aiDraftsFor={conversation} agentId={agent} />));
  expect(await screen.findByRole('button', { name: /^Draft with AI,\s*Draft ready$/ })).toBeInTheDocument();
});

test('the button says nothing about a draft once it is in the box, and follows a draft that became ready', async () => {
  let ready = false;
  serve((url) => (url.includes('?wait=') ? (ready = true, draft()) : { items: [ready ? draft() : draft({ status: 'running', text: null })] }));
  render(wrap(<ReplyComposer onSubmit={vi.fn()} aiDraftsFor={conversation} agentId={agent} />));
  fireEvent.click(await screen.findByRole('button', { name: /^Draft with AI/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use draft' }));
  expect(screen.getByRole('button', { name: 'Draft with AI' })).toBeInTheDocument();
});
