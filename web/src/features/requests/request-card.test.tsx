import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InvalidRequestError } from '../../api/client';
import { agentRequest } from './request.test.fixture';
import { RequestCard } from './request-card';

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
