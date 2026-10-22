import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { AITriageCard } from './ai-triage-card';
import type { AITriage } from './ai-triage-api';
import type { Tag, TeamMember } from '../../api/schemas';

const conversation = '22222222-2222-4222-8222-222222222222';
const hardware = '66666666-6666-4666-8666-666666666666';
const office = '77777777-7777-4777-8777-777777777777';
const sam = '88888888-8888-4888-8888-888888888888';
const tags = [{ id: hardware, name: 'hardware' }, { id: office, name: 'office' }] as Tag[];
const agents = [{ id: sam, display_name: 'Sam Rivera', email: 'sam@example.com', deactivated_at: null }] as unknown as TeamMember[];

function suggestion(change: Partial<AITriage> = {}): AITriage {
  return { id: '99999999-9999-4999-8999-999999999999', conversation_id: conversation, status: 'ready', error_code: null, automatic: true,
    tag_ids: [hardware, office], priority: 'urgent', assignee_agent_id: sam, reason: 'The printer is down for the whole office.', model: 'claude-haiku-5-5',
    outcome: null, applied: { tag_ids: [], priority: null, assignee_agent_id: null }, created_at: '2026-10-22T10:00:00Z', ready_at: '2026-10-22T10:00:03Z',
    decided_at: null, decided_by_agent_id: null, ...change };
}

function serve(route: (url: string, init?: RequestInit) => unknown) {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => new Response(JSON.stringify(route(url, init)), { status: 200, headers: { 'Content-Type': 'application/json' } }));
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

afterEach(() => vi.unstubAllGlobals());

const show = (onApplied = vi.fn()) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
  <AITriageCard conversationId={conversation} tags={tags} agents={agents} onApplied={onApplied} /></QueryClientProvider>);

test('a ready suggestion lists each part with a box, and applying all of it sends no body', async () => {
  const fetch = serve((url) => (url.endsWith('/accept') ? suggestion({ outcome: 'accepted' }) : { suggestion: suggestion() }));
  const onApplied = vi.fn();
  show(onApplied);
  expect(await screen.findByRole('checkbox', { name: 'hardware' })).toBeChecked();
  expect(screen.getByRole('checkbox', { name: 'Priority: Urgent' })).toBeChecked();
  expect(screen.getByRole('checkbox', { name: 'Assign to Sam Rivera' })).toBeChecked();
  expect(screen.getByText('The printer is down for the whole office.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
  expect(await screen.findByText('Applied. Change anything above as usual.')).toBeInTheDocument();
  const accept = fetch.mock.calls.find(([url]) => String(url).endsWith('/accept'));
  expect(accept?.[1]?.body).toBeUndefined();
  expect(onApplied).toHaveBeenCalled();
});

test('unchecking parts applies only the ones left', async () => {
  const fetch = serve((url) => (url.endsWith('/accept') ? suggestion({ outcome: 'partly_accepted' }) : { suggestion: suggestion() }));
  show();
  fireEvent.click(await screen.findByRole('checkbox', { name: 'office' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Assign to Sam Rivera' }));
  fireEvent.click(screen.getByRole('button', { name: 'Apply selected' }));
  await screen.findByText('Applied. Change anything above as usual.');
  const accept = fetch.mock.calls.find(([url]) => String(url).endsWith('/accept'));
  expect(JSON.parse(String(accept?.[1]?.body))).toEqual({ tag_ids: [hardware], priority: 'urgent', assignee_agent_id: null });
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
