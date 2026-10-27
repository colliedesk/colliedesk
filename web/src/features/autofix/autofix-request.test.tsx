import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AgentRequest } from '../../api/schemas';
import { json, serve } from '../kb/kb.test.fixture';
import { AutofixRequestDetail } from './autofix-request';
import { conversationId, repoOf, settingsOf } from './autofix.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const request = (overrides: Partial<AgentRequest>): AgentRequest => ({
  id: '12121212-1212-4121-8121-121212121212', kind: 'action', status: 'pending', method: 'POST', route: '/api/v1/conversations/{id}/fixes',
  path: `/api/v1/conversations/${conversationId}/fixes`, body: null, edited_body: null, approvable_by: 'agent', summary: 'Send a fix', conversation_id: conversationId,
  saved_reply: null, requested_by: { token_id: null, token_label: 'Claude Desktop', token_scope: 'draft', token_permissions: [], token_revoked: false, agent_id: null, agent_name: 'Mai Tran' },
  decided_by: null, decided_at: null, reason: null, result_status: null, result: null, result_note: null, created_at: '2026-10-27T10:59:00Z', expires_at: '2026-11-03T10:59:00Z',
  ...overrides,
});

function show(item: AgentRequest, result?: unknown, onApprove?: () => void) {
  serve({ 'GET /api/v1/autofix/settings': () => json(200, settingsOf()) });
  render(<QueryClientProvider client={new QueryClient()}><AutofixRequestDetail item={item} result={result} onApprove={onApprove} /></QueryClientProvider>);
}

describe('AutofixRequestDetail', () => {
  it('says what a send asks for in words, with the customer’s words as untrusted, and that the approver becomes the sender', async () => {
    show(request({ body: { repo_id: repoOf().id, connector: 'claude_issue', title: 'CSV export stops', body: '<customer-message untrusted="true" from="Tomás">\nCall [phone removed]\n</customer-message>', client_key: 'k' } }));
    expect(await screen.findByText('northwind/web')).toBeInTheDocument();
    expect(screen.getByText('CSV export stops')).toBeInTheDocument();
    expect(screen.getByText('UNTRUSTED · customer’s words')).toBeInTheDocument();
    expect(screen.getByText(/You become the sender/)).toBeInTheDocument();
  });

  it('says what disconnecting GitHub does when an AI agent asks for it', async () => {
    show(request({ method: 'DELETE', route: '/api/v1/autofix/github/installations/{id}', path: '/api/v1/autofix/github/installations/44444444-4444-4444-8444-444444444444', summary: 'Disconnect GitHub' }));
    expect(await screen.findByText(/uninstalls its app from northwind and disconnects 1 repository/)).toBeInTheDocument();
    expect(screen.getByText(/Fixes in progress on them are cancelled/)).toBeInTheDocument();
  });

  it('does not say what approving would do once the request is decided', () => {
    show(request({ status: 'approved', body: { repo_id: repoOf().id, connector: 'claude_issue', title: 't', body: 'b', client_key: 'k' } }));
    expect(screen.queryByText(/You become the sender/)).not.toBeInTheDocument();
    show(request({ status: 'rejected', route: '/api/v1/autofix/github/installations', path: '/api/v1/autofix/github/installations' }));
    expect(screen.queryByText(/Approve opens GitHub to install/)).not.toBeInTheDocument();
  });

  it('says Cursor gets it at once and keeps it', () => {
    show(request({ body: { repo_id: repoOf().id, connector: 'cursor_api', title: 't', body: 'b', client_key: 'k' } }));
    expect(screen.getByText(/Approve sends it to Cursor at once/)).toBeInTheDocument();
  });

  it('takes the approver to GitHub once the approval answers where to go', () => {
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    show(request({ status: 'approved', body: { repo_id: repoOf().id, connector: 'claude_issue', title: 't', body: 'b', client_key: 'k' } }),
      { fix: {}, authorize_url: 'https://github.com/login/oauth/authorize?state=s' });
    expect(assign).toHaveBeenCalledWith('https://github.com/login/oauth/authorize?state=s');
  });

  it('says so, and stays, when the address to GitHub is not https', () => {
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    show(request({ status: 'approved', body: { repo_id: repoOf().id, connector: 'claude_issue', title: 't', body: 'b', client_key: 'k' } }),
      { fix: {}, authorize_url: 'http://github.com/login/oauth/authorize?state=s' });
    expect(assign).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toHaveTextContent('not secure');
  });

  it('records consent in words when turning an agent on', () => {
    show(request({ method: 'PUT', route: '/api/v1/autofix/connectors/{kind}', path: '/api/v1/autofix/connectors/cursor_api', body: { consent_version: 'v1' } }));
    expect(screen.getByText(/Cursor keeps them with no end date/)).toBeInTheDocument();
  });

  it('approves turning an agent on only after the person ticks the data consent', () => {
    const onApprove = vi.fn();
    show(request({ method: 'PUT', route: '/api/v1/autofix/connectors/{kind}', path: '/api/v1/autofix/connectors/claude_issue', body: { consent_version: 'v1' } }), undefined, onApprove);
    const approve = screen.getByRole('button', { name: 'Approve' });
    expect(approve).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox', { name: /go into a GitHub issue/ }));
    fireEvent.click(approve);
    expect(onApprove).toHaveBeenCalledTimes(1);
  });

  it('refuses to approve consent to a text that changed since the AI agent asked', async () => {
    show(request({ method: 'PUT', route: '/api/v1/autofix/connectors/{kind}', path: '/api/v1/autofix/connectors/claude_issue', body: { consent_version: 'v0' } }), undefined, vi.fn());
    expect(await screen.findByRole('alert')).toHaveTextContent('consent text changed');
    fireEvent.click(screen.getByRole('checkbox', { name: /go into a GitHub issue/ }));
    expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled();
  });
});
