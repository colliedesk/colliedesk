import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeAll, expect, test, vi } from 'vitest';
import type { Conversation, Message } from '../../api/schemas';
import { TimelinePanel } from './timeline-panel';
import { presenceViewPart } from './presence-part';

const base = { reply_number: null, conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, body_html: null, origin: null,
  attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', delivery: null, email: null, agent_token: null, ai_draft_id: null, ai_draft_outcome: null, agent_request_id: null };
const incoming: Message = {
  ...base, id: '22222222-2222-4222-8222-222222222222', kind: 'incoming', body: 'Please help.',
  email: { from: 'ceo@customer.example', authentication: 'fail', attachments: [{ filename: 'invoice.pdf', content_type: 'application/pdf' }], original_attachment_id: null },
};
const failed: Message = {
  ...base, id: '33333333-3333-4333-8333-333333333333', kind: 'outgoing', body: 'Answer.',
  delivery: { status: 'failed', attempts: 3, last_error: '550 mailbox unavailable', sent_at: null },
};
const queued: Message = { ...base, id: '44444444-4444-4444-8444-444444444444', kind: 'outgoing', body: 'Later.', delivery: { status: 'queued', attempts: 0, last_error: null, sent_at: null } };
const note: Message = { ...base, id: '55555555-5555-4555-8555-555555555555', kind: 'note', body: 'Internal.' };


// The presence chunk loads when somebody else is on a conversation, a reply is refused or a notice shows;
// start with it there, so no test ends while it is still on its way (vitest refuses a module asked for
// after the test environment is gone).
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
});

test('shows the sender, a verification warning and attachments that were not imported', () => {
  render(<TimelinePanel items={[incoming]} timezone="UTC" />);
  const message = screen.getByRole('article');
  expect(within(message).getByText('ceo@customer.example')).toBeInTheDocument();
  expect(within(message).getByText(/failed sender verification/i)).toBeInTheDocument();
  expect(within(message).getByText(/1 attachment not imported: invoice\.pdf/i)).toBeInTheDocument();
});

test('shows delivery status and retries a failed reply', () => {
  const onRetryDelivery = vi.fn();
  render(<TimelinePanel items={[failed, queued, note]} timezone="UTC" onRetryDelivery={onRetryDelivery} />);
  const [failedItem, queuedItem, noteItem] = screen.getAllByRole('article');
  expect(within(failedItem).getByText(/delivery failed/i)).toHaveAttribute('title', '550 mailbox unavailable');
  fireEvent.click(within(failedItem).getByRole('button', { name: /retry delivery/i }));
  expect(onRetryDelivery).toHaveBeenCalledWith(failed.id);
  expect(within(queuedItem).getByText(/queued/i)).toBeInTheDocument();
  expect(within(queuedItem).queryByRole('button', { name: /retry delivery/i })).not.toBeInTheDocument();
  expect(within(noteItem).queryByText(/queued|sent|failed/i)).not.toBeInTheDocument();
});

