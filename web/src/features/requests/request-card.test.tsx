import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { expect, test, vi } from 'vitest';
import { track } from '../../lib/product-analytics';
import { type api, InvalidRequestError } from '../../api/client';
import { agentRequest } from './request.test.fixture';
import { RequestCard } from './request-card';
import { canApproveRequest } from './request-call';

vi.mock('../../lib/product-analytics', () => ({ track: vi.fn(() => true) }));
const analytics = { track: vi.mocked(track) };

const actions = () => ({ onApprove: vi.fn().mockResolvedValue({ request: agentRequest({ status: 'approved' }), result: null }), onReject: vi.fn().mockResolvedValue(undefined) });

test('a pending request says what it does, who asked through which token, and the exact call', () => {
  render(<RequestCard item={agentRequest()} timezone="UTC" {...actions()} />);
  expect(screen.getByRole('heading', { name: 'Change a conversation: status pending' })).toBeInTheDocument();
  expect(screen.getByText('Asked by Ada Admin via Claude triage (read)')).toBeInTheDocument();
  expect(screen.getByText('PATCH /api/v1/conversations/22222222-2222-4222-8222-222222222222')).toBeInTheDocument();
  expect(screen.getByText(/"status": "pending"/)).toBeInTheDocument();
});

test('Approve runs it once and shows a link or a credential the call answered, once', async () => {
  const handlers = actions();
  handlers.onApprove.mockResolvedValue({ request: agentRequest({ status: 'approved', result_status: 201, result_note: 'Shown once.' }),
    result: { url: '/api/v1/export/download/abc', expires_in_seconds: 60 } });
  render(<RequestCard item={agentRequest({ summary: 'Export all workspace data', method: 'POST', path: '/api/v1/export/link', body: null })} timezone="UTC" {...handlers} />);
  fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
  expect(handlers.onApprove).toHaveBeenCalledWith('11111111-1111-4111-8111-111111111111');
  expect(await screen.findByRole('link', { name: 'Open /api/v1/export/download/abc' })).toHaveAttribute('href', '/api/v1/export/download/abc');
  expect(screen.getByText(/only now/)).toBeInTheDocument();
});

test('Reject sends the optional reason; a refusal from the server is shown', async () => {
  const handlers = actions();
  handlers.onReject.mockRejectedValueOnce(new InvalidRequestError('This request was already approved, rejected, cancelled or expired.'));
  render(<RequestCard item={agentRequest()} timezone="UTC" {...handlers} />);
  fireEvent.change(screen.getByLabelText('Reason (optional)'), { target: { value: 'Not this week' } });
  fireEvent.click(screen.getByRole('button', { name: 'Reject' }));
  expect(handlers.onReject).toHaveBeenCalledWith('11111111-1111-4111-8111-111111111111', 'Not this week');
  expect(await screen.findByRole('alert')).toHaveTextContent('already approved');
  await waitFor(() => expect(screen.getByRole('button', { name: 'Reject' })).toBeEnabled());
});

test('a request of a revoked token can only be rejected', () => {
  render(<RequestCard item={agentRequest({ requested_by: { ...agentRequest().requested_by, token_revoked: true } })} timezone="UTC" {...actions()} />);
  expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled();
  expect(screen.getByText('The token that asked was revoked.')).toBeInTheDocument();
});

