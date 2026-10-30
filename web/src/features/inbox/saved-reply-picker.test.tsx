import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { SavedReply } from '../../api/schemas';
import { SavedReplyPicker } from './saved-reply-picker';

const reply = (id: string, title: string, body = `${title} text`): SavedReply => ({
  id, title, body, created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z', last_change: null,
});
const replies = [reply('1', 'Chào hỏi'), reply('2', 'Refund steps'), reply('3', 'Shipping delay')];

test('opens on the search box and lists every reply by title', () => {
  render(<SavedReplyPicker replies={replies} onPick={vi.fn()} onClose={vi.fn()} />);
  expect(screen.getByRole('dialog', { name: 'Saved replies' })).toBeInTheDocument();
  expect(screen.getByRole('combobox', { name: 'Search saved replies' })).toHaveFocus();
  expect(within(screen.getByRole('listbox')).getAllByRole('option').map((option) => option.textContent))
    .toEqual(['Chào hỏi', 'Refund steps', 'Shipping delay']);
});

test('typing filters by title ignoring case and accents, and says when nothing matches', () => {
  render(<SavedReplyPicker replies={replies} onPick={vi.fn()} onClose={vi.fn()} />);
  const search = screen.getByRole('combobox', { name: 'Search saved replies' });
  fireEvent.change(search, { target: { value: 'CHAO' } });
  expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual(['Chào hỏi']);
  fireEvent.change(search, { target: { value: 'ship' } });
  expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual(['Shipping delay']);
  fireEvent.change(search, { target: { value: 'zzz' } });
  expect(screen.queryAllByRole('option')).toHaveLength(0);
  expect(screen.getByText('No saved reply matches.')).toBeInTheDocument();
});

test('Enter picks the first match, arrows move the choice, all from the keyboard', () => {
  const onPick = vi.fn();
  render(<SavedReplyPicker replies={replies} onPick={onPick} onClose={vi.fn()} />);
  const search = screen.getByRole('combobox', { name: 'Search saved replies' });
  expect(screen.getByRole('option', { name: 'Chào hỏi', selected: true })).toBeInTheDocument();
  fireEvent.keyDown(search, { key: 'ArrowDown' });
  fireEvent.keyDown(search, { key: 'ArrowDown' });
  expect(screen.getByRole('option', { name: 'Shipping delay', selected: true })).toBeInTheDocument();
  expect(search).toHaveAttribute('aria-activedescendant', screen.getByRole('option', { name: 'Shipping delay' }).id);
  fireEvent.keyDown(search, { key: 'ArrowDown' });
  expect(screen.getByRole('option', { name: 'Shipping delay', selected: true })).toBeInTheDocument();
  fireEvent.keyDown(search, { key: 'ArrowUp' });
  fireEvent.keyDown(search, { key: 'Enter' });
  expect(onPick).toHaveBeenCalledWith(replies[1]);
});

test('typing then Enter picks the first match without touching the mouse', () => {
  const onPick = vi.fn();
  render(<SavedReplyPicker replies={replies} onPick={onPick} onClose={vi.fn()} />);
  const search = screen.getByRole('combobox', { name: 'Search saved replies' });
  fireEvent.change(search, { target: { value: 'refund' } });
  fireEvent.keyDown(search, { key: 'Enter' });
  expect(onPick).toHaveBeenCalledExactlyOnceWith(replies[1]);
});

test('Enter with nothing matching picks nothing; a click picks that reply', () => {
  const onPick = vi.fn();
  render(<SavedReplyPicker replies={replies} onPick={onPick} onClose={vi.fn()} />);
  const search = screen.getByRole('combobox', { name: 'Search saved replies' });
  fireEvent.change(search, { target: { value: 'zzz' } });
  fireEvent.keyDown(search, { key: 'Enter' });
  expect(onPick).not.toHaveBeenCalled();
  fireEvent.change(search, { target: { value: '' } });
  fireEvent.click(screen.getByRole('option', { name: 'Refund steps' }));
  expect(onPick).toHaveBeenCalledExactlyOnceWith(replies[1]);
});

test('Escape closes it, and Enter does nothing while an input method is composing', () => {
  const onClose = vi.fn();
  const onPick = vi.fn();
  render(<SavedReplyPicker replies={replies} onPick={onPick} onClose={onClose} />);
  const search = screen.getByRole('combobox', { name: 'Search saved replies' });
  fireEvent.keyDown(search, { key: 'Enter', isComposing: true });
  expect(onPick).not.toHaveBeenCalled();
  fireEvent.keyDown(search, { key: 'Escape' });
  expect(onClose).toHaveBeenCalledOnce();
});

test('with no saved replies it says so and leads to the place that adds them, without a search box', () => {
  const onManage = vi.fn();
  const onClose = vi.fn();
  render(<SavedReplyPicker replies={[]} onPick={vi.fn()} onClose={onClose} onManage={onManage} />);
  expect(screen.getByText('No saved replies yet.')).toBeInTheDocument();
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  const manage = screen.getByRole('button', { name: 'Manage saved replies' });
  expect(manage).toHaveFocus();
  fireEvent.keyDown(manage, { key: 'Escape' });
  expect(onClose).toHaveBeenCalledExactlyOnceWith(true);
  fireEvent.click(manage);
  expect(onManage).toHaveBeenCalledOnce();
});

test('with no saved replies and no way to add them, the list itself has the focus', () => {
  render(<SavedReplyPicker replies={[]} onPick={vi.fn()} onClose={vi.fn()} />);
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  expect(screen.getByRole('dialog', { name: 'Saved replies' })).toHaveFocus();
});

const texts = { label: 'Articles', search: 'Search articles', loading: 'Loading articles…', failed: 'Articles could not be loaded.', none: 'No published article yet.', noMatch: 'No published article matches.', manage: 'Open settings' };

test('a source that searches by itself gets every search and its answer is shown as it is, under its own names', () => {
  const onQueryChange = vi.fn();
  const { rerender } = render(<SavedReplyPicker replies={replies} texts={texts} onQueryChange={onQueryChange} onPick={vi.fn()} onClose={vi.fn()} />);
  expect(screen.getByRole('dialog', { name: 'Articles' })).toBeInTheDocument();
  const search = screen.getByRole('combobox', { name: 'Search articles' });
  fireEvent.change(search, { target: { value: 'zzz' } });
  expect(onQueryChange).toHaveBeenLastCalledWith('zzz');
  // The source has not answered yet: its last list stays, unfiltered.
  expect(screen.getAllByRole('option')).toHaveLength(3);
  rerender(<SavedReplyPicker replies={[]} texts={texts} onQueryChange={onQueryChange} onPick={vi.fn()} onClose={vi.fn()} />);
  expect(screen.getByRole('status')).toHaveTextContent('No published article matches.');
  fireEvent.change(search, { target: { value: '' } });
  expect(screen.getByRole('status')).toHaveTextContent('No published article yet.');
});

test('the way to add some carries the name the source gives it', () => {
  const onManage = vi.fn();
  render(<SavedReplyPicker replies={[]} texts={texts} onManage={onManage} onPick={vi.fn()} onClose={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Open settings' }));
  expect(onManage).toHaveBeenCalledOnce();
});

test('it comes into view when it opens, below the fold on a phone', () => {
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  try {
    render(<SavedReplyPicker replies={replies} onPick={vi.fn()} onClose={vi.fn()} />);
    expect(scrolled).toHaveBeenCalledExactlyOnceWith({ block: 'nearest' });
    expect(scrolled.mock.contexts[0]).toBe(screen.getByRole('dialog', { name: 'Saved replies' }));
  } finally {
    delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView;
  }
});
