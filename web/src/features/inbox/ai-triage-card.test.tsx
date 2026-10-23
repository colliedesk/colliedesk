import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { AITriageCard } from './ai-triage-card';
import type { AITriage } from './ai-triage-api';
import type { Tag, TeamMember } from '../../api/schemas';

const conversation = '22222222-2222-4222-8222-222222222222';
const hardware = '66666666-6666-4666-8666-666666666666';
const office = '77777777-7777-4777-8777-777777777777';
const sam = '88888888-8888-4888-8888-888888888888';
const me = '55555555-5555-4555-8555-555555555555';
const article = '44444444-4444-4444-8444-444444444444';
const tags = [{ id: hardware, name: 'hardware' }, { id: office, name: 'office' }] as Tag[];
const agents = [{ id: sam, display_name: 'Sam Rivera', email: 'sam@example.com', deactivated_at: null }] as unknown as TeamMember[];
const nothingApplied = { tag_ids: [], priority: null, assignee_agent_id: null };
const allApplied = { tag_ids: [hardware, office], priority: 'urgent', assignee_agent_id: sam };
// A suggestion with no part: written, failed, or nothing to change.
const empty = { tag_ids: [], priority: null, assignee_agent_id: null, reason: null };

function suggestion(change: Partial<AITriage> = {}): AITriage {
  return { id: '99999999-9999-4999-8999-999999999999', conversation_id: conversation, status: 'ready', error_code: null, automatic: true,
    tag_ids: [hardware, office], priority: 'urgent', assignee_agent_id: sam, reason: 'The printer is down for the whole office.',
    sources: [{ kind: 'kb_article', id: article, title: 'Printer paper jams' }], stale: { priority: null, assignee_agent_id: null }, model: 'claude-haiku-5-5',
    outcome: null, applied: nothingApplied, created_at: '2026-10-22T10:00:00Z', ready_at: '2026-10-22T10:00:03Z',
    decided_at: null, decided_by_agent_id: null, ...change };
}

function serve(route: (url: string, init?: RequestInit) => unknown) {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => new Response(JSON.stringify(route(url, init)), { status: 200, headers: { 'Content-Type': 'application/json' } }));
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

afterEach(() => vi.unstubAllGlobals());

const show = (onApplied = vi.fn(), canManage = false, onWaiting?: (waiting: boolean) => void) => render(
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <AITriageCard conversationId={conversation} tags={tags} agents={agents} onApplied={onApplied} currentAgentId={me} timezone="UTC" canManage={canManage} onWaiting={onWaiting} />
  </QueryClientProvider>);

test('a ready suggestion has a row with Accept for each part, its reason and its sources', async () => {
  serve(() => ({ suggestion: suggestion() }));
  show();
  const tagsRow = await screen.findByRole('group', { name: 'Tags' });
  expect(within(tagsRow).getByText('hardware')).toBeInTheDocument();
  expect(within(tagsRow).getByText('office')).toBeInTheDocument();
  expect(within(screen.getByRole('group', { name: 'Priority' })).getByText('Urgent')).toBeInTheDocument();
  expect(within(screen.getByRole('group', { name: 'Assignee' })).getByText('Sam Rivera')).toBeInTheDocument();
  expect(screen.getAllByRole('button', { name: /^Accept (tags|priority|assignee) / })).toHaveLength(3);
  expect(screen.getByText('The printer is down for the whole office.')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: /Printer paper jams/ })).toHaveAttribute('href', `/knowledge/articles/${article}`);
  expect(screen.getByRole('button', { name: 'Accept all' })).toBeInTheDocument();
});

