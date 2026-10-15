import { render, screen, within } from '@testing-library/react';
import { expect, test } from 'vitest';
import type { Conversation } from '../../api/schemas';
import { DetailsPanel } from './details-panel';

const active = { id: '22222222-2222-4222-8222-222222222222', email: 'ann@example.com', display_name: 'Ann', role: 'agent' as const, timezone_override: null, deactivated_at: null };
const gone = { ...active, id: '33333333-3333-4333-8333-333333333333', email: 'cy@example.com', display_name: 'Cy', deactivated_at: '2026-10-10T00:00:00Z' };
const conversation = (assignee_id: string | null) => ({
  id: '44444444-4444-4444-8444-444444444444', contact_id: '55555555-5555-4555-8555-555555555555', subject: 'Help', status: 'resolved', priority: 'normal',
  assignee_id, origin: null, created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z', tags: [],
}) as Conversation;

test('the assignee picker offers active agents only', () => {
  render(<DetailsPanel conversation={conversation(null)} agents={[active, gone]} tags={[]} />);
  const picker = screen.getByRole('combobox', { name: 'Assignee' });
  expect(within(picker).getAllByRole('option').map((option) => option.textContent)).toEqual(['Unassigned', 'Ann']);
});

test('a conversation kept by a deactivated agent still names them', () => {
  render(<DetailsPanel conversation={conversation(gone.id)} agents={[active, gone]} tags={[]} />);
  const picker = screen.getByRole('combobox', { name: 'Assignee' });
  expect(picker).toHaveValue(gone.id);
  expect(within(picker).getByRole('option', { name: 'Cy (deactivated)' })).toBeInTheDocument();
});
