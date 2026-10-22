import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, expect, test, vi } from 'vitest';
import type { Conversation } from '../../api/schemas';
import { ConversationList } from './conversation-list';
import { presenceViewPart } from './presence-part';

// The marks come from their own chunk; these tests start with it there.
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
  await new Promise((resolve) => setTimeout(resolve, 0));
});

const row = (updated_at: string): Conversation => ({ latest_reply_number: 0,
  id: '11111111-1111-4111-8111-111111111111', contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Mai Linh', subject: 'Example request',
  status: 'open', priority: 'normal', assignee_id: null, tags: [], channel: 'email', origin: null, created_at: updated_at, updated_at,
});

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

test('a row exposes its full timestamp to a screen reader and to keyboard focus, not only to a mouse hover', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  render(<ConversationList items={[row('2026-10-15T09:05:00Z')]} timezone="UTC" page={1} hasNextPage={false} />);
  const button = screen.getByRole('button', { name: /example request/i });
  expect(button).toHaveAccessibleName(/Oct 15, 9:05 AM/);
  expect(button).toHaveAttribute('data-tip', 'Oct 15, 9:05 AM');
  expect(button).not.toHaveAttribute('title');
});

test('the Today group becomes Earlier when midnight passes while the page stays open', () => {
  vi.setSystemTime(new Date('2026-10-15T23:58:30Z'));
  render(<ConversationList items={[row('2026-10-15T22:00:00Z')]} timezone="UTC" page={1} hasNextPage={false} />);
  expect(screen.getByRole('heading', { name: 'Today' })).toBeInTheDocument();
  act(() => { vi.advanceTimersByTime(120_000); });
  expect(screen.getByRole('heading', { name: 'Earlier' })).toBeInTheDocument();
});

test('the group follows the agent timezone: 23:30 UTC is already tomorrow in Ho Chi Minh', () => {
  vi.setSystemTime(new Date('2026-10-15T16:59:30Z'));
  render(<ConversationList items={[row('2026-10-15T10:00:00Z')]} timezone="Asia/Ho_Chi_Minh" page={1} hasNextPage={false} />);
  expect(screen.getByRole('heading', { name: 'Today' })).toBeInTheDocument();
  act(() => { vi.advanceTimersByTime(90_000); });
  expect(screen.getByRole('heading', { name: 'Earlier' })).toBeInTheDocument();
});

test('a screen reader hears which conversation is open, and only that one', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  const other = { ...row('2026-10-15T08:00:00Z'), id: '33333333-3333-4333-8333-333333333333', subject: 'Other request' };
  render(<ConversationList items={[row('2026-10-15T09:05:00Z'), other]} selectedId={other.id} timezone="UTC" page={1} hasNextPage={false} />);
  expect(screen.getByRole('button', { name: /other request/i })).toHaveAttribute('aria-current', 'true');
  expect(screen.getByRole('button', { name: /example request/i })).not.toHaveAttribute('aria-current');
});

test('the first load shows a loading status, never "No conversations" or the get-started guide', () => {
  render(<ConversationList items={[]} timezone="UTC" page={1} hasNextPage={false} loading />);
  expect(screen.getByRole('status')).toHaveTextContent('Loading workspace…');
  expect(screen.queryByText(/no conversations/i)).not.toBeInTheDocument();
  expect(screen.queryByRole('region', { name: /get started/i })).not.toBeInTheDocument();
});

test('a refetch keeps the previous rows, marks the list busy and shows no loading status', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  render(<ConversationList items={[row('2026-10-15T09:05:00Z')]} timezone="UTC" page={1} hasNextPage={false} refreshing />);
  expect(screen.getByRole('button', { name: /example request/i })).toBeInTheDocument();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  expect(document.querySelector('.conversation-scroll')).toHaveAttribute('aria-busy', 'true');
});

test('a settled list is not marked busy', () => {
  render(<ConversationList items={[]} timezone="UTC" page={1} hasNextPage={false} filtered />);
  expect(document.querySelector('.conversation-scroll')).toHaveAttribute('aria-busy', 'false');
});

const steps = () => within(screen.getByRole('region', { name: 'Get started' })).getAllByRole('listitem').map((item) => item.textContent);

test.each([
  ['leave_helpdesk', ['Recommended first step: Import from Freshdesk', 'Connect your support email', 'Invite your team']],
  ['shared_inbox', ['Recommended first step: Connect your support email', 'Import from Freshdesk', 'Invite your team']],
  ['lower_cost', ['Recommended first step: Invite your team', 'Connect your support email', 'Import from Freshdesk']],
  ['ai_agents', ['Recommended first step: Connect an AI agent', 'Connect your support email', 'Import from Freshdesk', 'Invite your team']],
  ['other', ['Connect your support email', 'Import from Freshdesk', 'Invite your team']],
  [null, ['Connect your support email', 'Import from Freshdesk', 'Invite your team']],
] as const)('a brand-new workspace whose goal is %s starts with the matching step', (goal, expected) => {
  render(<ConversationList items={[]} timezone="UTC" page={1} hasNextPage={false} canManage onboardingGoal={goal} />);
  expect(steps()).toEqual(expected);
});

