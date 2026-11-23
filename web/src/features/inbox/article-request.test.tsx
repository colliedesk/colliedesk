import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { Conversation, Message } from '../../api/schemas';
import { PermissionsContext } from '../../lib/permissions-context';
import { permissionsOf } from '../../test-roles';
import { MakeArticleMenu, RequestArticleButton } from './article-request';

const gaps = vi.hoisted(() => ({ similar: vi.fn(), ask: vi.fn(), withdraw: vi.fn(), fromReply: vi.fn() }));
const navigate = vi.hoisted(() => vi.fn());
vi.mock('../kb/gaps-api', () => ({ gapsApi: gaps }));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }));

const HERE = '11111111-1111-4111-8111-111111111111';
const CONTACT = '33333333-3333-4333-8333-333333333333';
const ASKED = '66666666-6666-4666-8666-666666666661';
const REPLY = '66666666-6666-4666-8666-666666666663';
const GAP = '88888888-8888-4888-8888-888888888888';
const ARTICLE = '99999999-9999-4999-8999-999999999999';

const conversation: Conversation = {
  latest_reply_number: 0, help_center: null, id: HERE, contact_id: CONTACT, contact_name: 'Mai Linh', subject: 'Locked out of my account', status: 'open', priority: 'normal',
  assignee_id: null, tags: [], channel: 'email', origin: null, created_at: '2026-10-25T08:00:00Z', updated_at: '2026-10-25T09:00:00Z',
};
const message = (id: string, body: string, kind: Message['kind']): Message => ({
  id, conversation_id: HERE, author_agent_id: null, author_name: 'Mai Linh', agent_token: null, agent_request_id: null, ai_draft_id: null,
  ai_draft_outcome: null, ai_draft_source: null, drafted_by: null, kind, body, body_html: null, origin: null, attachments: [], cc: [], created_at: '2026-10-25T08:00:00Z', delivery: null,
  email: null, reply_number: null,
});
const timeline = [message(ASKED, 'I cannot sign in since this morning.\nThe reset email never came.', 'incoming'), message(REPLY, 'Open Settings, then Reset password.', 'outgoing')];
const gap = (overrides = {}) => ({
  id: GAP, title: 'Reset a password', status: 'open', kinds: ['requested'], askers: 2, recent_askers: 2, conversation_count: 2, first_seen: '2026-11-20T09:00:00Z',
  last_seen: '2026-11-21T09:00:00Z', fixed_by_article: null, fixed_automatically: false, status_changed_by: null, status_changed_at: null, ...overrides,
});

let client: QueryClient;
const view = (ui: ReactElement, held = permissionsOf('agent')) =>
  render(<QueryClientProvider client={client}><PermissionsContext.Provider value={held}>{ui}</PermissionsContext.Provider></QueryClientProvider>);

beforeEach(() => {
  vi.clearAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  gaps.similar.mockResolvedValue({ items: [] });
});

test('a request for an article takes two presses: the title and one line come from the conversation', async () => {
  gaps.ask.mockResolvedValue({ gap: gap({ id: GAP, title: 'Locked out of my account', conversation_count: 1 }), created: true, added_to_existing: false });
  view(<RequestArticleButton conversation={conversation} timeline={timeline} />);
  fireEvent.click(screen.getByRole('button', { name: 'Request article' }));
  const dialog = screen.getByRole('dialog', { name: 'Request an article' });
  expect(within(dialog).getByLabelText('What should the article answer?')).toHaveValue('Locked out of my account');
  expect(within(dialog).getByLabelText('Note for the writer')).toHaveValue('I cannot sign in since this morning.');
  await waitFor(() => expect(gaps.similar).toHaveBeenCalledWith('Locked out of my account', expect.anything()));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Request' }));
  await waitFor(() => expect(gaps.ask).toHaveBeenCalledWith(HERE, { title: 'Locked out of my account', note: 'I cannot sign in since this morning.' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Article requested. It shows in Knowledge gaps.');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('a similar open request is offered, and the conversation is added to it instead of a duplicate', async () => {
  gaps.similar.mockResolvedValue({ items: [gap()] });
  gaps.ask.mockResolvedValue({ gap: gap({ conversation_count: 3 }), created: true, added_to_existing: true });
  view(<RequestArticleButton conversation={conversation} timeline={timeline} />);
  fireEvent.click(screen.getByRole('button', { name: 'Request article' }));
  const dialog = screen.getByRole('dialog', { name: 'Request an article' });
  expect(await within(dialog).findByText('A similar request is open: Reset a password, asked from 2 conversations.')).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add this conversation to it' }));
  await waitFor(() => expect(gaps.ask).toHaveBeenCalledWith(HERE, expect.objectContaining({ gap_id: GAP })));
  expect(await screen.findByRole('status')).toHaveTextContent('Added to Reset a password.');
});

test('Undo takes the request back, and a request made before has nothing to undo', async () => {
  gaps.ask.mockResolvedValueOnce({ gap: gap(), created: true, added_to_existing: false });
  gaps.withdraw.mockResolvedValue(undefined);
  view(<RequestArticleButton conversation={conversation} timeline={timeline} />);
  fireEvent.click(screen.getByRole('button', { name: 'Request article' }));
  fireEvent.click(screen.getByRole('button', { name: 'Request' }));
  fireEvent.click(within(await screen.findByRole('status')).getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(gaps.withdraw).toHaveBeenCalledWith(HERE));
  expect(await screen.findByRole('status')).toHaveTextContent('Request taken back.');

  gaps.ask.mockResolvedValueOnce({ gap: gap(), created: false, added_to_existing: false });
  fireEvent.click(screen.getByRole('button', { name: 'Request article' }));
  fireEvent.click(screen.getByRole('button', { name: 'Request' }));
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('This conversation already asked for Reset a password.'));
  expect(within(screen.getByRole('status')).queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
});

test('a role that may not read the knowledge base has no Request article', () => {
  view(<RequestArticleButton conversation={conversation} timeline={timeline} />, permissionsOf('agent').filter((permission) => permission !== 'kb_articles:read'));
  expect(screen.queryByRole('button', { name: 'Request article' })).not.toBeInTheDocument();
});

test('"Make an article" keeps a sent reply as a draft and opens it in the editor', async () => {
  gaps.fromReply.mockResolvedValue({ id: ARTICLE });
  view(<MakeArticleMenu conversation={conversation} message={timeline[1]} />, permissionsOf('admin'));
  fireEvent.click(screen.getByLabelText('Reply actions', { selector: 'summary' }));
  fireEvent.click(screen.getByRole('button', { name: 'Make an article' }));
  await waitFor(() => expect(gaps.fromReply).toHaveBeenCalledWith(HERE, REPLY, expect.stringMatching(/.{16,}/)));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/knowledge/articles/$articleId', params: { articleId: ARTICLE } }));
});

test('"Make an article" is not offered to a role that may not create articles', () => {
  view(<MakeArticleMenu conversation={conversation} message={timeline[1]} />, permissionsOf('admin').filter((permission) => permission !== 'kb_articles:create'));
  expect(screen.queryByLabelText('Reply actions', { selector: 'summary' })).not.toBeInTheDocument();
});