test('a decided request shows who decided and how the call went, with no buttons', () => {
  render(<RequestCard item={agentRequest({ status: 'failed', decided_by: { agent_id: null, name: 'Bo Agent' }, decided_at: '2026-10-17T10:00:00Z', result_status: 403 })}
    timezone="UTC" {...actions()} />);
  expect(screen.getByText('Failed')).toBeInTheDocument();
  expect(screen.getByText(/Approved by Bo Agent.*answered 403/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument();
});

test('a reply draft shows its text as the customer would read it', () => {
  render(<RequestCard item={agentRequest({ kind: 'reply_draft', body: { kind: 'outgoing', body: 'Hello Mai,\nplease restart it.' } })} timezone="UTC" {...actions()} />);
  expect(screen.getByText((_, node) => node?.textContent === 'Hello Mai,\nplease restart it.' && node.tagName === 'P')).toBeInTheDocument();
  expect(screen.queryByText(/"kind"/)).not.toBeInTheDocument();
});

test('a reply draft shows its copies, and an approval the person may not run is left to an admin', () => {
  render(<RequestCard item={agentRequest({ kind: 'reply_draft', approvable_by: 'admin', body: { kind: 'outgoing', body: 'Hi', cc: ['boss@cust.example.com'] } })}
    canApprove={false} timezone="UTC" {...actions()} />);
  expect(screen.getByText('Cc boss@cust.example.com')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument();
  expect(screen.getByText('Only an admin can approve this.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Reject' })).toBeInTheDocument();
});

test('an edited reply draft shows who edited it and what was sent', () => {
  render(<RequestCard item={agentRequest({ kind: 'reply_draft', status: 'approved', decided_by: { agent_id: null, name: 'Bo Agent' },
    body: { kind: 'outgoing', body: 'Agent text' }, edited_body: { kind: 'outgoing', body: 'Person text' } })} timezone="UTC" {...actions()} />);
  expect(screen.getByText('Edited by Bo Agent before sending')).toBeInTheDocument();
  expect(screen.getByText('Person text')).toBeInTheDocument();
});

const replyRequest = (overrides: Parameters<typeof agentRequest>[0]) => agentRequest({
  route: '/api/v1/saved-replies/{id}', path: '/api/v1/saved-replies/55555555-5555-4555-8555-555555555555', conversation_id: null, ...overrides,
});
const before = { title: 'Refund', body: 'We refunded you.', updated_at: '2026-10-16T09:00:00Z' };

test('a proposed edit of a saved reply shows the reply before and after', () => {
  render(<RequestCard item={replyRequest({ summary: 'Edit saved reply "Refund"', body: { title: 'Refund done', body: 'We refunded you today.', updated_at: before.updated_at },
    saved_reply: { id: '55555555-5555-4555-8555-555555555555', before, current: { ...before, deleted_at: null }, restores: null } })} timezone="UTC" {...actions()} />);
  const proposal = screen.getByRole('group', { name: 'Proposed change to the saved reply' });
  expect(within(proposal).getByText('We refunded you.').closest('del')).not.toBeNull();
  expect(within(proposal).getByText('We refunded you today.').closest('ins')).not.toBeNull();
  expect(within(proposal).getByText('Refund done').closest('ins')).not.toBeNull();
  expect(screen.queryByText(/"updated_at"/)).not.toBeInTheDocument();
  expect(screen.queryByText(/changed since/)).not.toBeInTheDocument();
});

test('a proposal says when the saved reply changed after the agent asked', () => {
  render(<RequestCard item={replyRequest({ body: { body: 'New text', updated_at: before.updated_at },
    saved_reply: { id: '55555555-5555-4555-8555-555555555555', before, current: { ...before, body: 'Edited by a person', updated_at: '2026-10-17T08:00:00Z', deleted_at: null }, restores: null } })} timezone="UTC" {...actions()} />);
  expect(screen.getByText(/changed since the agent asked/)).toBeInTheDocument();
  expect(screen.getByText('Edited by a person')).toBeInTheDocument();
});

test('a proposed new saved reply shows its text, and a proposed delete shows what would go to the trash', () => {
  const { unmount } = render(<RequestCard item={replyRequest({ method: 'POST', route: '/api/v1/saved-replies', path: '/api/v1/saved-replies', body: { title: 'Greeting', body: 'Hello there.' } })} timezone="UTC" {...actions()} />);
  const created = screen.getByRole('group', { name: 'Proposed change to the saved reply' });
  expect(within(created).getByText('Hello there.').closest('ins')).not.toBeNull();
  unmount();
  render(<RequestCard item={replyRequest({ method: 'DELETE', body: null, saved_reply: { id: '55555555-5555-4555-8555-555555555555', before, current: { ...before, deleted_at: null }, restores: null } })} timezone="UTC" {...actions()} />);
  const deleted = screen.getByRole('group', { name: 'Proposed change to the saved reply' });
  expect(within(deleted).getByText('We refunded you.').closest('del')).not.toBeNull();
  expect(within(deleted).getByText(/trash for 30 days/)).toBeInTheDocument();
});

test('a proposed version restore shows the text it would bring back', () => {
  render(<RequestCard item={replyRequest({ method: 'POST', route: '/api/v1/saved-replies/{id}/versions/{version}/restore',
    path: '/api/v1/saved-replies/55555555-5555-4555-8555-555555555555/versions/1/restore', body: { updated_at: before.updated_at },
    saved_reply: { id: '55555555-5555-4555-8555-555555555555', before, current: { ...before, deleted_at: null }, restores: { version: 1, title: 'Refunds', body: 'Refunded in 5 days.' } } })} timezone="UTC" {...actions()} />);
  const proposal = screen.getByRole('group', { name: 'Proposed change to the saved reply' });
  expect(within(proposal).getByText('We refunded you.').closest('del')).not.toBeNull();
  expect(within(proposal).getByText('Refunded in 5 days.').closest('ins')).not.toBeNull();
  expect(within(proposal).getByText('Refunds').closest('ins')).not.toBeNull();
  expect(within(proposal).getByText(/version 1/)).toBeInTheDocument();
});

test('a request that changes the token owner\'s own settings is approved only by that person', () => {
  const item = agentRequest({ approvable_by: 'owner', summary: 'Change the timezone of ada@example.com: UTC → Asia/Tokyo' });
  const owner = item.requested_by.agent_id!;
  expect(canApproveRequest(item, { id: owner, role: 'agent' })).toBe(true);
  expect(canApproveRequest(item, { id: '99999999-9999-4999-8999-999999999999', role: 'admin' })).toBe(false);
  expect(canApproveRequest(agentRequest({ approvable_by: 'admin' }), { id: owner, role: 'agent' })).toBe(false);
  expect(canApproveRequest(agentRequest({ approvable_by: 'agent' }), { id: owner, role: 'agent' })).toBe(true);
  render(<RequestCard item={item} canApprove={false} timezone="UTC" {...actions()} />);
  expect(screen.getByText('Only the person who owns this token can approve it: it changes their own settings.')).toBeInTheDocument();
});

test('a decision reports approved or rejected only after the server accepted it', async () => {
  const handlers = actions();
  const { rerender } = render(<RequestCard item={agentRequest()} timezone="UTC" {...handlers} />);
  fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
  await waitFor(() => expect(analytics.track).toHaveBeenCalledWith('agent_request_decided', { decision: 'approved' }));
  analytics.track.mockClear();
  handlers.onReject.mockRejectedValueOnce(new InvalidRequestError('refused'));
  rerender(<RequestCard key="again" item={agentRequest()} timezone="UTC" {...handlers} />);
  fireEvent.click(screen.getByRole('button', { name: 'Reject' }));
  await screen.findByRole('alert');
  expect(analytics.track).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Reject' }));
  await waitFor(() => expect(analytics.track).toHaveBeenCalledWith('agent_request_decided', { decision: 'rejected' }));
});

type PublicConfig = Awaited<ReturnType<typeof api.config>>;
const selfHosted: PublicConfig = {
  app_origin: 'https://desk.example.com', workspace_hosts: false, cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false, switched_off: false }, signup: { enabled: false },
  limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 },
  product_analytics: { enabled: false, key: null, host: null, realm: null },
  ai: { server_provider: false },
  autofix: { available: false, missing_settings: [], off_reason: null },
  features: { gmail: { enabled: true }, microsoft: { enabled: true }, autofix: { enabled: false }, ai_server: { enabled: false }, hosted_mail: { enabled: false },
    cloud_sign_in: { enabled: false }, billing: { enabled: false }, signup: { enabled: false } },
};
const cloud: PublicConfig = { ...selfHosted, cloud_sign_in: { enabled: true, publishable_key: 'pk_test' }, billing: { enabled: true, switched_off: false }, signup: { enabled: true } };
const gmailConnect = agentRequest({ summary: 'Connect Gmail for sending', method: 'POST', route: '/api/v1/channels/email/gmail', path: '/api/v1/channels/email/gmail', body: null, approvable_by: 'admin' });
const consent = { request: { ...gmailConnect, status: 'approved' as const, result_status: 200 }, result: { url: 'https://accounts.google.com/o/oauth2/v2/auth?state=s' } };

