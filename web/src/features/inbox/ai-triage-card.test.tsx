import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
    decided_at: null, decided_by_agent_id: null, auto_applied_at: null, auto_applied: nothingApplied, skipped: {}, undone_parts: nothingApplied,
    kept: { priority: null, assignee_agent_id: null }, undone_at: null, undone_by_agent_id: null, ...change };
}

function serve(route: (url: string, init?: RequestInit) => unknown) {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => new Response(JSON.stringify(route(url, init)), { status: 200, headers: { 'Content-Type': 'application/json' } }));
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

afterEach(() => vi.unstubAllGlobals());

const show = (onApplied = vi.fn(), canManage = false, onWaiting?: (waiting: boolean) => void, placement: 'details' | 'timeline' = 'details') => render(
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <AITriageCard conversationId={conversation} tags={tags} agents={agents} onApplied={onApplied} currentAgentId={me} timezone="UTC" canManage={canManage} onWaiting={onWaiting}
      placement={placement} />
  </QueryClientProvider>);

test('a ready suggestion has a row with Accept for each part, its reason and its sources', async () => {
  serve(() => ({ suggestion: suggestion(), auto_applied: null }));
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
  const fetch = serve((url) => (url.endsWith('/accept') ? suggestion({ applied: { ...nothingApplied, priority: 'urgent' } }) : { suggestion: suggestion(), auto_applied: null }));
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
    : { suggestion: suggestion(), auto_applied: null }));
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
    : { suggestion: suggestion(), auto_applied: null }));
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
  serve(() => ({ suggestion: suggestion({ priority: null, stale: { priority: 'urgent', assignee_agent_id: null } }), auto_applied: null }));
  show();
  expect(await screen.findByText('Not suggested: a person set Urgent before this arrived.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Accept the rest' })).toBeInTheDocument();
});

