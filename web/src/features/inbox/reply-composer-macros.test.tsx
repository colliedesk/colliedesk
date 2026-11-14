import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, expect, test, vi } from 'vitest';
import type { Macro } from '../../api/schemas';
import { ReplyComposer, replyPickersPart } from './reply-composer';

beforeAll(async () => {
  replyPickersPart.preload();
  await import('./reply-pickers');
  await new Promise((resolve) => setTimeout(resolve, 0));
});

const refund: Macro = {
  id: '11111111-1111-4111-8111-111111111111', title: 'Refund', body: 'Hi {{customer.name}}', scope: 'shared', owner_agent_id: null,
  actions: { status: 'pending' }, created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', deleted_at: null, purges_at: null,
};

test('applying a macro inserts the filled reply and offers Undo', async () => {
  const onApplyMacro = vi.fn().mockResolvedValue({ id: '22222222-2222-4222-8222-222222222222', body: 'Hi Mai', title: 'Refund' });
  const onUndoMacro = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer macros={[refund]} onApplyMacro={onApplyMacro} onUndoMacro={onUndoMacro} />);
  fireEvent.click(screen.getByRole('button', { name: 'Apply macro' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Refund' }));
  expect(onApplyMacro).toHaveBeenCalledWith(refund);
  expect(await screen.findByLabelText('Message body')).toHaveValue('Hi Mai');
  expect(screen.getByRole('status')).toHaveTextContent('Applied Refund');
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  await vi.waitFor(() => expect(onUndoMacro).toHaveBeenCalledWith('22222222-2222-4222-8222-222222222222'));
  await vi.waitFor(() => expect(screen.getByLabelText('Message body')).toHaveValue(''));
});
