import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InboxPage } from './inbox-page';

const conversation = {
  id: '11111111-1111-4111-8111-111111111111',
  contact_id: '22222222-2222-4222-8222-222222222222',
  subject: 'Example request',
  status: 'open' as const,
  priority: 'normal' as const,
  assignee_id: null,
  tags: [],
  origin: null,
  created_at: '2026-10-14T00:00:00Z',
  updated_at: '2026-10-14T00:00:00Z',
};

test('shows the dense inbox filters and three-pane conversation workspace', () => {
  render(<InboxPage />);
  const filters = screen.getByRole('form', { name: /inbox/i });
  expect(screen.getByRole('searchbox', { name: /search conversations/i })).toBeInTheDocument();
  expect(within(filters).getByLabelText(/status/i)).toBeInTheDocument();
  expect(within(filters).getByLabelText(/assignee/i)).toBeInTheDocument();
  expect(within(filters).getByLabelText(/tag/i)).toBeInTheDocument();
  expect(screen.getByRole('region', { name: /conversation list/i })).toBeInTheDocument();
  expect(screen.getByRole('region', { name: /timeline/i })).toBeInTheDocument();
  expect(screen.getByRole('complementary', { name: /conversation details/i })).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: /message body/i })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /^private note$/i }));
  expect(screen.getByRole('button', { name: /add private note/i })).toBeInTheDocument();
});

test('adds and removes tags through atomic conversation tag callbacks', () => {
  const onAddTag = vi.fn();
  const onRemoveTag = vi.fn();
  const tag = { id: '33333333-3333-4333-8333-333333333333', name: 'Billing', color: '#64748b' };
  const { rerender } = render(<InboxPage selected={conversation} conversations={[conversation]} tags={[tag]} onAddTag={onAddTag} onRemoveTag={onRemoveTag} />);
  fireEvent.click(screen.getByRole('checkbox', { name: /billing/i }));
  expect(onAddTag).toHaveBeenCalledWith(conversation.id, tag.id);
  rerender(<InboxPage selected={{ ...conversation, tags: [tag] }} conversations={[conversation]} tags={[tag]} onAddTag={onAddTag} onRemoveTag={onRemoveTag} />);
  fireEvent.click(screen.getByRole('checkbox', { name: /billing/i }));
  expect(onRemoveTag).toHaveBeenLastCalledWith(conversation.id, tag.id);
});

test('creates a tag inline from conversation details', () => {
  const onCreateTag = vi.fn();
  render(<InboxPage selected={conversation} conversations={[conversation]} onCreateTag={onCreateTag} />);
  fireEvent.change(screen.getByRole('textbox', { name: /new tag name/i }), { target: { value: 'Escalated' } });
  fireEvent.click(screen.getByRole('button', { name: /create tag/i }));
  expect(onCreateTag).toHaveBeenCalledWith(conversation.id, 'Escalated');
});

test('moves between conversation pages', () => {
  const onPage = vi.fn();
  render(<InboxPage page={2} hasNextPage onPage={onPage} />);
  fireEvent.click(screen.getByRole('button', { name: /previous page/i }));
  fireEvent.click(screen.getByRole('button', { name: /next page/i }));
  expect(onPage.mock.calls).toEqual([[1], [3]]);
});

test('renders message Markdown in the timeline', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} timeline={[{
    id: '44444444-4444-4444-8444-444444444444',
    conversation_id: conversation.id,
    author_agent_id: null,
    author_name: null,
    kind: 'outgoing',
    body: 'Use the **secure link**.',
    body_html: null,
    origin: null,
    attachments: [],
    created_at: '2026-10-14T00:00:00Z',
    delivery: null,
    email: null,
  }]} />);
  expect(screen.getByText('secure link').tagName).toBe('STRONG');
  expect(screen.getByRole('article')).toHaveTextContent('Reply');
});

test('tells the agent when replies will not be emailed', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} outboundNotice="Outgoing email is not configured on this server." />);
  expect(screen.getByRole('status')).toHaveTextContent('Outgoing email is not configured on this server.');
});

test('shows the author name and attachment download links on a timeline message', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} timeline={[{
    id: '55555555-5555-4555-8555-555555555555',
    conversation_id: conversation.id,
    author_agent_id: null,
    author_name: 'Grace Hopper',
    kind: 'incoming',
    body: 'Logs attached.',
    body_html: null,
    origin: null,
    attachments: [{
      id: '66666666-6666-4666-8666-666666666666',
      message_id: '55555555-5555-4555-8555-555555555555',
      filename: 'server.log',
      content_type: 'text/plain',
      size: 2048,
      sha256: 'b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9',
      created_at: '2026-10-14T00:00:00Z',
    }],
    created_at: '2026-10-14T00:00:00Z',
    delivery: null,
    email: null,
  }]} />);
  expect(screen.getByText('Grace Hopper')).toBeInTheDocument();
  const link = screen.getByRole('link', { name: /server\.log/ });
  expect(link).toHaveAttribute('href', '/api/v1/attachments/66666666-6666-4666-8666-666666666666');
  expect(link).toHaveAttribute('download');
  expect(screen.getByText('2.0 KiB')).toBeInTheDocument();
});