test('the AI agent step links to the MCP guide', () => {
  render(<ConversationList items={[]} timezone="UTC" page={1} hasNextPage={false} onboardingGoal="ai_agents" />);
  expect(screen.getByRole('link', { name: 'Connect an AI agent' })).toHaveAttribute('href', 'https://colliedesk.com/docs/mcp');
});

test('an agent who is not an admin never gets the settings steps, whatever the goal', () => {
  render(<ConversationList items={[]} timezone="UTC" page={1} hasNextPage={false} onboardingGoal="lower_cost" />);
  expect(steps()).toEqual(['Import from Freshdesk']);
});

test('a row marks who else has the conversation open, and says in words who is writing a reply', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  const viewers = new Map([['11111111-1111-4111-8111-111111111111', [
    { agent_id: '44444444-4444-4444-8444-444444444444', name: 'Lan Pham', writing: true, writing_since: '2026-10-15T11:59:00Z', edited_at: '2026-10-15T11:59:50Z', agent_token: null },
    { agent_id: '55555555-5555-4555-8555-555555555555', name: 'Ben', writing: false, writing_since: null, edited_at: null,
      agent_token: { id: '66666666-6666-4666-8666-666666666666', label: 'Claude Code', scope: 'read' as const, agent_name: 'Ben' } },
  ]]]);
  const other = { ...row('2026-10-15T08:00:00Z'), id: '33333333-3333-4333-8333-333333333333', subject: 'Other request' };
  render(<ConversationList items={[row('2026-10-15T09:05:00Z'), other]} timezone="UTC" page={1} hasNextPage={false} viewers={viewers} />);
  const marked = screen.getByRole('button', { name: /example request/i });
  expect(marked).toHaveAccessibleName(/Open now: Lan Pham \(writing a reply\), Ben via Claude Code/);
  expect(within(marked).getByText('Replying')).toBeInTheDocument();
  expect(marked.querySelector('.viewer-stack')).toHaveAttribute('data-tip', 'Lan Pham is writing a reply\nBen via Claude Code is viewing');
  expect(marked.querySelectorAll('.viewer-stack .avatar')).toHaveLength(2);
  expect(marked.querySelectorAll('.viewer-stack .avatar-agent')).toHaveLength(1);
  const unmarked = screen.getByRole('button', { name: /other request/i });
  expect(unmarked).not.toHaveAccessibleName(/Open now/);
  expect(unmarked.querySelector('.viewer-stack')).toBeNull();
});

test('many viewers show three avatars and a count', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  const people = ['Ann', 'Bob', 'Cy', 'Dee', 'Eve'].map((name, index) => ({
    agent_id: `4444444${index}-4444-4444-8444-444444444444`, name, writing: false, writing_since: null, edited_at: null, agent_token: null,
  }));
  render(<ConversationList items={[row('2026-10-15T09:05:00Z')]} timezone="UTC" page={1} hasNextPage={false} viewers={new Map([['11111111-1111-4111-8111-111111111111', people]])} />);
  const marked = screen.getByRole('button', { name: /example request/i });
  expect(marked.querySelectorAll('.viewer-stack .avatar')).toHaveLength(3);
  expect(within(marked).getByText('+2')).toBeInTheDocument();
  expect(within(marked).queryByText('Replying')).toBeNull();
});

test('an AI agent with several tokens is one tile of its own shape, apart from the same person in a tab', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  const ben = { agent_id: '55555555-5555-4555-8555-555555555555', name: 'Ben', writing: false, writing_since: null, edited_at: null, agent_token: null };
  const token = (id: string, label: string) => ({ ...ben, agent_token: { id, label, scope: 'read' as const, agent_name: 'Ben' } });
  const viewers = [ben, token('66666666-6666-4666-8666-666666666661', 'Claude Code'), token('66666666-6666-4666-8666-666666666662', 'Codex')];
  render(<ConversationList items={[row('2026-10-15T09:05:00Z')]} timezone="UTC" page={1} hasNextPage={false} viewers={new Map([['11111111-1111-4111-8111-111111111111', viewers]])} />);
  const marked = screen.getByRole('button', { name: /example request/i });
  expect(marked.querySelectorAll('.viewer-stack .avatar')).toHaveLength(2);
  expect(marked.querySelectorAll('.viewer-stack .avatar-agent')).toHaveLength(1);
  expect(marked.querySelector('.viewer-stack')).toHaveAttribute('data-tip', 'Ben is viewing\nBen via Claude Code and Codex is viewing');
});

test('a reply left untouched for minutes marks the row Draft open, not Replying, like the conversation header', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  const lan = { agent_id: '44444444-4444-4444-8444-444444444444', name: 'Lan', writing: true, writing_since: '2026-10-15T11:00:00Z', edited_at: '2026-10-15T11:50:00Z', agent_token: null };
  render(<ConversationList items={[row('2026-10-15T09:05:00Z')]} timezone="UTC" page={1} hasNextPage={false} viewers={new Map([['11111111-1111-4111-8111-111111111111', [lan]]])} />);
  const marked = screen.getByRole('button', { name: /example request/i });
  expect(within(marked).getByText('Draft open')).toBeInTheDocument();
  expect(within(marked).queryByText('Replying')).toBeNull();
  expect(marked).toHaveAccessibleName(/Open now: Lan \(reply not sent yet\)/);
});
