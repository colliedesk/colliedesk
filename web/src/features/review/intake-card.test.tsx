import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { expect, test, vi } from 'vitest';
import { AGENT, CONVERSATION, OTHER_CONVERSATION, candidate, detail, gathering } from './intake.test.fixture';
import { IntakeCard } from './intake-card';

const props = (overrides: Partial<ComponentProps<typeof IntakeCard>> = {}): ComponentProps<typeof IntakeCard> => ({
  item: gathering(), open: false, onToggle: vi.fn(), timezone: 'UTC', channelName: () => 'Zalo HC Labs',
  agentName: (id) => (id === AGENT ? 'Ann Agent' : 'a teammate'),
  onDecide: vi.fn().mockResolvedValue(undefined), onRequeue: vi.fn().mockResolvedValue(undefined), ...overrides,
});

test('a waiting card says who wrote, where, why it waits and how it begins', () => {
  render(<IntakeCard {...props()} />);
  const card = screen.getByRole('article', { name: 'Lan Nguyen' });
  expect(within(card).getByText('Group Customer A support · Zalo HC Labs')).toBeInTheDocument();
  expect(within(card).getByText('No command')).toBeInTheDocument();
  expect(within(card).getByText('The scale stopped printing labels')).toBeInTheDocument();
  expect(within(card).getByText('2 messages')).toBeInTheDocument();
});