function renderWithConfig(ui: ReactElement, config: PublicConfig) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(['public-config'], config);
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

test('on Colliedesk Cloud, approving a Gmail connection says how Google data is handled before the link to Google', async () => {
  renderWithConfig(<RequestCard item={gmailConnect} initialOutcome={consent} timezone="UTC" {...actions()} />, cloud);
  const privacy = await screen.findByRole('link', { name: 'Privacy policy' });
  expect(privacy).toHaveAttribute('href', 'https://colliedesk.com/privacy#google-user-data');
  expect(screen.getByText(/including the Limited Use requirements/)).toBeInTheDocument();
  const google = screen.getByRole('link', { name: /accounts\.google\.com/ });
  expect(privacy.compareDocumentPosition(google) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test('a self-hosted server approving a Gmail connection does not point to the privacy policy of Colliedesk Cloud', async () => {
  // Loaded first, so the absence below is the decision and not a chunk still on its way.
  await import('../settings/gmail-connection');
  renderWithConfig(<RequestCard item={gmailConnect} initialOutcome={consent} timezone="UTC" {...actions()} />, selfHosted);
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
  expect(screen.getByRole('link', { name: /accounts\.google\.com/ })).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: 'Privacy policy' })).not.toBeInTheDocument();
  expect(screen.queryByText(/Limited Use/)).not.toBeInTheDocument();
});

