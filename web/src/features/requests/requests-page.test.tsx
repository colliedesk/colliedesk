import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { agentRequest } from './request.test.fixture';
import { RequestsPage } from './requests-page';

const handlers = () => ({ onApprove: vi.fn(), onReject: vi.fn(), onFilter: vi.fn() });

test('lists waiting requests, switches to all, and explains an empty list', () => {
  const actions = handlers();
  const { rerender } = render(<RequestsPage items={[agentRequest()]} filter="pending" timezone="UTC" {...actions} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Requests' })).toBeInTheDocument();
  expect(screen.getByRole('article', { name: 'Change a conversation: status pending' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('radio', { name: 'All' }));
  expect(actions.onFilter).toHaveBeenCalledWith('all');
  rerender(<RequestsPage items={[]} filter="pending" timezone="UTC" {...actions} />);
  expect(screen.getByText(/Nothing is waiting/)).toBeInTheDocument();
});

test('an approval made on this page stays first with its answer after the list moved on', () => {
  const approved = agentRequest({ id: '99999999-9999-4999-8999-999999999999', status: 'approved', summary: 'Create an agent token: label nightly, scope read',
    decided_by: { agent_id: null, name: 'Ada Admin' }, result_status: 201 });
  render(<RequestsPage items={[agentRequest()]} approved={[{ request: approved, result: { token: 'cdk_agent_secret' } }]} filter="pending" timezone="UTC" {...handlers()} />);
  const cards = screen.getAllByRole('article');
  expect(cards[0]).toHaveAccessibleName('Create an agent token: label nightly, scope read');
  expect(screen.getByText(/cdk_agent_secret/)).toBeInTheDocument();
  expect(cards).toHaveLength(2);
});
