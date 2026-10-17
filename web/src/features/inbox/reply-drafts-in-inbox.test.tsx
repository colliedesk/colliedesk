import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Message } from '../../api/schemas';
import { agentRequest } from '../requests/request.test.fixture';
import { InboxPage } from './inbox-page';
import { TimelinePanel } from './timeline-panel';

const conversation = {
  id: '22222222-2222-4222-8222-222222222222', contact_id: '55555555-5555-4555-8555-555555555555', contact_name: 'Mai Linh',
  subject: 'Printer jam', status: 'open' as const, priority: 'normal' as const, assignee_id: null, tags: [], origin: null,
  created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z',
};
const draft = agentRequest({ kind: 'reply_draft', method: 'POST', body: { kind: 'outgoing', body: 'Try turning it off and on.' },
  requested_by: { ...agentRequest().requested_by, token_label: 'Claude drafts', token_scope: 'draft' } });

test('the inbox marks a conversation with a waiting draft and shows the draft above the composer', async () => {
  const onSendDraft = vi.fn().mockResolvedValue(undefined);
  const onDiscardDraft = vi.fn().mockResolvedValue(undefined);
  render(<InboxPage conversations={[conversation]} selected={conversation} replyDrafts={[draft]} draftConversationIds={new Set([conversation.id])}
    onSendDraft={onSendDraft} onDiscardDraft={onDiscardDraft} />);
  const list = screen.getByRole('region', { name: /conversation list/i });
  expect(within(list).getByText('Draft waiting')).toBeInTheDocument();
  const card = screen.getByRole('region', { name: 'Reply draft by Ada Admin via Claude drafts' });
  fireEvent.click(within(card).getByRole('button', { name: 'Send' }));
  expect(onSendDraft).toHaveBeenCalledWith(draft, undefined);
  await waitFor(() => expect(within(card).getByRole('button', { name: 'Discard' })).toBeEnabled());
  fireEvent.click(within(card).getByRole('button', { name: 'Discard' }));
  expect(onDiscardDraft).toHaveBeenCalledWith(draft);
});

test('a reply sent from a draft says which token asked and who approved it', () => {
  const sent: Message = {
    id: '66666666-6666-4666-8666-666666666666', conversation_id: conversation.id, author_agent_id: '77777777-7777-4777-8777-777777777777',
    author_name: null, agent_token: { id: '33333333-3333-4333-8333-333333333333', label: 'Claude drafts', scope: 'draft', agent_name: 'Ada Admin' },
    agent_request_id: draft.id, kind: 'outgoing', body: 'Try turning it off and on.', body_html: null, origin: null, attachments: [], cc: [],
    created_at: '2026-10-17T09:05:00Z', delivery: null, email: null,
  };
  render(<TimelinePanel conversation={conversation} items={[sent]} timezone="UTC" agentNames={new Map([['77777777-7777-4777-8777-777777777777', 'Bo Agent']])} />);
  expect(screen.getByText('requested by Claude drafts, approved by Bo Agent')).toBeInTheDocument();
});

test('a reply whose draft a person rewrote says it came from an agent draft and who wrote the final text', () => {
  const rewritten: Message = {
    id: '88888888-8888-4888-8888-888888888888', conversation_id: conversation.id, author_agent_id: '77777777-7777-4777-8777-777777777777',
    author_name: null, agent_token: null, agent_request_id: draft.id, kind: 'outgoing', body: 'My own words.', body_html: null, origin: null,
    attachments: [], cc: [], created_at: '2026-10-17T09:05:00Z', delivery: null, email: null,
  };
  render(<TimelinePanel conversation={conversation} items={[rewritten]} timezone="UTC" agentNames={new Map([['77777777-7777-4777-8777-777777777777', 'Bo Agent']])} />);
  expect(screen.getByText('from an agent draft, edited and sent by Bo Agent')).toBeInTheDocument();
});
