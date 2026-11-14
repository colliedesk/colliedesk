import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Mention, Message, TeamMember } from '../../api/schemas';
import { MessageItem } from './message-item';
import { ReplyComposer } from './reply-composer';
import { MentionsList } from './mentions-list';
import { askableTeammates } from './mention-picker';

const member = (id: string, display_name: string, email: string, extra: Partial<TeamMember> = {}): TeamMember => ({
  id, email, display_name, roles: [{ id: '00000000-0000-4000-8000-000000000001', name: 'Agent', system: 'agent' }], manageable: false,
  timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0, ...extra,
});
const me = member('10000000-0000-4000-8000-000000000001', 'Hoàng Tú', 'tu@northwind.example');
const long = member('10000000-0000-4000-8000-000000000002', 'Long Bui', 'long@northwind.example');
const loan = member('10000000-0000-4000-8000-000000000003', 'Loan Pham', 'loan@northwind.example');
const gone = member('10000000-0000-4000-8000-000000000004', 'Lo Gone', 'gone@northwind.example', { deactivated_at: '2026-11-01T00:00:00Z' });

test('the people a note can ask are the active teammates, never the writer or someone deactivated', () => {
  expect(askableTeammates([me, long, loan, gone], me.id).map((person) => person.name)).toEqual(['Long Bui', 'Loan Pham']);
});

test('@ in a private note lists teammates at the @: arrows, Enter and Esc work, and the note asks the person picked', async () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} teammates={askableTeammates([me, long, loan, gone], me.id)} />);
  fireEvent.click(screen.getByRole('button', { name: /Private note/ }));
  expect(screen.getByText('Not visible to the customer · Type @ to ask a teammate')).toBeInTheDocument();
  const box = screen.getByRole('textbox', { name: 'Message body' });
  fireEvent.change(box, { target: { value: 'Bank confirmed RF-2291-08. @Lo', selectionStart: 30, selectionEnd: 30 } });
  const list = screen.getByRole('listbox', { name: 'Told in Mentions and by email' });
  expect(within(list).getAllByRole('option').map((option) => option.textContent)).toEqual([expect.stringContaining('Long Bui'), expect.stringContaining('Loan Pham')]);
  expect(within(list).queryByText(/Lo Gone/)).toBeNull();
  fireEvent.keyDown(box, { key: 'ArrowDown' });
  expect(within(list).getByRole('option', { selected: true })).toHaveTextContent('Loan Pham');
  fireEvent.keyDown(box, { key: 'Enter' });
  expect(box).toHaveValue('Bank confirmed RF-2291-08. @Loan Pham ');
  expect(screen.queryByRole('listbox')).toBeNull();
  // Esc closes a list opened again without picking.
  fireEvent.change(box, { target: { value: 'Bank confirmed RF-2291-08. @Loan Pham @L', selectionStart: 41, selectionEnd: 41 } });
  expect(screen.getByRole('listbox')).toBeInTheDocument();
  fireEvent.keyDown(box, { key: 'Escape' });
  expect(screen.queryByRole('listbox')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: /Add private note/ }));
  expect(onSubmit).toHaveBeenCalledWith('note', 'Bank confirmed RF-2291-08. @Loan Pham @L', undefined, undefined, undefined, undefined, undefined, [loan.id]);
});

test('@ in a customer reply does nothing', () => {
  render(<ReplyComposer onSubmit={vi.fn()} teammates={askableTeammates([me, long], me.id)} />);
  const box = screen.getByRole('textbox', { name: 'Message body' });
  fireEvent.change(box, { target: { value: 'Hi @Lo', selectionStart: 6, selectionEnd: 6 } });
  expect(screen.queryByRole('listbox')).toBeNull();
});

const note: Message = {
  reply_number: null, id: '22222222-2222-4222-8222-222222222222', conversation_id: '11111111-1111-4111-8111-111111111111',
  author_agent_id: me.id, author_name: 'Hoàng Tú', agent_token: null, ai_draft_id: null, ai_draft_outcome: null, ai_draft_source: null, drafted_by: null,
  agent_request_id: null, kind: 'note', body: 'Bank confirmed RF-2291-08 lands by Thursday. @Long Bui can you check?', body_html: null, origin: null,
  attachments: [], cc: [], created_at: '2026-11-03T02:24:00Z', delivery: null, email: null,
  mentions: [{ agent_id: long.id, name: 'Long Bui', by_email: true, read_at: null }],
};

