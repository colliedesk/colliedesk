import { render, screen, within } from '@testing-library/react';
import { beforeAll, expect, test } from 'vitest';
import type { Message } from '../../api/schemas';
import { TimelinePanel } from './timeline-panel';
import { presenceViewPart } from './presence-part';

const base = { reply_number: null, conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, body_html: null, origin: null,
  attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', delivery: null, email: null, agent_token: null, ai_draft_id: null, ai_draft_outcome: null, ai_draft_source: null, drafted_by: null, agent_request_id: null };
const ada = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';

function avatarOf(article: HTMLElement) {
  return article.querySelector('.avatar')?.textContent;
}


// The presence chunk loads when somebody else is on a conversation, a reply is refused or a notice shows;
// start with it there, so no test ends while it is still on its way (vitest refuses a module asked for
// after the test environment is gone).
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
});

test('a reply and a note written in Colliedesk name the team member who wrote them', () => {
  const reply: Message = { ...base, id: '22222222-2222-4222-8222-222222222222', kind: 'outgoing', body: 'On its way.', author_agent_id: ada, author_name: 'Ada Admin' };
  const note: Message = { ...base, id: '33333333-3333-4333-8333-333333333333', kind: 'note', body: 'Refund approved.', author_agent_id: ada, author_name: 'Ada Admin' };
  render(<TimelinePanel items={[reply, note]} timezone="UTC" />);
  for (const article of screen.getAllByRole('article')) {
    expect(within(article).getByText('Ada Admin')).toBeInTheDocument();
    expect(avatarOf(article)).toBe('AA');
  }
});

test('a message an agent token wrote names the person once, in the token line', () => {
  const written: Message = { ...base, id: '44444444-4444-4444-8444-444444444444', kind: 'note', body: 'Checked the logs.', author_agent_id: ada, author_name: 'Ada Admin',
    agent_token: { id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', label: 'Support bot', agent_name: 'Ada Admin' } };
  render(<TimelinePanel items={[written]} timezone="UTC" />);
  const article = screen.getByRole('article');
  expect(within(article).getByText('by Ada Admin via Support bot')).toBeInTheDocument();
  expect(within(article).queryByText('Ada Admin')).not.toBeInTheDocument();
  expect(avatarOf(article)).toBe('AA');
});

test('a reply sent from an agent draft names the person who sent it once', () => {
  const sent: Message = { ...base, id: '55555555-5555-4555-8555-555555555555', kind: 'outgoing', body: 'Edited answer.', author_agent_id: ada, author_name: 'Ada Admin',
    ai_draft_id: null, ai_draft_outcome: null, agent_request_id: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd' };
  render(<TimelinePanel items={[sent]} timezone="UTC" />);
  const article = screen.getByRole('article');
  expect(within(article).getByText('from an agent draft, edited and sent by Ada Admin')).toBeInTheDocument();
  expect(within(article).queryByText('Ada Admin')).not.toBeInTheDocument();
});

test('an incoming email shows the contact name and keeps the address visible', () => {
  const incoming: Message = { ...base, id: '66666666-6666-4666-8666-666666666666', kind: 'incoming', body: 'Hello.', author_name: 'Casey Customer',
    email: { from: 'casey@customer.example', authentication: 'pass', attachments: [], original_attachment_id: null } };
  render(<TimelinePanel items={[incoming]} timezone="UTC" />);
  const article = screen.getByRole('article');
  expect(within(article).getByText('Casey Customer')).toBeInTheDocument();
  expect(within(article).getByText('casey@customer.example')).toBeInTheDocument();
  expect(avatarOf(article)).toBe('CC');
});

test('an incoming email from someone without a name shows the address', () => {
  const incoming: Message = { ...base, id: '77777777-7777-4777-8777-777777777777', kind: 'incoming', body: 'Hi.',
    email: { from: 'robin@customer.example', authentication: 'pass', attachments: [], original_attachment_id: null } };
  render(<TimelinePanel items={[incoming]} timezone="UTC" />);
  const article = screen.getByRole('article');
  expect(within(article).getByText('robin@customer.example')).toBeInTheDocument();
  expect(avatarOf(article)).toBe('RE');
});

// A reply sent from a draft an agent token wrote names that token and its person, and does not claim the
// built-in AI wrote it; a reply from an AI draft keeps its AI mark.
test('a reply from a written draft names the token that drafted it, not AI', () => {
  const written: Message = { ...base, id: '44444444-4444-4444-8444-444444444444', kind: 'outgoing', body: 'Fixed.', author_agent_id: ada, author_name: 'Tom Okoye',
    ai_draft_id: '55555555-5555-4555-8555-555555555555', ai_draft_outcome: 'sent_unchanged', ai_draft_source: 'written',
    drafted_by: { id: '66666666-6666-4666-8666-666666666666', label: 'Claude Code', agent_name: 'Tom Okoye' } };
  const fromAI: Message = { ...written, id: '77777777-7777-4777-8777-777777777777', ai_draft_source: 'ai', drafted_by: null };
  render(<TimelinePanel items={[written, fromAI]} timezone="UTC" />);
  const [first, second] = screen.getAllByRole('article');
  expect(first).toHaveTextContent('Drafted by Claude Code, agent token of Tom Okoye, sent as written');
  expect(first).not.toHaveTextContent('AI draft');
  expect(first.querySelector('.ai-chip')).toBeNull();
  expect(second).toHaveTextContent('from an AI draft, sent as written');
  expect(second.querySelector('.ai-chip')).not.toBeNull();
});