test('accepting one part sends only it and leaves the rest open', async () => {
  const fetch = serve((url) => (url.endsWith('/accept') ? suggestion({ applied: { ...nothingApplied, priority: 'urgent' } }) : { suggestion: suggestion() }));
  const onApplied = vi.fn();
  show(onApplied);
  fireEvent.click(await screen.findByRole('button', { name: 'Accept priority Urgent' }));
  expect(await within(screen.getByRole('group', { name: 'Priority' })).findByText('Set')).toBeInTheDocument();
  const accept = fetch.mock.calls.find(([url]) => String(url).endsWith('/accept'));
  expect(JSON.parse(String(accept?.[1]?.body))).toEqual({ priority: 'urgent' });
  expect(onApplied).toHaveBeenCalled();
  expect(screen.getByRole('button', { name: 'Accept the rest' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Accept tags hardware, office' })).toBeInTheDocument();
});

test('it tells the details toggle while a part waits, and no longer once all of it is accepted', async () => {
  serve((url) => (url.endsWith('/accept')
    ? suggestion({ outcome: 'accepted', applied: allApplied, decided_at: '2026-10-22T10:42:00Z', decided_by_agent_id: me })
    : { suggestion: suggestion() }));
  const onWaiting = vi.fn();
  show(vi.fn(), false, onWaiting);
  fireEvent.click(await screen.findByRole('button', { name: 'Accept all' }));
  await screen.findByText(/Accepted by you/);
  expect(onWaiting.mock.calls.map(([waiting]) => waiting)).toContain(true);
  expect(onWaiting).toHaveBeenLastCalledWith(false);
});

test('accept all sends no body and then says who accepted it and how to undo', async () => {
  const fetch = serve((url) => (url.endsWith('/accept')
    ? suggestion({ outcome: 'accepted', applied: allApplied, decided_at: '2026-10-22T10:42:00Z', decided_by_agent_id: me })
    : { suggestion: suggestion() }));
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Accept all' }));
  expect(await screen.findByText(/Accepted by you, .*10:42.*Change a field below to undo\./)).toBeInTheDocument();
  expect(within(screen.getByRole('group', { name: 'Tags' })).getByText('Added')).toBeInTheDocument();
  expect(within(screen.getByRole('group', { name: 'Assignee' })).getByText('Assigned')).toBeInTheDocument();
  const accept = fetch.mock.calls.find(([url]) => String(url).endsWith('/accept'));
  expect(accept?.[1]?.body).toBeUndefined();
  expect(screen.queryByRole('button', { name: /^Accept/ })).not.toBeInTheDocument();
});

test('a field a person set first is not suggested, and says so', async () => {
  serve(() => ({ suggestion: suggestion({ priority: null, stale: { priority: 'urgent', assignee_agent_id: null } }) }));
  show();
  expect(await screen.findByText('Not suggested: you set Urgent before this arrived.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Accept the rest' })).toBeInTheDocument();
});

test('a suggestion with nothing to change says so and offers another', async () => {
  serve(() => ({ suggestion: suggestion(empty) }));
  show();
  expect(await screen.findByText('Nothing to change: the tags, priority and assignee already fit.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Suggest again' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Accept all' })).not.toBeInTheDocument();
});

test('while it is written a person keeps working, or cancels it', async () => {
  let current = suggestion({ status: 'queued', ...empty, ready_at: null });
  const fetch = serve((url) => (url.endsWith('/dismiss') ? (current = { ...current, status: 'cancelled', error_code: 'cancelled' }) : { suggestion: current }));
  show();
  expect(await screen.findByText('Suggesting tags, priority and assignee…')).toBeInTheDocument();
  expect(screen.getByText('Keep working; it appears here when ready.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(await screen.findByText('Stopped.')).toBeInTheDocument();
  expect(fetch.mock.calls.some(([url]) => String(url).endsWith('/dismiss'))).toBe(true);
});

test('past the daily limit it says automatic suggestions are paused and a person can still ask', async () => {
  serve(() => ({ suggestion: suggestion({ status: 'failed', error_code: 'automatic_limit', ...empty, ready_at: null, created_at: '2026-10-22T22:30:00Z' }) }));
  show();
  // Midnight UTC after it, in the agent's timezone (UTC here).
  expect(await screen.findByText(/^Automatic suggestions are paused until .*12:00\sAM.*: the daily limit is reached\. You can still ask for one\.$/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Suggest now' })).toBeInTheDocument();
});

test('a failure a setting causes sends an admin to AI settings', async () => {
  serve(() => ({ suggestion: suggestion({ status: 'failed', error_code: 'ai_allowance_reached', ...empty, ready_at: null }) }));
  show(vi.fn(), true);
  expect(await screen.findByText(/this month’s included AI is used up/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Open AI settings' })).toHaveAttribute('href', '/settings/ai');
});

test('without a suggestion a person asks for one, and dismissing changes nothing', async () => {
  let current: AITriage | null = null;
  serve((url, init) => {
    if (init?.method === 'POST' && url.endsWith('/ai-triage')) return (current = suggestion());
    if (url.endsWith('/dismiss')) return (current = suggestion({ outcome: 'dismissed', decided_at: '2026-10-22T10:01:00Z' }));
    return { suggestion: current };
  });
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Suggest tags and priority' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Dismiss' }));
  expect(await screen.findByText('Dismissed.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Suggest again' })).toBeInTheDocument();
});