test('a custom token\'s request lists what the token held when it asked', () => {
  const base = agentRequest();
  render(<RequestCard item={agentRequest({ requested_by: { ...base.requested_by, token_scope: 'custom', token_permissions: ['kb_articles:read', 'kb_articles:update'] } })} timezone="UTC" {...actions()} />);
  expect(screen.getByText('What the token may do (2 permissions)')).toBeInTheDocument();
  expect(screen.getByText('Knowledge base articles: Edit')).toBeInTheDocument();
});

test('approving Microsoft 365 for a channel says a work account may need its admin first; Gmail for a channel keeps Google\'s statement', async () => {
  const microsoftConnect = agentRequest({ summary: 'Connect Microsoft 365 to the channel', method: 'POST', route: '/api/v1/channels/{id}/email/microsoft',
    path: '/api/v1/channels/11111111-1111-4111-8111-111111111111/email/microsoft', body: null, approvable_by: 'admin' });
  const { unmount } = renderWithConfig(<RequestCard item={microsoftConnect} timezone="UTC" {...actions()} />, cloud);
  expect(await screen.findByText(/A work account may need its Microsoft 365 admin's approval first/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Privacy policy' })).toHaveAttribute('href', 'https://colliedesk.com/privacy#microsoft-user-data');
  unmount();
  const gmailForChannel = { ...gmailConnect, route: '/api/v1/channels/{id}/email/gmail', path: '/api/v1/channels/11111111-1111-4111-8111-111111111111/email/gmail' };
  renderWithConfig(<RequestCard item={gmailForChannel} timezone="UTC" {...actions()} />, cloud);
  expect(await screen.findByText(/including the Limited Use requirements/)).toBeInTheDocument();
});

test('turning a coding agent on has no Approve on the card: it is in the request’s detail, after the consent', () => {
  render(<RequestCard item={agentRequest({ method: 'PUT', route: '/api/v1/autofix/connectors/{kind}', path: '/api/v1/autofix/connectors/claude_issue', body: { consent_version: 'v1' } })}
    timezone="UTC" {...actions()} />);
  expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Reject' })).toBeInTheDocument();
});
