import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, me, serve } from '../kb/kb.test.fixture';
import type { Fix } from './autofix-api';
import { AutofixConversation, AutofixStrip, targetOf } from './autofix-conversation';
import { connectorOf, conversationId, detailOf, fixOf, previewOf, repoOf, settingsOf, withPR } from './autofix.test.fixture';

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, '', '/');
});

function mount(fix: Fix, aiDraft = false, extra: Parameters<typeof serve>[0] = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(['me'], { ...me, ai_features: { draft: aiDraft, triage: false } });
  const calls = serve({
    [`GET /api/v1/conversations/${conversationId}/fixes`]: () => json(200, { items: [fix] }),
    [`GET /api/v1/autofix/fixes/${fix.id}`]: () => json(200, detailOf(fix)),
    'GET /api/v1/autofix/settings': () => json(200, settingsOf()),
    ...extra,
  });
  render(<QueryClientProvider client={client}><AutofixConversation conversationId={conversationId} customerName="Tomás Vidal" timezone="UTC" /></QueryClientProvider>);
  return calls;
}

describe('AutofixConversation', () => {
  it('marks a merged fix released with the version the person saw', async () => {
    const merged = fixOf({ state: 'merged', ...withPR });
    const calls = mount(merged, false, { [`PUT /api/v1/autofix/fixes/${merged.id}/deployed`]: () => json(200, { ...merged, state: 'deployed' }) });
    fireEvent.click(await screen.findByRole('button', { name: 'Mark as released' }));
    await waitFor(() => expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ row_version: 3 }));
  });

  it('puts a plain reply in this conversation’s box when AI drafts are off', async () => {
    const put = vi.fn();
    window.addEventListener('colliedesk:insert-reply', put);
    mount(fixOf({ state: 'deployed', ...withPR, deployed_at: '2026-10-27T11:05:00Z' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Draft reply' }));
    await waitFor(() => expect(put).toHaveBeenCalled());
    const detail = (put.mock.calls[0][0] as CustomEvent<{ conversationId: string; text: string; aiDraftId?: string }>).detail;
    expect(detail.conversationId).toBe(conversationId);
    expect(detail.text).toMatch(/^Hi Tomás,\n\nThe fix for this is now released/);
    expect(detail.aiDraftId).toBeUndefined();
    window.removeEventListener('colliedesk:insert-reply', put);
  });

  it('asks the AI draft for a reply from the outcome when AI drafts are on', async () => {
    const put = vi.fn();
    window.addEventListener('colliedesk:insert-reply', put);
    const draft = { id: '99999999-9999-4999-8999-999999999999', status: 'ready', text: 'Hi Tomás, it is fixed.' };
    const calls = mount(fixOf({ state: 'deployed', ...withPR, deployed_at: '2026-10-27T11:05:00Z' }), true, {
      [`POST /api/v1/conversations/${conversationId}/ai-drafts`]: () => json(201, aiDraft(draft)),
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Draft reply' }));
    await waitFor(() => expect(put).toHaveBeenCalled());
    expect((calls.find((call) => call.path.endsWith('/ai-drafts'))?.body as { prompt: string }).prompt).toMatch(/released at/);
    expect((put.mock.calls[0][0] as CustomEvent<{ aiDraftId?: string }>).detail.aiDraftId).toBe(draft.id);
    expect(await screen.findByText('The reply draft is in the reply box.')).toBeInTheDocument();
    expect(screen.queryByText(/is being written/)).not.toBeInTheDocument();
    window.removeEventListener('colliedesk:insert-reply', put);
  });

  it('says why GitHub sent the person back without sending, and cleans the address', async () => {
    window.history.replaceState(null, '', `/conversations/${conversationId}?fix_error=denied`);
    mount(fixOf({ state: 'cancelled', outcome_reason: 'authorization_denied', issue_number: null, issue_url: null, vendor_url: null }));
    expect(await screen.findByText('You declined on GitHub, so nothing was sent.')).toBeInTheDocument();
    expect(window.location.search).toBe('');
  });

  it('says why a fix was not sent in the words of sending, not of connecting GitHub', async () => {
    window.history.replaceState(null, '', `/conversations/${conversationId}?fix_error=forbidden`);
    mount(fixOf({ state: 'sending', issue_number: null, issue_url: null, vendor_url: null }));
    expect(await screen.findByText(/You can’t send fixes in this workspace now/)).toBeInTheDocument();
    expect(screen.queryByText(/connect GitHub/)).not.toBeInTheDocument();
  });

  it('says a fix no longer waiting for GitHub was not sent by this confirmation', async () => {
    window.history.replaceState(null, '', `/conversations/${conversationId}?fix_error=expired`);
    mount(fixOf({ state: 'cancelled', outcome_reason: 'authorization_abandoned', issue_number: null, issue_url: null, vendor_url: null }));
    expect(await screen.findByText(/This fix was no longer waiting for GitHub/)).toBeInTheDocument();
  });
});

it('sends a fix stuck waiting for GitHub again: it ends it, then opens the send dialog as the next attempt', async () => {
  const stuck = fixOf({ state: 'sending', issue_number: null, issue_url: null, vendor_url: null, state_changed_at: new Date(Date.now() - 5 * 60_000).toISOString() });
  const calls = mount(stuck, false, {
    [`POST /api/v1/autofix/fixes/${stuck.id}/cancel`]: () => json(200, { ...stuck, state: 'cancelled', outcome_reason: 'cancelled_by_person' }),
    [`POST /api/v1/conversations/${conversationId}/fixes/preview`]: () => json(200, previewOf()),
  });
  fireEvent.click(await screen.findByRole('button', { name: 'Retry (attempt 2)' }));
  expect(await screen.findByRole('heading', { name: 'Retry · attempt 2' })).toBeInTheDocument();
  expect(calls.find((call) => call.path.endsWith('/cancel'))?.body).toEqual({ row_version: 3 });
});

it('drops the “being written” line when the AI draft fails', async () => {
  const calls = mount(fixOf({ state: 'deployed', ...withPR, deployed_at: '2026-10-27T11:05:00Z' }), true, {
    [`POST /api/v1/conversations/${conversationId}/ai-drafts`]: () => json(500, { error: { code: 'internal', message: 'x' } }),
  });
  fireEvent.click(await screen.findByRole('button', { name: 'Draft reply' }));
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  expect(screen.queryByText(/is being written/)).not.toBeInTheDocument();
  expect(calls.some((call) => call.path.endsWith('/ai-drafts'))).toBe(true);
});

describe('targetOf', () => {
  it('starts with a repository that takes fixes and its default agent when that one is on', () => {
    const settings = settingsOf({
      repos: [repoOf({ id: 'a0000000-0000-4000-8000-000000000000', paused_reason: 'became_public' }), repoOf({ default_connector: 'copilot_issue' })],
      connectors: [connectorOf(), connectorOf({ kind: 'copilot_issue' })],
    });
    expect(targetOf(settings)).toMatchObject({ repo: { id: repoOf().id }, connector: 'copilot_issue' });
    expect(targetOf(settingsOf({ connectors: [connectorOf({ kind: 'cursor_api', credential: null })] })).connector).toBeUndefined();
  });
  it('keeps the strip on a phone when the fix did not load, so the way to Try again stays one tap away', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    serve({
      [`GET /api/v1/conversations/${conversationId}/fixes`]: () => json(500, { error: { code: 'internal', message: 'boom' } }),
      'GET /api/v1/autofix/settings': () => json(200, settingsOf()),
    });
    const open = vi.fn();
    render(<QueryClientProvider client={client}><AutofixStrip conversationId={conversationId} timezone="UTC" onOpenDetails={open} /></QueryClientProvider>);
    fireEvent.click(await screen.findByRole('button', { name: /Autofix.*Did not load/ }));
    expect(open).toHaveBeenCalled();
  });
});

function aiDraft(fields: Record<string, unknown>) {
  return {
    id: fields.id, conversation_id: conversationId, status: 'ready', error_code: null, prompt: 'x', text: '', missing_info: null, sources: [], warnings: [], model: null,
    outcome: null, similarity: null, sent_message_id: null, requested_by_agent_id: null, automatic: false, answers_message_id: null, reply_needed: null,
    created_at: '2026-10-27T11:00:00Z', ready_at: '2026-10-27T11:00:00Z', decided_at: null, ...fields,
  };
}
