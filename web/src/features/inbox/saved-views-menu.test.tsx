import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { ConflictError } from '../../api/client';
import type { SavedView } from '../../api/schemas';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SavedViewsMenu as SavedViewsLoader, SavedViewsMenuView as SavedViewsMenu } from './saved-views-menu';

const views = vi.hoisted(() => ({ list: vi.fn(), trash: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), restore: vi.fn() }));
vi.mock('../../api/saved-views', () => ({ savedViewsApi: views }));

const me = '66666666-6666-4666-8666-666666666666';
const view = (id: string, name: string, scope: 'personal' | 'shared', filters: SavedView['filters'], deleted = false): SavedView => ({
  id, name, scope, filters, owner_agent_id: scope === 'personal' ? me : null, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
  deleted_at: deleted ? '2026-10-18T00:00:00Z' : null, purges_at: deleted ? '2026-11-17T00:00:00Z' : null,
});
const urgent = view('11111111-1111-4111-8111-111111111111', 'Urgent queue', 'shared', { status: ['open'], priority: ['urgent'] });
const mine = view('22222222-2222-4222-8222-222222222222', 'Waiting on me', 'personal', { assignee: ['me'], awaiting: 'team', sort: 'waiting_longest' });
const old = view('33333333-3333-4333-8333-333333333333', 'Old view', 'shared', {}, true);

// The actions menu of one view, opened.
const actionsOf = (name: string) => {
  const summary = screen.getByLabelText(`Actions for ${name}`);
  fireEvent.click(summary);
  return summary.closest('details')!;
};
const open = () => { fireEvent.click(screen.getByText('Views')); return screen.getByRole('group', { name: 'Views' }); };

test('views are listed shared first, then personal, and opening one sets its filters', () => {
  const onApply = vi.fn();
  render(<SavedViewsMenu views={[urgent, mine]} trash={[]} query={{}} timezone="UTC" onApply={onApply} />);
  const menu = open();
  expect(within(within(menu).getByRole('list', { name: 'Shared with the team' })).getByRole('button', { name: 'Urgent queue' })).toBeInTheDocument();
  fireEvent.click(within(within(menu).getByRole('list', { name: 'Only me' })).getByRole('button', { name: 'Waiting on me' }));
  expect(onApply).toHaveBeenCalledWith({ assignee: 'me', awaiting: 'team', sort: 'waiting_longest', status: 'all' });
});

test('while the views load the menu says so, not that there are none', () => {
  const { rerender } = render(<SavedViewsMenu views={[]} loading trash={[]} query={{}} timezone="UTC" />);
  expect(screen.getByText('Loading views…')).toBeInTheDocument();
  expect(screen.queryByText(/No saved views yet/)).toBeNull();
  rerender(<SavedViewsMenu views={[]} trash={[]} query={{}} timezone="UTC" />);
  expect(screen.getByText(/No saved views yet/)).toBeInTheDocument();
});

test('the view that matches the current filters is marked as current', () => {
  render(<SavedViewsMenu views={[urgent, mine]} trash={[]} query={{ priority: 'urgent' }} timezone="UTC" />);
  const menu = open();
  expect(within(menu).getByRole('button', { name: 'Urgent queue' })).toHaveAttribute('aria-current', 'true');
  expect(within(menu).getByRole('button', { name: 'Waiting on me' })).not.toHaveAttribute('aria-current');
});

test('the current filters are saved under a name, for me or for everyone', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  render(<SavedViewsMenu views={[]} trash={[]} query={{ priority: 'high,urgent', awaiting: 'team' }} timezone="UTC" onCreate={onCreate} />);
  const menu = open();
  expect(within(menu).getByText('No saved views yet. Set filters, then save them here.')).toBeInTheDocument();
  fireEvent.change(within(menu).getByLabelText('View name'), { target: { value: 'Hot' } });
  fireEvent.click(within(menu).getByRole('radio', { name: 'Everyone in the workspace' }));
  fireEvent.click(within(menu).getByRole('button', { name: 'Save view' }));
  await waitFor(() => expect(onCreate).toHaveBeenCalledWith({ name: 'Hot', scope: 'shared', filters: { status: ['open'], priority: ['high', 'urgent'], awaiting: 'team' } }));
  await waitFor(() => expect(within(menu).getByLabelText('View name')).toHaveValue(''));
});

test('a refused save says why and keeps the name', async () => {
  const onCreate = vi.fn().mockRejectedValue(new ConflictError('A view with this name already exists. Pick another name.'));
  render(<SavedViewsMenu views={[]} trash={[]} query={{}} timezone="UTC" onCreate={onCreate} />);
  const menu = open();
  fireEvent.change(within(menu).getByLabelText('View name'), { target: { value: 'Urgent queue' } });
  fireEvent.click(within(menu).getByRole('button', { name: 'Save view' }));
  expect(await within(menu).findByRole('alert')).toHaveTextContent('A view with this name already exists.');
  expect(within(menu).getByLabelText('View name')).toHaveValue('Urgent queue');
});

