import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { Conversation, Message } from '../../api/schemas';
import { MessageIntakeLabel, MessageMoveMenu, MessageMovesLog } from './message-moves';

const { api, ConflictError, InvalidRequestError } = vi.hoisted(() => {
  class InvalidRequestError extends Error {}
  class ConflictError extends InvalidRequestError { constructor(message: string, readonly detail?: Record<string, unknown>) { super(message); } }
  return { api: { conversations: vi.fn(), conversation: vi.fn(), updateConversation: vi.fn() }, ConflictError, InvalidRequestError };
});
const moves = vi.hoisted(() => ({ list: vi.fn(), move: vi.fn(), undo: vi.fn(), intake: vi.fn() }));
vi.mock('../../api/client', () => ({ api, ConflictError, InvalidRequestError }));
vi.mock('./message-moves-api', () => ({ messageMovesApi: moves }));

const HERE = '11111111-1111-4111-8111-111111111111';
const THERE = '22222222-2222-4222-8222-222222222222';
const CONTACT = '33333333-3333-4333-8333-333333333333';
const ME = '44444444-4444-4444-8444-444444444444';
const ANN = '55555555-5555-4555-8555-555555555555';
const FIRST = '66666666-6666-4666-8666-666666666661';
const SECOND = '66666666-6666-4666-8666-666666666662';
const REPLY = '66666666-6666-4666-8666-666666666663';
const MOVE = '77777777-7777-4777-8777-777777777777';

const conversation = (overrides: Partial<Conversation> = {}): Conversation => ({
  latest_reply_number: 0, id: HERE, contact_id: CONTACT, contact_name: 'Mai Linh', subject: 'Printer jams', status: 'open', priority: 'normal',
  assignee_id: null, tags: [], channel: 'email', origin: null, created_at: '2026-10-25T08:00:00Z', updated_at: '2026-10-25T09:00:00Z', ...overrides,
});
const message = (id: string, body: string, kind: Message['kind'] = 'incoming'): Message => ({
  id, conversation_id: HERE, author_agent_id: null, author_name: 'Mai Linh', agent_token: null, agent_request_id: null, ai_draft_id: null,
  ai_draft_outcome: null, kind, body, body_html: null, origin: null, attachments: [], cc: [], created_at: '2026-10-25T08:00:00Z', delivery: null,
  email: null, reply_number: null,
});
const timeline = [message(FIRST, 'The printer jams\non every page'), message(SECOND, 'Also the invoice is wrong'), message(REPLY, 'On it', 'outgoing')];
const move = (overrides = {}) => ({
  id: MOVE, message_ids: [SECOND], from_conversation_id: HERE, to_conversation_id: THERE, created_conversation: false, closed_source: false,
  gathering_id: null, decision_id: null, moved_by_agent_id: ANN, via_token: false, moved_at: '2026-10-25T10:00:00Z', undone_at: null,
  undone_by_agent_id: null, undoable: true, ...overrides,
});
const intake = (gathering: string | null, rule: string | null = 'thread_reference', basis: string | null = 'match') =>
  ({ messages: [FIRST, SECOND].map((id) => ({ message_id: id, decision_basis: basis, decision_rule: rule, gathering_id: gathering })) });

let client: QueryClient;
const view = (ui: ReactElement) => render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);

beforeEach(() => {
  vi.clearAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  moves.list.mockResolvedValue({ items: [] });
  moves.intake.mockResolvedValue(intake(null));
  api.conversations.mockResolvedValue({ items: [conversation(), conversation({ id: THERE, subject: 'Invoice question', status: 'resolved' })], page: 1, page_size: 50, has_next_page: false });
  api.conversation.mockResolvedValue(conversation({ id: THERE, subject: 'Invoice question' }));
});