test('a posted note shows each mention in bold and a quiet line that says who was told, then when they saw it', async () => {
  const { rerender, container } = render(<MessageItem item={note} timezone="Asia/Ho_Chi_Minh" currentAgentId={me.id} />);
  await waitFor(() => expect(container.querySelector('.markdown-body strong')).toHaveTextContent('@Long Bui'));
  expect(screen.getByText('Long Bui was told · in Mentions and by email')).toBeInTheDocument();
  expect(container.querySelector('article')).toHaveAttribute('id', `message-${note.id}`);
  rerender(<MessageItem item={{ ...note, mentions: [{ agent_id: long.id, name: 'Long Bui', by_email: false, read_at: '2026-11-03T07:12:00Z' }] }} timezone="Asia/Ho_Chi_Minh" currentAgentId={me.id} />);
  expect(screen.getByText(/^Long Bui saw this Nov 3, 2:12\sPM$/)).toBeInTheDocument();
  // The person asked reads who asked them.
  rerender(<MessageItem item={note} timezone="Asia/Ho_Chi_Minh" currentAgentId={long.id} />);
  expect(screen.getByText('Hoàng Tú asked you · marked read when you opened it')).toBeInTheDocument();
});

const mention = (id: string, extra: Partial<Mention>): Mention => ({
  id, conversation_id: '11111111-1111-4111-8111-111111111111', message_id: note.id, subject: 'Refund still not on my card after 10 days', display_ref: '',
  excerpt: '@Long Bui can you check the provider shows it as settled?', author_agent_id: me.id, author_name: 'Hoàng Tú', agent_token: null,
  created_at: '2026-11-03T02:24:00Z', read_at: null, ...extra,
});

test('the Mentions list shows who asked, the subject, the quoted note, the time and an unread dot; a row opens its note', () => {
  const onOpen = vi.fn();
  const onMarkAllRead = vi.fn();
  const onShow = vi.fn();
  const items = [
    mention('30000000-0000-4000-8000-000000000001', {}),
    mention('30000000-0000-4000-8000-000000000002', { author_name: 'Mai Tran', agent_token: { id: '40000000-0000-4000-8000-000000000001', label: 'Claude', agent_name: 'Mai Tran' },
      subject: 'Courier fee refund', excerpt: '@Long Bui the courier fee refund needs your OK.', read_at: '2026-11-03T03:00:00Z' }),
  ];
  render(<MentionsList items={items} unread={1} show="unread" onShow={onShow} onOpen={onOpen} onMarkAllRead={onMarkAllRead} timezone="Asia/Ho_Chi_Minh" />);
  expect(screen.getByRole('button', { name: 'Unread 1' })).toHaveAttribute('aria-pressed', 'true');
  fireEvent.click(screen.getByRole('button', { name: 'All' }));
  expect(onShow).toHaveBeenCalledWith('all');
  const rows = screen.getAllByRole('link');
  expect(rows[0]).toHaveTextContent('Hoàng Tú');
  expect(rows[0]).toHaveTextContent('Refund still not on my card after 10 days');
  expect(rows[0]).toHaveTextContent('“@Long Bui can you check the provider shows it as settled?”');
  expect(rows[0]).toHaveTextContent('Nov 3');
  expect(within(rows[0]).getByText('Unread')).toBeInTheDocument();
  expect(rows[1]).toHaveTextContent('Mai Tran via Claude');
  expect(within(rows[1]).queryByText('Unread')).toBeNull();
  fireEvent.click(rows[0]);
  expect(onOpen).toHaveBeenCalledWith(items[0]);
  fireEvent.click(screen.getByRole('button', { name: 'Mark all read' }));
  expect(onMarkAllRead).toHaveBeenCalled();
});

test('an empty Mentions list says what puts a note there', () => {
  render(<MentionsList items={[]} unread={0} show="unread" onShow={vi.fn()} onOpen={vi.fn()} onMarkAllRead={vi.fn()} timezone="UTC" />);
  expect(screen.getByText(/No unread mentions/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Mark all read' })).toBeDisabled();
});
