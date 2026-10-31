import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Conversation } from '../../api/schemas';
import { DetailsPanel } from './details-panel';

const active = { id: '22222222-2222-4222-8222-222222222222', email: 'ann@example.com', display_name: 'Ann', role: 'agent' as const, timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0 };
const gone = { ...active, id: '33333333-3333-4333-8333-333333333333', email: 'cy@example.com', display_name: 'Cy', deactivated_at: '2026-10-10T00:00:00Z' };
// The signed-in agent.
const tom = { ...active, id: '66666666-6666-4666-8666-666666666666', email: 'tom@example.com', display_name: 'Tom' };
const conversation = (assignee_id: string | null) => ({ latest_reply_number: 0,
  id: '44444444-4444-4444-8444-444444444444', contact_id: '55555555-5555-4555-8555-555555555555', contact_name: 'Mai Linh', subject: 'Help', status: 'resolved', priority: 'normal',
  assignee_id, channel: 'email', origin: null, created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z', tags: [],
}) as Conversation;

const options = (name: string) => within(screen.getByRole('combobox', { name })).getAllByRole('option').map((option) => option.textContent);

test('the assignee picker offers active agents only', () => {
  render(<DetailsPanel conversation={conversation(null)} agents={[active, gone]} tags={[]} />);
  expect(options('Assignee')).toEqual(['Ann', 'Unassigned']);
});

test('the assignee picker lists Me first, then teammates, then Unassigned', () => {
  const onUpdate = vi.fn();
  render(<DetailsPanel conversation={conversation(null)} agents={[active, tom]} currentAgentId={tom.id} tags={[]} onUpdate={onUpdate} />);
  expect(options('Assignee')).toEqual(['Me', 'Ann', 'Unassigned']);
  fireEvent.change(screen.getByRole('combobox', { name: 'Assignee' }), { target: { value: tom.id } });
  expect(onUpdate).toHaveBeenCalledWith('44444444-4444-4444-8444-444444444444', { assignee_id: tom.id });
});

test('priority runs from Urgent to Low, in the order of the Filter menu', () => {
  render(<DetailsPanel conversation={conversation(null)} agents={[active]} tags={[]} />);
  expect(options('Priority')).toEqual(['Urgent', 'High', 'Normal', 'Low']);
});

test('a conversation kept by a deactivated agent still names them', () => {
  render(<DetailsPanel conversation={conversation(gone.id)} agents={[active, gone]} tags={[]} />);
  const picker = screen.getByRole('combobox', { name: 'Assignee' });
  expect(picker).toHaveValue(gone.id);
  expect(within(picker).getByRole('option', { name: 'Cy (deactivated)' })).toBeInTheDocument();
});

test('the details narrow the list to the conversation\'s customer', () => {
  const onShow = vi.fn();
  render(<DetailsPanel conversation={conversation(null)} agents={[active]} tags={[]} onShowContactConversations={onShow} />);
  fireEvent.click(screen.getByRole('button', { name: 'All conversations of Mai Linh' }));
  expect(onShow).toHaveBeenCalledWith('55555555-5555-4555-8555-555555555555');
});

test('targets list each clock in full, read only, with the business time left or late; an admin can edit them', () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  vi.setSystemTime(new Date('2026-11-02T05:30:00Z'));
  try {
    const clock = { state: 'running', due_at: null, warn_at: null, met_at: null, breached_at: null, due_soon: false, overdue: false, business_minutes_left: null };
    const sla = { state: 'overdue', first_response: { ...clock, due_at: '2026-11-02T04:10:00Z', breached_at: '2026-11-02T04:10:00Z', overdue: true, business_minutes_left: -80 }, resolution: null };
    const withTargets = { ...conversation(null), sla } as unknown as Conversation;
    render(<DetailsPanel conversation={withTargets} agents={[]} tags={[]} timezone="UTC" canManage />);
    const targets = screen.getByRole('region', { name: 'Targets' });
    expect(targets).toHaveTextContent('First reply');
    expect(targets).toHaveTextContent('Overdue since 4:10 AM');
    expect(targets).toHaveTextContent('1 h 20 min of business time late');
    expect(targets).toHaveTextContent('No target');
    expect(within(targets).getByRole('link', { name: 'Edit targets' })).toHaveAttribute('href', '/settings/targets');
  } finally {
    vi.useRealTimers();
  }
});

test('a conversation without targets shows no Targets section', () => {
  render(<DetailsPanel conversation={{ ...conversation(null), sla: null } as unknown as Conversation} agents={[]} tags={[]} timezone="UTC" />);
  expect(screen.queryByRole('region', { name: 'Targets' })).toBeNull();
});