test('an email message moves to another conversation of its customer, never to its own; Take back to Review is for chat only', async () => {
  moves.move.mockResolvedValue({ move: move(), warnings: [] });
  view(<MessageMoveMenu conversation={conversation()} message={timeline[1]} timeline={timeline} timezone="UTC" />);
  await waitFor(() => expect(moves.intake).toHaveBeenCalledWith(HERE));
  fireEvent.click(screen.getByLabelText('Move this message', { selector: 'summary' }));
  expect(screen.queryByRole('button', { name: 'Take back to Review' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Move to another conversation…' }));
  const dialog = screen.getByRole('dialog', { name: 'Move to another conversation' });
  expect(within(dialog).getByRole('checkbox', { name: /Also the invoice is wrong/ })).toBeChecked();
  expect(within(dialog).getByRole('checkbox', { name: /The printer jams/ })).not.toBeChecked();
  // With nothing typed, the customer's other conversations; the open one is not offered.
  await within(dialog).findByRole('radio', { name: /Invoice question/ });
  expect(api.conversations.mock.calls[0][0]).toContain(`contact=${CONTACT}`);
  expect(within(dialog).queryByRole('radio', { name: /Printer jams/ })).not.toBeInTheDocument();
  expect(within(dialog).getByText('Reopens when they arrive')).toBeInTheDocument();
  // Closing as no new request is offered only when every customer message goes.
  expect(within(dialog).queryByRole('checkbox', { name: /Close this conversation/ })).not.toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole('radio', { name: /Invoice question/ }));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
  await waitFor(() => expect(moves.move).toHaveBeenCalledWith(HERE, { message_ids: [SECOND], to: { conversation_id: THERE } }, expect.any(String)));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
});

test('moving every customer message can close the conversation; a refusal stays in the dialog', async () => {
  moves.move.mockRejectedValue(new ConflictError('Someone replied in this conversation, so it cannot close; move the messages alone.', { code: 'source_has_activity' }));
  view(<MessageMoveMenu conversation={conversation()} message={timeline[0]} timeline={timeline} timezone="UTC" />);
  fireEvent.click(screen.getByLabelText('Move this message', { selector: 'summary' }));
  fireEvent.click(screen.getByRole('button', { name: 'Move to another conversation…' }));
  const dialog = screen.getByRole('dialog');
  fireEvent.click(within(dialog).getByRole('checkbox', { name: /Also the invoice is wrong/ }));
  fireEvent.click(within(dialog).getByRole('checkbox', { name: /Close this conversation/ }));
  fireEvent.click(await within(dialog).findByRole('radio', { name: /Invoice question/ }));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('cannot close');
  expect(moves.move).toHaveBeenCalledWith(HERE, { message_ids: [FIRST, SECOND], to: { conversation_id: THERE }, close_source: true }, expect.any(String));
});

test('a split opens a conversation with a subject from the message\'s first line', async () => {
  moves.move.mockResolvedValue({ move: move({ created_conversation: true }), warnings: [] });
  view(<MessageMoveMenu conversation={conversation()} message={timeline[0]} timeline={timeline} timezone="UTC" />);
  fireEvent.click(screen.getByLabelText('Move this message', { selector: 'summary' }));
  fireEvent.click(screen.getByRole('button', { name: 'Split into a new conversation…' }));
  const dialog = screen.getByRole('dialog', { name: 'Split into a new conversation' });
  expect(within(dialog).getByLabelText('Subject of the new conversation')).toHaveValue('The printer jams');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Split' }));
  await waitFor(() => expect(moves.move).toHaveBeenCalledWith(HERE, { message_ids: [FIRST], to: { new: { subject: 'The printer jams' } } }, expect.any(String)));
});

test('a chat message says why it is here and goes back to Review in one press', async () => {
  moves.intake.mockResolvedValue(intake('88888888-8888-4888-8888-888888888888'));
  moves.move.mockResolvedValue({ move: move({ to_conversation_id: null }), warnings: [] });
  view(<><MessageIntakeLabel conversation={conversation()} message={timeline[0]} />
    <MessageMoveMenu conversation={conversation({ channel: 'loopback' })} message={timeline[0]} timeline={timeline} timezone="UTC" /></>);
  expect(await screen.findByText('Added: a reply to a message here')).toBeInTheDocument();
  fireEvent.click(screen.getByLabelText('Move this message', { selector: 'summary' }));
  fireEvent.click(screen.getByRole('button', { name: 'Take back to Review' }));
  await waitFor(() => expect(moves.move).toHaveBeenCalledWith(HERE, { message_ids: [FIRST], to: { queue: true } }, expect.any(String)));
});

test('an email message has no label: every one is the rules\'', async () => {
  const { container } = view(<MessageIntakeLabel conversation={conversation()} message={timeline[0]} />);
  await waitFor(() => expect(moves.intake).toHaveBeenCalled());
  expect(container).toBeEmptyDOMElement();
});

test('the log says who moved what where, with Undo while it can be undone, and why it cannot', async () => {
  moves.list.mockResolvedValue({ items: [move(), move({ id: '77777777-7777-4777-8777-777777777778', to_conversation_id: null, moved_by_agent_id: ME, undoable: false, undone_at: '2026-10-25T11:00:00Z', undone_by_agent_id: ANN })] });
  moves.undo.mockRejectedValue(new ConflictError('superseded', { code: 'move_superseded' }));
  view(<MessageMovesLog conversation={conversation()} timeline={timeline} timezone="UTC" currentAgentId={ME} agentNames={new Map([[ANN, 'Ann']])} />);
  const log = await screen.findByRole('region', { name: 'Moves' });
  expect(await within(log).findByRole('link', { name: 'Invoice question' })).toHaveAttribute('href', `/conversations/${THERE}`);
  expect(log).toHaveTextContent('Ann moved 1 message to Invoice question');
  expect(log).toHaveTextContent('You took 1 message back to Review');
  expect(log).toHaveTextContent('Undone by Ann');
  expect(within(log).getAllByRole('button', { name: 'Undo' })).toHaveLength(1);
  expect(within(log).getByRole('button', { name: 'Undo' })).toHaveAccessibleDescription(/Ann moved 1 message to Invoice question/);
  expect(within(log).getByRole('link', { name: 'Open Review' })).toHaveAttribute('href', '/review');
  fireEvent.click(within(log).getByRole('button', { name: 'Undo' }));
  expect(await within(log).findByRole('alert')).toHaveTextContent('a message was moved again since');
  expect(moves.undo).toHaveBeenCalledWith(MOVE, expect.any(String));
});

test('a move made here keeps its warnings in its line', async () => {
  client.setQueryData(['message-move-warnings', MOVE], ['other_contact']);
  moves.list.mockResolvedValue({ items: [move({ from_conversation_id: THERE, to_conversation_id: HERE })] });
  view(<MessageMovesLog conversation={conversation()} timeline={timeline} timezone="UTC" />);
  const log = await screen.findByRole('region', { name: 'Moves' });
  expect(log).toHaveTextContent('A teammate moved 1 message here from');
  expect(log).toHaveTextContent('belongs to another customer');
});

test('a chat conversation nobody worked on offers to move all of it as no new request; a worked one does not', async () => {
  moves.intake.mockResolvedValue(intake('88888888-8888-4888-8888-888888888888', 'new_thread', 'default'));
  const customerOnly = timeline.slice(0, 2);
  const { unmount } = view(<MessageMovesLog conversation={conversation({ channel: 'loopback' })} timeline={customerOnly} timezone="UTC" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Not a new request? Move it…' }));
  const dialog = screen.getByRole('dialog', { name: 'Move to another conversation' });
  expect(within(dialog).getByRole('checkbox', { name: /The printer jams/ })).toBeChecked();
  expect(within(dialog).getByRole('checkbox', { name: /Also the invoice is wrong/ })).toBeChecked();
  expect(within(dialog).getByRole('checkbox', { name: /Close this conversation/ })).toBeChecked();
  unmount();
  // The channel's default assignee is no person's work: the server still lets it close.
  const assigned = view(<MessageMovesLog conversation={conversation({ channel: 'loopback', assignee_id: ME })} timeline={customerOnly} timezone="UTC" />);
  expect(await screen.findByRole('button', { name: 'Not a new request? Move it…' })).toBeInTheDocument();
  assigned.unmount();
  view(<MessageMovesLog conversation={conversation({ channel: 'loopback' })} timeline={timeline} timezone="UTC" />);
  await waitFor(() => expect(moves.intake).toHaveBeenCalledTimes(3));
  expect(screen.queryByRole('button', { name: 'Not a new request? Move it…' })).not.toBeInTheDocument();
});

test('the channel\'s own received notice is no work of a person: the conversation still offers to move all of it', async () => {
  moves.intake.mockResolvedValue(intake('88888888-8888-4888-8888-888888888888', 'new_thread', 'default'));
  const notice = { ...message(REPLY, 'Đã nhận yêu cầu', 'outgoing'), notice_kind: 'received' };
  const { unmount } = view(<MessageMovesLog conversation={conversation({ channel: 'loopback' })} timeline={[...timeline.slice(0, 2), notice]} timezone="UTC" />);
  expect(await screen.findByRole('button', { name: 'Not a new request? Move it…' })).toBeInTheDocument();
  unmount();
  view(<MessageMovesLog conversation={conversation({ channel: 'loopback' })} timeline={[...timeline.slice(0, 2), notice, message('99999999-9999-4999-8999-999999999999', 'On it', 'outgoing')]} timezone="UTC" />);
  await waitFor(() => expect(moves.intake).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole('button', { name: 'Not a new request? Move it…' })).not.toBeInTheDocument();
});

test('a thank-you after a conversation was resolved says it kept it resolved, with Reopen', async () => {
  api.updateConversation.mockResolvedValue(conversation({ status: 'open' }));
  const thanks = { ...message(FIRST, 'cảm ơn em'), intake_reply_kind: 'acknowledgement' as const };
  const { rerender } = view(<MessageIntakeLabel conversation={conversation({ status: 'resolved' })} message={thanks} />);
  expect(screen.getByText(/^Written after it was resolved · kept Resolved/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Reopen' }));
  await waitFor(() => expect(api.updateConversation).toHaveBeenCalledWith(HERE, { status: 'open' }));
  rerender(<QueryClientProvider client={client}><MessageIntakeLabel conversation={conversation({ status: 'open' })} message={thanks} /></QueryClientProvider>);
  expect(screen.getByText(/^Written after it was resolved · kept Resolved/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Reopen' })).not.toBeInTheDocument();
});

test('the move dialog takes the focus when it opens, and Escape closes it', async () => {
  view(<MessageMoveMenu conversation={conversation()} message={timeline[1]} timeline={timeline} timezone="UTC" />);
  fireEvent.click(screen.getByLabelText('Move this message', { selector: 'summary' }));
  fireEvent.click(screen.getByRole('button', { name: 'Split into a new conversation…' }));
  const dialog = screen.getByRole('dialog', { name: 'Split into a new conversation' });
  expect(dialog.contains(document.activeElement)).toBe(true);
  fireEvent.keyDown(document.activeElement as Element, { key: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
});