const imported = {
  id: '77777777-7777-4777-8777-777777777777',
  conversation_id: conversation.id,
  author_agent_id: null,
  author_name: 'Bob Former',
  kind: 'note' as const,
  body: 'Forwarding the logs',
  body_html: '<p onclick="steal()">Forwarding the <b>logs</b><img src="https://tracker.example/p.gif"></p>',
  origin: { system: 'freshdesk', external_id: '7004', kind: 'forward' as const, from: null, to: ['vendor@partner.example'], cc: ['boss@acme.example'], bcc: [] },
  attachments: [],
  created_at: '2026-10-14T00:00:00Z',
  delivery: null,
  email: null,
};

test('labels imported messages with their source kind and recipients', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} timeline={[imported]} />);
  expect(screen.getByText('Forwarded from Freshdesk')).toBeInTheDocument();
  expect(screen.getByText('To: vendor@partner.example')).toBeInTheDocument();
  expect(screen.getByText('Cc: boss@acme.example')).toBeInTheDocument();
});

test('shows the original HTML only inside a sandboxed frame that loads nothing', () => {
  const { container } = render(<InboxPage selected={conversation} conversations={[conversation]} timeline={[imported]} />);
  expect(container.querySelector('iframe')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: /show original/i }));
  const frame = screen.getByTitle('Original message');
  expect(frame.tagName).toBe('IFRAME');
  expect(frame).toHaveAttribute('sandbox', '');
  const document = frame.getAttribute('srcdoc') ?? '';
  expect(document).toContain("default-src 'none'");
  expect(document).toContain('<b>logs</b>');
  expect(container.querySelector('[onclick]')).toBeNull();
});

test('lists time entries with the total time logged', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} timeline={[]} timeEntries={[
    { id: '88888888-8888-4888-8888-888888888888', conversation_id: conversation.id, agent_id: null, agent_name: 'Alice Agent', minutes: 90, note: 'Call', billable: true, executed_at: '2025-01-16T02:00:00Z' },
    { id: '99999999-9999-4999-8999-999999999999', conversation_id: conversation.id, agent_id: null, agent_name: 'Bob Former', minutes: 15, note: '', billable: false, executed_at: '2025-01-16T07:00:00Z' },
  ]} />);
  const section = screen.getByRole('region', { name: /time logged/i });
  expect(within(section).getByText('Total 1 h 45 min')).toBeInTheDocument();
  expect(within(section).getByText(/Alice Agent/)).toBeInTheDocument();
  expect(within(section).getByText('Billable')).toBeInTheDocument();
});

test('shows where an imported conversation came from', () => {
  const origin = { system: 'freshdesk', external_id: '41', type: 'Problem', source: 'email', due_at: '2025-01-18T23:30:00Z', first_response_due_at: null, cc: [], forwarded_to: [] };
  render(<InboxPage selected={{ ...conversation, origin }} conversations={[conversation]} />);
  const details = screen.getByRole('complementary', { name: /conversation details/i });
  expect(within(details).getByText('Freshdesk ticket 41')).toBeInTheDocument();
  expect(within(details).getByText('Type: Problem')).toBeInTheDocument();
});

test('an empty new workspace explains the next steps instead of a filter message', () => {
  render(<InboxPage conversations={[]} canManage />);
  const start = screen.getByRole('region', { name: /get started/i });
  expect(start).toHaveTextContent('No conversations yet');
  expect(within(start).getByRole('link', { name: 'Connect your support email' })).toHaveAttribute('href', '/settings');
  expect(within(start).getByRole('link', { name: 'Import from Freshdesk' })).toHaveAttribute('href', 'https://colliedesk.com/docs/import-freshdesk');
  expect(within(start).getByRole('link', { name: 'Invite your team' })).toHaveAttribute('href', '/settings');
});

test('an empty filtered list says no conversation matches', () => {
  render(<InboxPage conversations={[]} filtered />);
  expect(screen.queryByRole('region', { name: /get started/i })).not.toBeInTheDocument();
  expect(screen.getByText('No conversations match these filters.')).toBeInTheDocument();
});

test('a list that failed to load says so and offers a retry, never "Get started"', () => {
  const onRetry = vi.fn();
  render(<InboxPage conversations={[]} failed onRetry={onRetry} />);
  expect(screen.queryByRole('region', { name: /get started/i })).not.toBeInTheDocument();
  expect(screen.queryByText('No conversations match these filters.')).not.toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent('Conversations could not be loaded.');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(onRetry).toHaveBeenCalled();
});