test('a view can be renamed, refilled with the current filters, shared or kept for me', async () => {
  const onUpdate = vi.fn().mockResolvedValue(undefined);
  render(<SavedViewsMenu views={[urgent, mine]} trash={[]} query={{ status: 'pending' }} timezone="UTC" onUpdate={onUpdate} />);
  open();
  const first = actionsOf('Urgent queue');
  fireEvent.click(within(first).getByRole('button', { name: 'Replace with current filters' }));
  await waitFor(() => expect(onUpdate).toHaveBeenLastCalledWith(urgent, { filters: { status: ['pending'] } }));
  const second = actionsOf('Waiting on me');
  fireEvent.click(within(second).getByRole('button', { name: 'Share with the team' }));
  await waitFor(() => expect(onUpdate).toHaveBeenLastCalledWith(mine, { scope: 'shared' }));
  fireEvent.click(within(actionsOf('Waiting on me')).getByRole('button', { name: 'Rename' }));
  fireEvent.change(within(second).getByLabelText('New name for Waiting on me'), { target: { value: 'Mine' } });
  fireEvent.click(within(second).getByRole('button', { name: 'Save name' }));
  await waitFor(() => expect(onUpdate).toHaveBeenLastCalledWith(mine, { name: 'Mine' }));
});

test('a delete goes to the trash with an Undo, and the trash restores', async () => {
  const onDelete = vi.fn().mockResolvedValue(undefined);
  const onRestore = vi.fn().mockResolvedValue(undefined);
  render(<SavedViewsMenu views={[urgent]} trash={[old]} query={{}} timezone="UTC" onDelete={onDelete} onRestore={onRestore} />);
  const menu = open();
  fireEvent.click(within(actionsOf('Urgent queue')).getByRole('button', { name: 'Delete' }));
  await waitFor(() => expect(onDelete).toHaveBeenCalledWith(urgent));
  const notice = await screen.findByRole('status');
  expect(notice).toHaveTextContent('View “Urgent queue” deleted.');
  fireEvent.click(within(notice).getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(onRestore).toHaveBeenCalledWith(urgent));

  fireEvent.click(within(menu).getByText('Recently deleted (1)'));
  expect(within(menu).getByText(/Gone for good Nov 17, 2026/)).toBeInTheDocument();
  fireEvent.click(within(menu).getByRole('button', { name: 'Restore Old view' }));
  await waitFor(() => expect(onRestore).toHaveBeenLastCalledWith(old));
});

test('a restore the server refuses says why, next to the trash', async () => {
  const onRestore = vi.fn().mockRejectedValue(new ConflictError('A view with this name already exists. Rename that one first.'));
  render(<SavedViewsMenu views={[]} trash={[old]} query={{}} timezone="UTC" onRestore={onRestore} />);
  const menu = open();
  fireEvent.click(within(menu).getByText('Recently deleted (1)'));
  fireEvent.click(within(menu).getByRole('button', { name: 'Restore Old view' }));
  const trash = within(menu).getByText('Recently deleted (1)').closest('details')!;
  expect(await within(trash).findByRole('alert')).toHaveTextContent('A view with this name already exists. Rename that one first.');
});

test('views load when the menu opens and the trash when it is opened, never before; a change sends the version it was made on', async () => {
  views.list.mockResolvedValue({ items: [urgent] });
  views.trash.mockResolvedValue({ items: [old] });
  views.update.mockResolvedValue(urgent);
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><SavedViewsLoader query={{ status: 'pending' }} timezone="UTC" /></QueryClientProvider>);
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(views.list).not.toHaveBeenCalled();
  expect(views.trash).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText('Views'));
  await screen.findByRole('button', { name: 'Urgent queue' });
  expect(views.trash).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText('Recently deleted'));
  await screen.findByRole('button', { name: 'Restore Old view' });
  fireEvent.click(within(actionsOf('Urgent queue')).getByRole('button', { name: 'Replace with current filters' }));
  await waitFor(() => expect(views.update).toHaveBeenCalledWith(urgent.id, { filters: { status: ['pending'] }, updated_at: urgent.updated_at }));
});

test('the menu stays open while a view saves, so the new view shows up in it', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  render(<SavedViewsMenu views={[]} trash={[]} query={{}} timezone="UTC" onCreate={onCreate} />);
  const menu = open();
  const details = menu.closest('details')!;
  fireEvent.change(within(menu).getByLabelText('View name'), { target: { value: 'Hot' } });
  const save = within(menu).getByRole('button', { name: 'Save view' });
  fireEvent.click(save);
  // The disabled button drops focus to nowhere.
  fireEvent.blur(save, { relatedTarget: null });
  await waitFor(() => expect(onCreate).toHaveBeenCalled());
  expect(details.open).toBe(true);
});
