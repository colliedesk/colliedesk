import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { AIDraft } from './ai-draft-api';
import { AIDraftCard } from './ai-draft-card';

const ready: AIDraft = {
  id: '11111111-1111-4111-8111-111111111111', conversation_id: '22222222-2222-4222-8222-222222222222', message_id: null, status: 'ready', action: 'answer',
  body: 'Annual plans are refundable within 30 days.', confidence: 'high', confidence_reasons: [], handoff_reason: null,
  topic_article_id: '33333333-3333-4333-8333-333333333333',
  sources: [{ kind: 'kb_article', id: '33333333-3333-4333-8333-333333333333', number: 12, title: 'Refund policy' }],
  triage: { tags: ['billing'], priority: 'high', reason: 'Refund question' }, triage_applied_at: null, outcome: null, similarity: null,
  auto_send_at: null, error_code: null, model: 'claude-sonnet-5-5', created_at: '2026-10-22T10:00:00Z', ready_at: '2026-10-22T10:00:04Z',
};

const handlers = () => ({ onSend: vi.fn().mockResolvedValue(undefined), onDiscard: vi.fn().mockResolvedValue(undefined), onApplyTriage: vi.fn().mockResolvedValue(undefined),
  onCancelAutoAnswer: vi.fn().mockResolvedValue(undefined), onRegenerate: vi.fn().mockResolvedValue(undefined) });

test('a ready draft shows its text, why it is trusted and its sources, and Send sends it as written', () => {
  const on = handlers();
  render(<AIDraftCard draft={ready} timezone="UTC" {...on} />);
  expect(screen.getByRole('region', { name: 'AI draft' })).toBeInTheDocument();
  expect(screen.getByText('Annual plans are refundable within 30 days.')).toBeInTheDocument();
  expect(screen.getByText('From your knowledge base')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '#12 Refund policy' })).toHaveAttribute('href', '/knowledge/articles/33333333-3333-4333-8333-333333333333');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(on.onSend).toHaveBeenCalledWith('Annual plans are refundable within 30 days.');
});

test('Edit lets the person change the text before sending it', () => {
  const on = handlers();
  render(<AIDraftCard draft={ready} timezone="UTC" {...on} />);
  fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
  fireEvent.change(screen.getByLabelText('AI draft text'), { target: { value: 'Yes, within 30 days.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(on.onSend).toHaveBeenCalledWith('Yes, within 30 days.');
});

test('the triage suggestion is applied with one click and then says so', async () => {
  const on = handlers();
  const { rerender } = render(<AIDraftCard draft={ready} timezone="UTC" {...on} />);
  expect(screen.getByText(/Suggested: billing · priority high/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Apply: tag billing, priority high' }));
  await waitFor(() => expect(on.onApplyTriage).toHaveBeenCalled());
  rerender(<AIDraftCard draft={{ ...ready, triage_applied_at: '2026-10-22T10:01:00Z' }} timezone="UTC" {...on} />);
  expect(screen.getByText('Triage applied')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /^Apply/ })).not.toBeInTheDocument();
});

test('a draft that needs checking says why, and a handoff offers no reply to send', () => {
  const on = handlers();
  const { rerender } = render(<AIDraftCard draft={{ ...ready, confidence: 'medium', confidence_reasons: ['no_kb_source', 'risk:refund_or_billing_exception'] }} timezone="UTC" {...on} />);
  expect(screen.getByText('Check before sending')).toBeInTheDocument();
  expect(screen.getByText(/no knowledge base article backs it/)).toBeInTheDocument();
  expect(screen.getByText(/a refund or an exception/)).toBeInTheDocument();
  rerender(<AIDraftCard draft={{ ...ready, action: 'handoff', body: null, confidence: 'low', confidence_reasons: ['handoff'], handoff_reason: 'Needs the invoice history.' }} timezone="UTC" {...on} />);
  expect(screen.getByText('Needs the invoice history.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Send' })).not.toBeInTheDocument();
});

test('a scheduled answer says when it goes out and can be stopped', () => {
  const on = handlers();
  render(<AIDraftCard draft={{ ...ready, status: 'scheduled', auto_send_at: '2026-10-22T10:05:00Z' }} timezone="UTC" {...on} />);
  expect(screen.getByText(/Sends on its own at 10:05/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Stop' }));
  expect(on.onCancelAutoAnswer).toHaveBeenCalled();
});

test('a draft being written says so, and a failed one can be written again', () => {
  const on = handlers();
  const { rerender } = render(<AIDraftCard draft={{ ...ready, status: 'queued', body: null, confidence: null, action: null }} timezone="UTC" {...on} />);
  expect(screen.getByRole('status')).toHaveTextContent('Writing a draft from your knowledge base');
  rerender(<AIDraftCard draft={{ ...ready, status: 'failed', body: null, error_code: 'rate_limited' }} timezone="UTC" {...on} />);
  fireEvent.click(screen.getByRole('button', { name: 'Write a new draft' }));
  expect(on.onRegenerate).toHaveBeenCalled();
});

test('Discard throws the draft away; a failed action keeps the card with the reason', async () => {
  const on = handlers();
  on.onDiscard.mockRejectedValueOnce(new Error('offline'));
  render(<AIDraftCard draft={ready} timezone="UTC" {...on} />);
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('could not be done');
});
