import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { ReplyComposer } from './reply-composer';

test('N starts a private note and R goes back to a reply, both focusing the message body', () => {
  render(<ReplyComposer />);
  const body = screen.getByLabelText('Message body');
  fireEvent.keyDown(document.body, { key: 'n' });
  expect(screen.getByRole('button', { name: 'Private note', pressed: true })).toBeInTheDocument();
  expect(body).toHaveFocus();
  body.blur();
  fireEvent.keyDown(document.body, { key: 'r' });
  expect(screen.getByRole('button', { name: 'Reply', pressed: true })).toBeInTheDocument();
  expect(body).toHaveFocus();
});

test('letters typed into a field are text, not shortcuts', () => {
  render(<><input aria-label="Search" /><ReplyComposer /></>);
  fireEvent.keyDown(screen.getByLabelText('Search'), { key: 'n' });
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'n' });
  expect(screen.getByRole('button', { name: 'Reply', pressed: true })).toBeInTheDocument();
});

test('shortcuts do nothing without a conversation', () => {
  render(<ReplyComposer disabled />);
  fireEvent.keyDown(document.body, { key: 'n' });
  expect(screen.getByRole('button', { name: 'Reply', pressed: true })).toBeInTheDocument();
});

test('Ctrl or Cmd + Enter sends', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'On it.' } });
  fireEvent.keyDown(body, { key: 'Enter', metaKey: true });
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'On it.');
  fireEvent.change(body, { target: { value: 'Second' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(onSubmit).toHaveBeenLastCalledWith('outgoing', 'Second');
  fireEvent.keyDown(body, { key: 'Enter' });
  expect(onSubmit).toHaveBeenCalledTimes(2);
});

test('Ctrl/Cmd + Enter does not send while an input method is composing text', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Cảm ơn' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true, isComposing: true });
  fireEvent.keyDown(body, { key: 'Enter', metaKey: true, keyCode: 229 });
  expect(onSubmit).not.toHaveBeenCalled();
});

test('R and N still work when a radio tab or checkbox has focus', () => {
  render(<><input type="radio" aria-label="Open" /><ReplyComposer /></>);
  fireEvent.keyDown(screen.getByRole('radio', { name: 'Open' }), { key: 'n' });
  expect(screen.getByRole('button', { name: 'Private note', pressed: true })).toBeInTheDocument();
  expect(screen.getByLabelText('Message body')).toHaveFocus();
});

test('a key that ends an input method composition is not a shortcut', () => {
  render(<ReplyComposer />);
  fireEvent.keyDown(document.body, { key: 'n', isComposing: true });
  expect(screen.getByRole('button', { name: 'Reply', pressed: true })).toBeInTheDocument();
});