test('a suggestion with nothing to change says so and offers another', async () => {
  serve(() => ({ suggestion: suggestion(empty), auto_applied: null }));
  show();
  expect(await screen.findByText('Nothing to change: the tags, priority and assignee already fit.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Suggest again' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Accept all' })).not.toBeInTheDocument();
});

test('while it is written a person keeps working, or cancels it', async () => {
  let current = suggestion({ status: 'queued', ...empty, ready_at: null });
  const fetch = serve((url) => (url.endsWith('/dismiss') ? (current = { ...current, status: 'cancelled', error_code: 'cancelled' }) : { suggestion: current, auto_applied: null }));
  show();
  expect(await screen.findByText('Suggesting tags, priority and assignee…')).toBeInTheDocument();
  expect(screen.getByText('Keep working; it appears here when ready.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(await screen.findByText('Stopped.')).toBeInTheDocument();
  expect(fetch.mock.calls.some(([url]) => String(url).endsWith('/dismiss'))).toBe(true);
});

test('past the daily limit it says automatic suggestions are paused and a person can still ask', async () => {
  serve(() => ({ suggestion: suggestion({ status: 'failed', error_code: 'automatic_limit', ...empty, ready_at: null, created_at: '2026-10-22T22:30:00Z' }), auto_applied: null }));
  show();
  // Midnight UTC after it, in the agent's timezone (UTC here).
  expect(await screen.findByText(/^Automatic suggestions are paused until .*12:00\sAM.*: the daily limit is reached\. You can still ask for one\.$/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Suggest now' })).toBeInTheDocument();
});

test('a failure a setting causes sends an admin to AI settings', async () => {
  serve(() => ({ suggestion: suggestion({ status: 'failed', error_code: 'ai_allowance_reached', ...empty, ready_at: null }), auto_applied: null }));
  show(vi.fn(), true);
  expect(await screen.findByText(/this month’s included AI is used up/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Open AI settings' })).toHaveAttribute('href', '/settings/ai');
});

test('a failure a setting causes offers only the way to the setting, not another try', async () => {
  serve(() => ({ suggestion: suggestion({ status: 'failed', error_code: 'auth', ...empty, ready_at: null }), auto_applied: null }));
  show(vi.fn(), false);
  expect(await screen.findByText(/refused its key/)).toBeInTheDocument();
  expect(screen.getByText('Ask an admin to check Settings → Built-in AI.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
});

test('an answer too long says so', async () => {
  serve(() => ({ suggestion: suggestion({ status: 'failed', error_code: 'too_long', ...empty, ready_at: null }), auto_applied: null }));
  show();
  expect(await screen.findByText(/answer ran too long/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
});

test('a dropped connection says nothing changed and what to do; an accepted part moves focus and is announced', async () => {
  let fail = true;
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/accept')) {
      if (fail) throw new TypeError('Failed to fetch');
      return new Response(JSON.stringify(suggestion({ applied: { ...nothingApplied, priority: 'urgent' } })), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    return new Response(JSON.stringify({ suggestion: suggestion(), auto_applied: null }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  }));
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Accept priority Urgent' }));
  expect(await screen.findByText(/Nothing was changed: the request did not reach Colliedesk\. Check your connection, then try again\./)).toBeInTheDocument();
  expect(screen.queryByText('Failed to fetch')).not.toBeInTheDocument();
  fail = false;
  fireEvent.click(screen.getByRole('button', { name: 'Accept priority Urgent' }));
  expect(await screen.findByText('Applied. The rest of the suggestion is still open.')).toBeInTheDocument();
  expect(document.activeElement).toBe(screen.getByRole('heading', { name: /Suggested/ }));
});

test('without a suggestion a person asks for one, and dismissing changes nothing', async () => {
  let current: AITriage | null = null;
  serve((url, init) => {
    if (init?.method === 'POST' && url.endsWith('/ai-triage')) return (current = suggestion());
    if (url.endsWith('/dismiss')) return (current = suggestion({ outcome: 'dismissed', decided_at: '2026-10-22T10:01:00Z' }));
    return { suggestion: current, auto_applied: null };
  });
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Suggest tags and priority' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Dismiss' }));
  expect(await screen.findByText('Dismissed.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Suggest again' })).toBeInTheDocument();
});

const appliedByAI = (change: Partial<AITriage> = {}) => suggestion({ outcome: 'auto_applied', auto_applied_at: '2026-10-22T10:31:00Z', decided_at: '2026-10-22T10:31:00Z',
  tag_ids: [hardware], assignee_agent_id: null, applied: { tag_ids: [hardware], priority: 'urgent', assignee_agent_id: null },
  auto_applied: { tag_ids: [hardware], priority: 'urgent', assignee_agent_id: null }, skipped: { assignee: { reason: 'already_assigned', value: sam } }, ...change });

test('what AI applied by itself shows with an undo for each part and for all of it, and what it left says why', async () => {
  const fetch = serve((url) => (url.endsWith('/undo') ? appliedByAI({ outcome: 'auto_applied', auto_applied: { tag_ids: [hardware], priority: null, assignee_agent_id: null } })
    : { suggestion: appliedByAI(), auto_applied: appliedByAI() }));
  const onApplied = vi.fn();
  show(onApplied);
  expect(await screen.findByRole('heading', { name: /Applied by AI/ })).toBeInTheDocument();
  expect(within(screen.getByRole('group', { name: 'Assignee' })).getByText('Not set: Sam Rivera was already assigned.')).toBeInTheDocument();
  expect(screen.getByText('Only filled what nobody set.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Undo priority Urgent' }));
  await waitFor(() => expect(onApplied).toHaveBeenCalled());
  const undo = fetch.mock.calls.find(([url]) => String(url).endsWith('/undo'));
  expect(JSON.parse(String(undo?.[1]?.body))).toEqual({ parts: ['priority'] });
  expect(screen.getByRole('button', { name: 'Undo all' })).toBeInTheDocument();
});

// The page may read the conversation a moment before AI applies, and the card a moment after: the card
// reads it again, once, even when it never saw the suggestion being written.
test('a suggestion AI already applied when the card first reads it has the conversation read again, once', async () => {
  const first = appliedByAI({ id: '99999999-9999-4999-8999-999999999999', auto_applied_at: '2026-10-22T11:00:00Z' });
  serve(() => ({ suggestion: first, auto_applied: first }));
  const onApplied = vi.fn();
  const { unmount } = show(onApplied);
  expect(await screen.findByRole('heading', { name: /Applied by AI/ })).toBeInTheDocument();
  await waitFor(() => expect(onApplied).toHaveBeenCalledTimes(1));
  unmount();
  show(onApplied);
  expect(await screen.findByRole('heading', { name: /Applied by AI/ })).toBeInTheDocument();
  expect(onApplied).toHaveBeenCalledTimes(1);
});

test('after an undo the card says what went back and what was kept, and applies it again on request', async () => {
  const undone = appliedByAI({ outcome: 'undone', auto_applied: nothingApplied, applied: nothingApplied, undone_parts: { tag_ids: [hardware], priority: null, assignee_agent_id: null },
    kept: { priority: 'urgent', assignee_agent_id: null }, undone_at: '2026-10-22T10:45:00Z', undone_by_agent_id: me });
  const fetch = serve((url) => (url.endsWith('/accept') ? appliedByAI({ outcome: 'accepted' }) : { suggestion: undone, auto_applied: undone }));
  show();
  expect(await screen.findByRole('heading', { name: /Undone/ })).toBeInTheDocument();
  expect(screen.getByText('hardware removed')).toBeInTheDocument();
  expect(screen.getByText('Kept Urgent: a person changed it after AI.')).toBeInTheDocument();
  expect(screen.getByText('Fills only what is still empty.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Apply again' }));
  await waitFor(() => expect(fetch.mock.calls.some(([url]) => String(url).endsWith('/accept'))).toBe(true));
});

test('on the timeline, what AI applied shows as a line with Undo, even after a newer suggestion', async () => {
  serve(() => ({ suggestion: suggestion({ id: '12121212-1212-4212-8212-121212121212', automatic: false }), auto_applied: appliedByAI() }));
  show(vi.fn(), false, undefined, 'timeline');
  const line = await screen.findByRole('group', { name: 'Applied by AI' });
  expect(within(line).getByText(/hardware/)).toBeInTheDocument();
  expect(within(line).getAllByRole('button', { name: /^Undo/ }).length).toBeGreaterThan(0);
});

test('applied again after an undo, it is AI’s again with Undo, and the timeline says who applied it again', async () => {
  const again = appliedByAI({ outcome: 'auto_applied', undone_at: '2026-10-22T10:45:00Z', undone_by_agent_id: me, decided_by_agent_id: me,
    applied: { tag_ids: [hardware], priority: null, assignee_agent_id: null }, auto_applied: { tag_ids: [hardware], priority: null, assignee_agent_id: null }, priority: null,
    skipped: { priority: { reason: 'set_by_person', value: 'high' } } });
  serve(() => ({ suggestion: again, auto_applied: again }));
  show(vi.fn(), false, undefined, 'timeline');
  const line = await screen.findByRole('group', { name: 'Applied by AI' });
  expect(within(line).getByText(/applied again by you/)).toBeInTheDocument();
  expect(within(line).getByRole('button', { name: /^Undo/ })).toBeInTheDocument();
  expect(within(screen.getByRole('group', { name: 'Priority' })).getByText('Not set: a person had set it to High.')).toBeInTheDocument();
});
