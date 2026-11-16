import { fireEvent, render as renderPlain, screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { expect, test, vi } from 'vitest';
import { ConflictError } from '../../api/client';
import type { Tag } from '../../api/schemas';
import { SettingsEditsProvider } from './settings-edits';
import { TagsSection } from './tags-section';

// A rename is typed text: the page's Save bar, which the provider draws, saves it.
const render = (ui: ReactElement) => renderPlain(ui, { wrapper: SettingsEditsProvider });

const tag = (id: string, name: string, count: number): Tag => ({ id, name, color: '#64748b', conversation_count: count });
const tags = [tag('t1', 'billing', 128), tag('t2', 'shipping-delay', 12), tag('t3', 'vip', 1), tag('t4', 'old-promo', 0)];
const handlers = () => ({ onRename: vi.fn().mockResolvedValue(undefined), onDelete: vi.fn().mockResolvedValue(undefined) });
const rowOf = (name: string) => screen.getByText(name, { selector: 'strong' }).closest('li') as HTMLElement;

test('lists every tag with how many conversations carry it', () => {
  render(<TagsSection tags={tags} canRename canDelete {...handlers()} />);
  expect(within(rowOf('billing')).getByText('128 conversations')).toBeInTheDocument();
  expect(within(rowOf('vip')).getByText('1 conversation')).toBeInTheDocument();
  expect(within(rowOf('old-promo')).getByText('No conversations')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Rename billing' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Delete billing' })).toBeInTheDocument();
});

test('says where tags come from when there are none', () => {
  render(<TagsSection tags={[]} canRename canDelete {...handlers()} />);
  expect(screen.getByText('No tags yet. Add one from the details of a conversation.')).toBeInTheDocument();
});

test('a person whose role cannot rename or delete tags sees the list without those actions', () => {
  render(<TagsSection tags={tags} canRename={false} canDelete={false} {...handlers()} />);
  expect(screen.getByText('128 conversations')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /^Rename / })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /^Delete / })).not.toBeInTheDocument();
});

test('renames a tag in its row and saves it with the Save bar', async () => {
  const actions = handlers();
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename billing' }));
  const field = screen.getByRole('textbox', { name: 'New name for billing' });
  expect(field).toHaveFocus();
  expect(field).toHaveValue('billing');
  fireEvent.change(field, { target: { value: 'invoices' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(actions.onRename).toHaveBeenCalledWith('t1', 'invoices'));
  await waitFor(() => expect(screen.queryByRole('textbox')).not.toBeInTheDocument());
  expect(screen.getByRole('status')).toHaveTextContent('Renamed “billing” to “invoices”.');
});

test('Enter in the field saves the new name too', async () => {
  const actions = handlers();
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename vip' }));
  const field = screen.getByRole('textbox', { name: 'New name for vip' });
  fireEvent.change(field, { target: { value: 'VIP' } });
  fireEvent.submit(field);
  await waitFor(() => expect(actions.onRename).toHaveBeenCalledWith('t3', 'VIP'));
});

test('Cancel and Escape keep the old name and send nothing', () => {
  const actions = handlers();
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename billing' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'New name for billing' }), { target: { value: 'x' } });
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Rename billing' })).toHaveFocus();
  fireEvent.click(screen.getByRole('button', { name: 'Rename billing' }));
  fireEvent.keyDown(screen.getByRole('textbox', { name: 'New name for billing' }), { key: 'Escape' });
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  expect(actions.onRename).not.toHaveBeenCalled();
});

test('a name another tag has is refused with the server reason and the field stays open', async () => {
  const actions = handlers();
  actions.onRename.mockRejectedValue(new ConflictError('Another tag is already named "billing". Pick another name.', { code: 'tag_name_taken' }));
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename vip' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'New name for vip' }), { target: { value: 'billing' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Another tag is already named "billing". Pick another name.');
  expect(screen.getByRole('textbox', { name: 'New name for vip' })).toHaveValue('billing');
});

test('an empty name is not sent', async () => {
  const actions = handlers();
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename vip' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'New name for vip' }), { target: { value: '  ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('A tag needs a name.');
  expect(actions.onRename).not.toHaveBeenCalled();
});

test('delete asks in the row, naming how many conversations lose the tag, then deletes it', async () => {
  const actions = handlers();
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete shipping-delay' }));
  const ask = screen.getByRole('group', { name: 'Delete shipping-delay' });
  expect(ask).toHaveTextContent('Delete “shipping-delay”? It comes off 12 conversations. This cannot be undone.');
  expect(actions.onDelete).not.toHaveBeenCalled();
  fireEvent.click(within(ask).getByRole('button', { name: 'Delete tag' }));
  await waitFor(() => expect(actions.onDelete).toHaveBeenCalledWith('t2'));
  expect(await screen.findByRole('status')).toHaveTextContent('Deleted “shipping-delay”.');
});

test('deleting a tag no conversation carries says so, and Cancel keeps it', () => {
  const actions = handlers();
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete old-promo' }));
  const ask = screen.getByRole('group', { name: 'Delete old-promo' });
  expect(ask).toHaveTextContent('Delete “old-promo”? No conversation has it. This cannot be undone.');
  fireEvent.click(within(ask).getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('group', { name: 'Delete old-promo' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Delete old-promo' })).toHaveFocus();
  expect(actions.onDelete).not.toHaveBeenCalled();
});

test('a rename of a tag someone deleted meanwhile closes the field and says so', async () => {
  const actions = handlers();
  actions.onRename.mockRejectedValue(new Error('API 404'));
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename vip' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'New name for vip' }), { target: { value: 'VIP' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('“vip” was deleted by someone else.');
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
});

test('deleting a tag someone deleted meanwhile counts as done', async () => {
  const actions = handlers();
  actions.onDelete.mockRejectedValue(new Error('API 404'));
  render(<TagsSection tags={tags} canRename canDelete {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete vip' }));
  fireEvent.click(within(screen.getByRole('group', { name: 'Delete vip' })).getByRole('button', { name: 'Delete tag' }));
  expect(await screen.findByText('“vip” was already deleted.')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.queryByRole('group', { name: 'Delete vip' })).not.toBeInTheDocument();
});

test('after a delete the focus goes to the line that says what happened', async () => {
  render(<TagsSection tags={tags} canRename canDelete {...handlers()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete vip' }));
  fireEvent.click(within(screen.getByRole('group', { name: 'Delete vip' })).getByRole('button', { name: 'Delete tag' }));
  await waitFor(() => expect(screen.getByRole('status')).toHaveFocus());
});

test('spaces around the same name are no change to save', () => {
  render(<TagsSection tags={tags} canRename canDelete {...handlers()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename vip' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'New name for vip' }), { target: { value: 'vip ' } });
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
});

test('a tag that leaves the list while it is being renamed takes its field and its unsaved change with it', () => {
  const view = render(<TagsSection tags={tags} canRename canDelete {...handlers()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename vip' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'New name for vip' }), { target: { value: 'VIP' } });
  expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
  view.rerender(<TagsSection tags={tags.filter((item) => item.name !== 'vip')} canRename canDelete {...handlers()} />);
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
});
