import { fireEvent, render as renderPlain, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { expect, test, vi } from 'vitest';
import type { Macro } from '../../api/schemas';
import { MacrosSection } from './macros-section';
import { SettingsEditsProvider } from './settings-edits';

const render = (ui: ReactElement) => renderPlain(ui, { wrapper: SettingsEditsProvider });
const pressSave = () => fireEvent.click(screen.getByRole('button', { name: 'Save' }));

const macro = (id: string, title: string, scope: 'shared' | 'personal' = 'shared'): Macro => ({
  id, title, body: 'Hi {{customer.name}}', scope, owner_agent_id: scope === 'personal' ? 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' : null,
  actions: { status: 'pending' }, created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', deleted_at: null, purges_at: null,
});

test('lists a macro with its scope and the status it sets', () => {
  render(<MacrosSection macros={[macro('1', 'Refund')]} trash={[]} people={[]} tags={[]} onCreate={vi.fn()} onUpdate={vi.fn()} onDelete={vi.fn()} onRestore={vi.fn()} />);
  expect(screen.getByRole('heading', { name: 'Macros' })).toBeInTheDocument();
  expect(screen.getByText('Refund')).toBeInTheDocument();
  expect(screen.getByText('Shared')).toBeInTheDocument();
  expect(screen.getByText('pending')).toBeInTheDocument();
});

test('saving a new macro sends its title, text and status', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  render(<MacrosSection macros={[]} trash={[]} people={[]} tags={[]} onCreate={onCreate} onUpdate={vi.fn()} onDelete={vi.fn()} onRestore={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'New macro' }));
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Close it' } });
  fireEvent.change(screen.getByLabelText('Reply text'), { target: { value: 'Done, {{ticket.id}}' } });
  fireEvent.change(screen.getByLabelText('Set status'), { target: { value: 'resolved' } });
  pressSave();
  await vi.waitFor(() => expect(onCreate).toHaveBeenCalledWith({
    title: 'Close it', body: 'Done, {{ticket.id}}', scope: 'shared', actions: { status: 'resolved' },
  }));
});