test('renders customer email as plain text without remote images or disguised links', () => {
  const hostile: Message = {
    ...base, id: '66666666-6666-4666-8666-666666666666', kind: 'incoming',
    email: { from: 'x@customer.example', authentication: 'none', attachments: [], original_attachment_id: null },
    body: '# Not a heading\n![](https://tracker.example/pixel.png?id=agent)\n[https://bank.example](https://evil.example/login)\nSee https://docs.example.com/help.',
  };
  const { container } = render(<TimelinePanel items={[hostile]} timezone="UTC" />);
  expect(container.querySelector('img')).toBeNull();
  expect(container.querySelector('h1')).toBeNull();
  expect(screen.getByText(/# Not a heading/)).toBeInTheDocument();
  const links = screen.getAllByRole('link');
  for (const link of links) {
    expect(link.textContent).toBe(link.getAttribute('href'));
    expect(link).toHaveAttribute('rel', 'noopener noreferrer nofollow');
  }
  expect(links.map((link) => link.getAttribute('href'))).toContain('https://docs.example.com/help');
});

test('lists stored email files and offers the original message separately', () => {
  const original = { id: '77777777-7777-4777-8777-777777777777', message_id: '88888888-8888-4888-8888-888888888888', filename: 'original-message.eml.gz',
    content_type: 'application/gzip', size: 2048, sha256: 'a'.repeat(64), inline: false, created_at: '2026-10-14T00:00:00Z' };
  const invoice = { ...original, id: '99999999-9999-4999-8999-999999999999', filename: 'invoice.pdf', content_type: 'application/pdf', size: 1024, sha256: 'b'.repeat(64) };
  const stored: Message = {
    ...base, id: original.message_id, kind: 'incoming', body: 'Invoice attached.', attachments: [original, invoice],
    email: { from: 'v@customer.example', authentication: 'pass', attachments: [{ filename: 'invoice.pdf', content_type: 'application/pdf' }], original_attachment_id: original.id },
  };
  render(<TimelinePanel items={[stored]} timezone="UTC" />);
  const message = screen.getByRole('article');
  expect(within(message).getByRole('link', { name: /download original email/i })).toHaveAttribute('href', `/api/v1/attachments/${original.id}`);
  const files = within(message).getByRole('list', { name: /attachments/i });
  expect(within(files).getByRole('link', { name: 'invoice.pdf' })).toHaveAttribute('href', `/api/v1/attachments/${invoice.id}`);
  expect(within(files).queryByText('original-message.eml.gz')).not.toBeInTheDocument();
  expect(within(message).queryByText(/not imported/i)).not.toBeInTheDocument();
});

test('names the agent token that wrote a message and leaves a person\'s message unlabelled', () => {
  const written: Message = {
    ...base, id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', kind: 'outgoing', body: 'Your refund is on its way.',
    author_agent_id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
    agent_token: { id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', label: 'Support bot', scope: 'send', agent_name: 'Ada Admin' }, ai_draft_id: null, ai_draft_outcome: null, agent_request_id: null,
  };
  render(<TimelinePanel items={[written, note]} timezone="UTC" />);
  const [byToken, byPerson] = screen.getAllByRole('article');
  expect(within(byToken).getByText('by Ada Admin via Support bot')).toBeInTheDocument();
  expect(within(byPerson).queryByText(/via/i)).not.toBeInTheDocument();
});

test('shows the loading status while the selected conversation timeline loads, not an empty timeline', () => {
  render(<TimelinePanel items={[]} timezone="UTC" loading />);
  expect(screen.getByRole('status')).toHaveTextContent('Loading workspace…');
});

test('shows no loading status once the timeline has loaded', () => {
  render(<TimelinePanel items={[note]} timezone="UTC" />);
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('an outgoing reply lists the files that went with it as downloads', () => {
  const sent: Message = { ...base, id: '66666666-6666-4666-8666-666666666666', kind: 'outgoing', body: 'Invoice attached.',
    attachments: [{ id: '77777777-7777-4777-8777-777777777777', message_id: '66666666-6666-4666-8666-666666666666', filename: 'invoice.pdf', content_type: 'application/pdf', size: 2048, sha256: 'a'.repeat(64), inline: false, created_at: '2026-10-14T00:00:00Z' }] };
  render(<TimelinePanel items={[sent]} timezone="UTC" />);
  const link = within(screen.getByRole('list', { name: 'Attachments' })).getByRole('link', { name: 'invoice.pdf' });
  expect(link).toHaveAttribute('href', '/api/v1/attachments/77777777-7777-4777-8777-777777777777');
  expect(screen.getByText('2.0 KiB')).toBeInTheDocument();
});

test('shows who a reply was copied to and who the customer copied', () => {
  const copied: Message = { ...failed, id: '66666666-6666-4666-8666-666666666666', cc: ['boss@customer.example', 'ops@customer.example'] };
  const customer: Message = { ...incoming, id: '77777777-7777-4777-8777-777777777777', email: { ...incoming.email!, cc: ['legal@customer.example'] } };
  render(<TimelinePanel items={[customer, copied, queued]} timezone="UTC" />);
  const [customerItem, copiedItem, plainItem] = screen.getAllByRole('article');
  expect(within(copiedItem).getByText('Cc: boss@customer.example, ops@customer.example')).toBeInTheDocument();
  expect(within(customerItem).getByText('Cc: legal@customer.example')).toBeInTheDocument();
  expect(within(plainItem).queryByText(/^Cc:/)).not.toBeInTheDocument();
});

test('a sent reply that missed a copied address says which one, without offering a retry', () => {
  const partly: Message = {
    ...base, id: '66666666-6666-4666-8666-666666666666', kind: 'outgoing', body: 'Copying your team.', cc: ['gone@customer.example'],
    delivery: { status: 'sent', attempts: 1, last_error: 'Sent to the customer, but not to: gone@customer.example (550 5.1.1 No such user)', sent_at: '2026-10-14T00:01:00Z' },
  };
  render(<TimelinePanel items={[partly]} timezone="UTC" onRetryDelivery={vi.fn()} />);
  const item = screen.getByRole('article');
  expect(within(item).getByText(/not to: gone@customer\.example/i)).toBeInTheDocument();
  expect(within(item).queryByRole('button', { name: /retry delivery/i })).not.toBeInTheDocument();
});

// Resolving never needs the details pane: the thread header has it, one press, and the way back.
const thread = { latest_reply_number: 0, id: '11111111-1111-4111-8111-111111111111', contact_id: '22222222-2222-4222-8222-222222222222',
  contact_name: 'Mai Linh', subject: 'Refund', status: 'open' as const, priority: 'normal' as const, assignee_id: null, tags: [],
  channel: 'email' as const, origin: null, created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z' };

test('an open or pending conversation has Resolve in its header, one press', async () => {
  for (const status of ['open', 'pending'] as const) {
    const onSetStatus = vi.fn().mockResolvedValue(undefined);
    const { unmount } = render(<TimelinePanel conversation={{ ...thread, status }} items={[]} timezone="UTC" onSetStatus={onSetStatus} />);
    fireEvent.click(screen.getByRole('button', { name: 'Resolve' }));
    expect(onSetStatus).toHaveBeenCalledWith('resolved');
    expect(screen.queryByRole('button', { name: 'Reopen' })).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Resolve' })).not.toHaveAttribute('aria-disabled'));
    unmount();
  }
});

test('a resolved or closed conversation offers Reopen instead', async () => {
  for (const status of ['resolved', 'closed'] as const) {
    const onSetStatus = vi.fn().mockResolvedValue(undefined);
    const { unmount } = render(<TimelinePanel conversation={{ ...thread, status }} items={[]} timezone="UTC" onSetStatus={onSetStatus} />);
    fireEvent.click(screen.getByRole('button', { name: 'Reopen' }));
    expect(onSetStatus).toHaveBeenCalledWith('open');
    expect(screen.queryByRole('button', { name: 'Resolve' })).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Reopen' })).not.toHaveAttribute('aria-disabled'));
    unmount();
  }
});

test('the header button ignores presses while the change runs, keeps the focus, and a failed change says so', async () => {
  let refuse!: (error: Error) => void;
  const onSetStatus = vi.fn(() => new Promise((_resolve, reject) => { refuse = reject; }));
  render(<TimelinePanel conversation={thread} items={[]} timezone="UTC" onSetStatus={onSetStatus} />);
  const resolve = screen.getByRole('button', { name: 'Resolve' });
  resolve.focus();
  fireEvent.click(resolve);
  expect(resolve).toHaveAttribute('aria-disabled', 'true');
  // It keeps the keyboard focus while the change runs, and a second press does nothing.
  expect(resolve).toHaveFocus();
  fireEvent.click(resolve);
  expect(onSetStatus).toHaveBeenCalledTimes(1);
  refuse(new Error('API 500'));
  expect(await screen.findByRole('alert')).toHaveTextContent('The status could not be changed. Try again.');
  expect(resolve).not.toHaveAttribute('aria-disabled');
});

const zaloRef = { id: '12121212-1212-4212-8212-121212121212', type: 'zalo', name: 'Hảo Đạt OA', enabled: true, deleted: false, replies_via: null };
test('a change that would tell the customer says so on the button, and its arrow offers the quiet one', async () => {
  const onSetStatus = vi.fn().mockResolvedValue(undefined);
  render(<TimelinePanel conversation={{ ...thread, channel_ref: zaloRef, status_notices: ['resolved', 'closed'] }} items={[]} timezone="UTC" onSetStatus={onSetStatus} />);
  const resolve = screen.getByRole('button', { name: 'Resolve and tell the customer on Hảo Đạt OA' });
  expect(resolve).toHaveTextContent('Resolve and tell');
  expect(resolve).not.toHaveAttribute('data-tip');
  const more = screen.getByLabelText('More ways to resolve', { selector: 'summary' });
  expect(more).toHaveAttribute('data-tip', 'More ways to resolve');
  fireEvent.click(within(screen.getByRole('group', { name: 'More ways to resolve' })).getByRole('button', { name: 'Resolve without telling the customer' }));
  expect(onSetStatus).toHaveBeenCalledWith('resolved', { notify: false });
  await waitFor(() => expect(resolve).not.toHaveAttribute('aria-disabled'));
  fireEvent.click(resolve);
  expect(onSetStatus).toHaveBeenLastCalledWith('resolved');
});

test('Reopen that tells offers a quiet reopen, to add a note; a change that tells nothing has no arrow', async () => {
  const { unmount } = render(<TimelinePanel conversation={{ ...thread, status: 'closed', channel_ref: zaloRef, status_notices: ['open'] }} items={[]} timezone="UTC" onSetStatus={vi.fn().mockResolvedValue(undefined)} />);
  expect(screen.getByRole('button', { name: 'Reopen and tell the customer on Hảo Đạt OA' })).toHaveTextContent('Reopen and tell');
  expect(within(screen.getByRole('group', { name: 'More ways to reopen' })).getByRole('button', { name: /Reopen without telling the customer/ })).toHaveTextContent('To add a note or fix a detail');
  unmount();
  render(<TimelinePanel conversation={{ ...thread, channel_ref: zaloRef, status_notices: ['open'] }} items={[]} timezone="UTC" onSetStatus={vi.fn()} />);
  expect(screen.getByRole('button', { name: 'Resolve' })).toBeInTheDocument();
  expect(screen.queryByLabelText('More ways to resolve')).not.toBeInTheDocument();
});

test('the last status change not made by a person on the team is one quiet line, at its time among the messages', () => {
  const later: Message = { ...incoming, id: '66666666-6666-4666-8666-666666666666', created_at: '2026-10-20T10:00:00Z' };
  const first: Message = { ...incoming, created_at: '2026-10-14T00:00:00Z' };
  const closed = { ...thread, status: 'closed' as const, channel_ref: zaloRef, last_status_change: { to: 'closed' as const, at: '2026-10-18T09:00:00Z', by: 'auto_close' as const } };
  const { unmount } = render(<TimelinePanel conversation={closed} items={[first, later]} timezone="UTC" canManageChannels />);
  const line = screen.getByText(/Closed by itself after staying resolved/).closest('p') as HTMLElement;
  expect(line).toHaveTextContent('Closed by itself after staying resolved (Hảo Đạt OA setting)');
  expect(within(line).getByRole('link', { name: 'Hảo Đạt OA' })).toHaveAttribute('href', `/settings/channels/${zaloRef.id}?tab=messages`);
  expect(line.querySelector('time')).toHaveAttribute('datetime', '2026-10-18T09:00:00Z');
  const stack = line.parentElement as HTMLElement;
  const order = Array.from(stack.children);
  expect(order.indexOf(line)).toBeGreaterThan(order.findIndex((node) => node.textContent?.includes(first.body)));
  expect(order.indexOf(line)).toBeLessThan(order.length - 1);
  unmount();
  render(<TimelinePanel conversation={closed} items={[first]} timezone="UTC" />);
  expect(screen.queryByRole('link', { name: 'Hảo Đạt OA' })).not.toBeInTheDocument();
  expect(screen.getByText(/Closed by itself after staying resolved \(Hảo Đạt OA setting\)/)).toBeInTheDocument();
});

test('a status line names who set it: a token, the customer, their Reopen command, the import; never a person on the team', () => {
  const cases = [
    [{ to: 'resolved', by: 'token', name: 'Claude (Queue triage)' }, 'Claude (Queue triage) resolved it'],
    [{ to: 'open', by: 'customer' }, 'Reopened when the customer wrote'],
    [{ to: 'open', by: 'command' }, 'Reopened by the customer’s Reopen command'],
    [{ to: 'resolved', by: 'import' }, 'Set to Resolved by the import'],
  ] as const;
  for (const [change, words] of cases) {
    const { unmount } = render(<TimelinePanel conversation={{ ...thread, last_status_change: { at: '2026-10-18T09:00:00Z', ...change } }} items={[]} timezone="UTC" />);
    expect(screen.getByText(words)).toBeInTheDocument();
    unmount();
  }
  render(<TimelinePanel conversation={{ ...thread, last_status_change: { to: 'resolved', at: '2026-10-18T09:00:00Z', by: 'agent', name: 'Ann' } }} items={[]} timezone="UTC" />);
  expect(document.querySelector('.status-event')).toBeNull();
});

test('a chat conversation whose notices are still unknown cannot be resolved yet; if they never come, the quiet way is offered', () => {
  const onSetStatus = vi.fn().mockResolvedValue(undefined);
  const { rerender } = render(<TimelinePanel conversation={{ ...thread, channel_ref: zaloRef }} items={[]} timezone="UTC" onSetStatus={onSetStatus} />);
  const resolve = screen.getByRole('button', { name: 'Resolve' });
  expect(resolve).toHaveAttribute('aria-disabled', 'true');
  expect(resolve).toHaveAttribute('aria-busy', 'true');
  fireEvent.click(resolve);
  expect(onSetStatus).not.toHaveBeenCalled();
  rerender(<TimelinePanel conversation={{ ...thread, channel_ref: zaloRef }} items={[]} timezone="UTC" onSetStatus={onSetStatus} noticesFailed />);
  expect(screen.getByRole('button', { name: 'Resolve' })).not.toHaveAttribute('aria-disabled');
  fireEvent.click(within(screen.getByRole('group', { name: 'More ways to resolve' })).getByRole('button', { name: 'Resolve without telling the customer' }));
  expect(onSetStatus).toHaveBeenCalledWith('resolved', { notify: false });
});

test('a chat conversation the server says tells nothing has a plain Resolve, ready at once', () => {
  render(<TimelinePanel conversation={{ ...thread, channel_ref: zaloRef, may_tell_customer: false }} items={[]} timezone="UTC" onSetStatus={vi.fn()} />);
  expect(screen.getByRole('button', { name: 'Resolve' })).not.toHaveAttribute('aria-disabled');
  expect(screen.queryByRole('group', { name: 'More ways to resolve' })).not.toBeInTheDocument();
});

test('an email conversation the server says would tell (a chat message moved in) offers the quiet way too', () => {
  render(<TimelinePanel conversation={{ ...thread, status_notices: ['resolved'] }} items={[]} timezone="UTC" onSetStatus={vi.fn()} />);
  expect(screen.getByRole('button', { name: 'Resolve and tell the customer on the channel' })).toBeInTheDocument();
  expect(screen.getByRole('group', { name: 'More ways to resolve' })).toBeInTheDocument();
});

test('no conversation, no status button', () => {
  render(<TimelinePanel items={[]} timezone="UTC" onSetStatus={vi.fn()} />);
  expect(screen.queryByRole('button', { name: 'Resolve' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Reopen' })).not.toBeInTheDocument();
});

test('a help chat conversation says its sender is unverified and links the chat it came from', async () => {
  render(<TimelinePanel conversation={{ ...thread, channel: 'help_chat' }} items={[]} timezone="UTC" />);
  expect(await screen.findByText('Unverified sender (Help chat)')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Open chat transcript' })).toHaveAttribute('href', `/settings/ai/conversations/${thread.id}/chat`);
});

test('another conversation shows neither', () => {
  render(<TimelinePanel conversation={thread} items={[]} timezone="UTC" />);
  expect(screen.queryByText('Unverified sender (Help chat)')).not.toBeInTheDocument();
});

test('the header shows a numbered conversation\'s code with a way to copy it; the default series shows none', () => {
  const conversation = (series: string, ref: string) => ({ id: base.conversation_id, subject: 'Máy in', status: 'open', priority: 'normal', contact_id: '88888888-8888-4888-8888-888888888888',
    contact_name: 'Nguyễn Văn A', tags: [], channel: 'zalo', number: 142, number_series: series, display_ref: ref }) as unknown as Conversation;
  const { unmount } = render(<TimelinePanel conversation={conversation('chd', 'CHD-2026-0142')} items={[]} timezone="UTC" />);
  expect(screen.getByText('CHD-2026-0142')).toHaveClass('conv-ref');
  expect(screen.getByRole('button', { name: 'Copy reference CHD-2026-0142' })).toBeInTheDocument();
  unmount();
  render(<TimelinePanel conversation={conversation('default', '#12')} items={[]} timezone="UTC" />);
  expect(screen.queryByText('#12')).toBeNull();
  expect(screen.queryByRole('button', { name: /Copy reference/ })).toBeNull();
});

test('the header says which clock is due or late in full words', () => {
  const clock = { state: 'running', due_at: null, warn_at: null, met_at: null, breached_at: null, due_soon: false, overdue: false, business_minutes_left: null };
  const conversation = { id: base.conversation_id, subject: 'Máy in', status: 'open', priority: 'urgent', contact_id: '88888888-8888-4888-8888-888888888888', contact_name: 'A', tags: [], channel: 'zalo',
    sla: { state: 'overdue', first_response: { ...clock, state: 'met', met_at: '2026-11-02T02:00:00Z' }, resolution: { ...clock, due_at: '2026-11-02T04:10:00Z', overdue: true } } } as unknown as Conversation;
  render(<TimelinePanel conversation={conversation} items={[]} timezone="UTC" />);
  expect(screen.getByText('Resolve overdue')).toHaveClass('sla-late');
});