test('Decide opens the card; Create sends the subject made from its first line and the picked command', async () => {
  const handlers = props({ item: gathering(), detail: detail({ items: [detail().items[0]] }), open: true });
  render(<IntakeCard {...handlers} />);
  expect(screen.getByText('The scale stopped printing labels', { selector: '.intake-item *' })).toBeInTheDocument();
  expect(screen.getByLabelText('Subject')).toHaveValue('The scale stopped printing labels');
  fireEvent.change(screen.getByLabelText('Command (optional)'), { target: { value: 'bug' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create conversation' }));
  expect(handlers.onDecide).toHaveBeenCalledWith({ action: 'create', subject: 'The scale stopped printing labels', command: 'bug' });
});

test('a subject leaves out a command the customer typed first', () => {
  render(<IntakeCard {...props({ open: true, detail: detail({ items: [{ ...detail().items[0], body: 'Bug The printer jams\nsecond line' }], commands: ['bug'] }) })} />);
  expect(screen.getByLabelText('Subject')).toHaveValue('The printer jams');
});

test('Add to a conversation sends the picked conversation; the rules target is picked first', () => {
  const handlers = props({ open: true, item: gathering({ rules: { action: 'attach', rule: 'thread', basis: 'default', target_conversation_id: OTHER_CONVERSATION } }),
    detail: detail({ candidates: [candidate({ conversation_id: OTHER_CONVERSATION, subject: 'Login fails' }), candidate()] }) });
  render(<IntakeCard {...handlers} />);
  expect(screen.getByRole('radio', { name: 'Add to a conversation' })).toBeChecked();
  expect(screen.getByRole('radio', { name: /Login fails/ })).toBeChecked();
  fireEvent.click(screen.getByRole('radio', { name: /Scale prints blank labels/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Add to conversation' }));
  expect(handlers.onDecide).toHaveBeenCalledWith({ action: 'attach', conversation_id: CONVERSATION });
});

test('with no conversation to join, Add explains and cannot be sent', () => {
  render(<IntakeCard {...props({ open: true, detail: detail({ candidates: [] }) })} />);
  fireEvent.click(screen.getByRole('radio', { name: 'Add to a conversation' }));
  expect(screen.getByText(/no open conversation here/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Add to conversation' })).toBeDisabled();
});

test('Not a request sends why', () => {
  const handlers = props({ open: true, detail: detail() });
  render(<IntakeCard {...handlers} />);
  fireEvent.click(screen.getByRole('radio', { name: 'Not a request' }));
  fireEvent.click(screen.getByRole('radio', { name: 'Answered in chat' }));
  fireEvent.click(screen.getByRole('button', { name: 'Set aside' }));
  expect(handlers.onDecide).toHaveBeenCalledWith({ action: 'ignore', ignore_reason: 'handled_in_chat' });
});

test('a call that was not saved says so and can be sent again', async () => {
  const onDecide = vi.fn().mockRejectedValueOnce(new Error('API 500')).mockResolvedValueOnce(undefined);
  render(<IntakeCard {...props({ open: true, detail: detail(), onDecide })} />);
  fireEvent.click(screen.getByRole('button', { name: 'Create conversation' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('It was not saved');
  await waitFor(() => expect(screen.getByRole('button', { name: 'Create conversation' })).toBeEnabled());
  fireEvent.click(screen.getByRole('button', { name: 'Create conversation' }));
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
  expect(onDecide).toHaveBeenCalledTimes(2);
});

test('a card holding a password says so, and one behind another of its lane says why it waits', () => {
  const { rerender } = render(<IntakeCard {...props({ item: gathering({ review_reason: 'credential' }) })} />);
  expect(screen.getByText(/Ask the customer to change it/)).toBeInTheDocument();
  rerender(<IntakeCard {...props({ item: gathering({ review_reason: 'lane_blocked' }) })} />);
  expect(screen.getByText('Waiting for the card above')).toBeInTheDocument();
  expect(screen.getByText(/Deciding that one first keeps their messages in order/)).toBeInTheDocument();
});

test('a card of messages taken back from a conversation says so, and by whom when the server knows', () => {
  const detached = { action: 'review' as const, rule: 'detached', basis: 'default' as const, target_conversation_id: null };
  const { rerender } = render(<IntakeCard {...props({ item: gathering({ review_reason: 'requeued', rules: detached, requeued_by_agent_id: AGENT }) })} />);
  expect(screen.getByText('Taken back from a conversation by Ann Agent')).toBeInTheDocument();
  rerender(<IntakeCard {...props({ item: gathering({ review_reason: 'requeued', rules: detached }) })} />);
  expect(screen.getByText('Taken back from a conversation')).toBeInTheDocument();
});

test('a card put back says by whom', () => {
  render(<IntakeCard {...props({ item: gathering({ review_reason: 'requeued', requeued_by_agent_id: AGENT, requeued_at: '2026-10-26T03:00:00Z' }) })} />);
  expect(screen.getByText('Put back by Ann Agent')).toBeInTheDocument();
});

test('a card AI set aside can be put back', async () => {
  const handlers = props({ item: gathering({ state: 'ignored', review_reason: null, decided_by: 'ai', ignore_reason: null, decided_at: '2026-10-26T02:03:00Z' }) });
  render(<IntakeCard {...handlers} />);
  expect(screen.getByText('Set aside')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Put back in queue' }));
  expect(handlers.onRequeue).toHaveBeenCalledTimes(1);
  // While the call runs, a second press does nothing.
  expect(screen.getByRole('button', { name: 'Put back in queue' })).toBeDisabled();
  await waitFor(() => expect(screen.getByRole('button', { name: 'Put back in queue' })).toBeEnabled());
});

test('a decided card says what happened, who decided and links its conversation', () => {
  const { rerender } = render(<IntakeCard {...props({ item: gathering({ state: 'attached', review_reason: null, decided_by: 'rules', conversation_id: CONVERSATION,
    rules: { action: 'attach', rule: 'quote', basis: 'match', target_conversation_id: CONVERSATION }, decided_at: '2026-10-26T02:03:00Z' }) })} />);
  expect(screen.getByText('Added to a conversation')).toBeInTheDocument();
  expect(screen.getByText('Rules · match')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Open the conversation' })).toHaveAttribute('href', `/conversations/${CONVERSATION}`);
  rerender(<IntakeCard {...props({ item: gathering({ state: 'created', review_reason: null, decided_by: 'rules', fallback: true, conversation_id: CONVERSATION, decided_at: '2026-10-26T02:03:00Z' }) })} />);
  expect(screen.getByText('Rules · default')).toBeInTheDocument();
  expect(screen.getByText('Fallback: AI did not decide in time')).toBeInTheDocument();
  rerender(<IntakeCard {...props({ item: gathering({ state: 'ignored', review_reason: null, decided_by: 'agent', decided_by_agent_id: AGENT, ignore_reason: 'duplicate', decided_at: '2026-10-26T02:03:00Z' }) })} />);
  expect(screen.getByText('Ann Agent')).toBeInTheDocument();
  expect(screen.getByText('Set aside: Already reported')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Put back in queue' })).toBeInTheDocument();
});

test('messages that could not be loaded say so and can be asked again', () => {
  const onRetryDetail = vi.fn();
  render(<IntakeCard {...props({ open: true, detailFailed: true, onRetryDetail })} />);
  expect(screen.getByRole('alert')).toHaveTextContent('could not be loaded');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(onRetryDetail).toHaveBeenCalled();
});

test('a chat message keeps the line breaks the customer typed, however long the line', () => {
  const body = 'The scale in the second warehouse stopped printing labels this morning\nwe restarted it';
  render(<IntakeCard {...props({ open: true, detail: detail({ items: [{ ...detail().items[0], body }] }) })} />);
  expect(document.querySelector('.intake-item-body')?.textContent).toBe(body);
});