test('get-started links open Settings inside the app and hide admin-only steps from agents', () => {
  const onOpenSettings = vi.fn();
  const { rerender } = render(<InboxPage conversations={[]} canManage onOpenSettings={onOpenSettings} />);
  const link = screen.getByRole('link', { name: 'Connect your support email' });
  expect(fireEvent.click(link)).toBe(false); // default navigation (full page load) prevented
  expect(onOpenSettings).toHaveBeenCalled();
  rerender(<InboxPage conversations={[]} canManage={false} onOpenSettings={onOpenSettings} />);
  const start = screen.getByRole('region', { name: /get started/i });
  expect(within(start).queryByRole('link', { name: 'Invite your team' })).not.toBeInTheDocument();
  expect(within(start).queryByRole('link', { name: 'Connect your support email' })).not.toBeInTheDocument();
  expect(within(start).getByRole('link', { name: 'Import from Freshdesk' })).toBeInTheDocument();
});

test('details drawer takes focus when opened and returns it on Escape', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} />);
  const toggle = screen.getByRole('button', { name: /show details/i });
  expect(toggle).toHaveAttribute('aria-expanded', 'false');
  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute('aria-expanded', 'true');
  expect(screen.getByRole('button', { name: /hide details/i })).toHaveFocus();
  fireEvent.keyDown(screen.getByRole('complementary', { name: /conversation details/i }), { key: 'Escape' });
  expect(screen.queryByRole('button', { name: /hide details/i })).not.toBeInTheDocument();
  expect(toggle).toHaveFocus();
});

test('groups the list under Today and Earlier headings, only for groups that have rows', () => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-14T15:00:00Z'));
  try {
    const older = { ...conversation, id: '44444444-4444-4444-8444-444444444444', subject: 'Older request', updated_at: '2026-10-10T09:00:00Z' };
    const { rerender } = render(<InboxPage conversations={[conversation, older]} />);
    const list = screen.getByRole('region', { name: /conversation list/i });
    const headings = within(list).getAllByRole('heading', { level: 2 }).map((heading) => heading.textContent);
    expect(headings).toEqual(['Today', 'Earlier']);
    rerender(<InboxPage conversations={[conversation]} />);
    expect(screen.queryByRole('heading', { name: 'Earlier' })).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});

const other = { ...conversation, id: '55555555-5555-4555-8555-555555555555', subject: 'Another request' };
const openDrawer = () => fireEvent.click(screen.getByRole('button', { name: /show details/i }));

test('the details drawer closes when another conversation is selected', () => {
  const { rerender } = render(<InboxPage selected={conversation} conversations={[conversation, other]} />);
  openDrawer();
  expect(screen.getByRole('button', { name: /hide details/i })).toBeInTheDocument();
  rerender(<InboxPage selected={other} conversations={[conversation, other]} />);
  expect(screen.queryByRole('button', { name: /hide details/i })).not.toBeInTheDocument();
});

test('deselecting a conversation closes the drawer and keeps focus inside the list pane', () => {
  const { rerender } = render(<InboxPage selected={conversation} conversations={[conversation]} />);
  openDrawer();
  rerender(<InboxPage conversations={[conversation]} />);
  expect(screen.queryByRole('button', { name: /hide details/i })).not.toBeInTheDocument();
  expect(screen.getByRole('region', { name: /conversation list/i })).toHaveFocus();
});

test('Escape inside a select does not close the drawer, Escape elsewhere does', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} />);
  openDrawer();
  const panel = screen.getByRole('complementary', { name: /conversation details/i });
  fireEvent.keyDown(within(panel).getByLabelText(/^status$/i), { key: 'Escape' });
  expect(screen.getByRole('button', { name: /hide details/i })).toBeInTheDocument();
  fireEvent.keyDown(within(panel).getByLabelText(/new tag name/i), { key: 'Escape' });
  expect(screen.queryByRole('button', { name: /hide details/i })).not.toBeInTheDocument();
});

test('widening the window past the drawer breakpoint closes the drawer instead of stranding focus', () => {
  let listener: ((event: { matches: boolean }) => void) | undefined;
  vi.stubGlobal('matchMedia', () => ({
    matches: false, addEventListener: (_: string, fn: typeof listener) => { listener = fn; }, removeEventListener: () => { listener = undefined; },
  }));
  try {
    render(<InboxPage selected={conversation} conversations={[conversation]} />);
    openDrawer();
    act(() => listener?.({ matches: true }));
    expect(screen.queryByRole('button', { name: /hide details/i })).not.toBeInTheDocument();
  } finally {
    vi.unstubAllGlobals();
  }
});

test('a refusal shown for one conversation does not follow the agent to the next', async () => {
  const other = { ...conversation, id: '99999999-9999-4999-8999-999999999999', subject: 'Other request' };
  const onMessage = vi.fn().mockRejectedValue(new Error('refused'));
  const { rerender } = render(<InboxPage selected={conversation} conversations={[conversation, other]} onMessage={onMessage} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'For the first customer' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(await screen.findByRole('alert')).toHaveTextContent(/could not be sent/i);
  rerender(<InboxPage selected={other} conversations={[conversation, other]} onMessage={onMessage} />);
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.getByLabelText('Message body')).toHaveValue('');
});

test('a delivery notice from a retry is shown above the composer', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} deliveryNotice="Try again later." />);
  expect(screen.getByRole('alert')).toHaveTextContent('Try again later.');
});
