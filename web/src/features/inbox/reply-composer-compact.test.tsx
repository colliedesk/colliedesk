import { act, fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, expect, test } from 'vitest';
import { ReplyComposer, replyPickersPart } from './reply-composer';

// On a phone the reply box stays one line until the agent uses it, so the conversation keeps the screen
// (layout.css styles data-compact only below 44 rem). The state is the same at every width; these tests
// read it from the card.
beforeAll(async () => {
  replyPickersPart.preload();
  await import('./reply-pickers');
  await new Promise((resolve) => setTimeout(resolve, 0));
});

const card = () => document.querySelector('.composer-card')!;

test('an untouched reply box is compact, opens when the agent goes into it, and closes again when they press elsewhere with nothing written', () => {
  render(<><p>Timeline</p><ReplyComposer /></>);
  expect(card()).toHaveAttribute('data-compact', 'true');
  // Compact, it still offers the box and Send reply, so the next step is on screen.
  expect(screen.getByLabelText('Message body')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Send reply' })).toBeInTheDocument();
  act(() => screen.getByLabelText('Message body').focus());
  expect(card()).toHaveAttribute('data-compact', 'false');
  fireEvent.pointerDown(screen.getByText('Timeline'));
  expect(card()).toHaveAttribute('data-compact', 'true');
});

test('a press on a button inside the open box keeps it open, even where the browser does not focus the button', () => {
  render(<><p>Timeline</p><ReplyComposer /></>);
  act(() => screen.getByLabelText('Message body').focus());
  const note = screen.getByRole('button', { name: 'Private note' });
  fireEvent.pointerDown(note);
  // Safari leaves the focus nowhere when a button is pressed.
  act(() => screen.getByLabelText('Message body').blur());
  fireEvent.click(note);
  expect(card()).toHaveAttribute('data-compact', 'false');
  expect(screen.getByRole('button', { name: 'Private note', pressed: true })).toBeInTheDocument();
});

test('text in the box keeps it open when the agent reads the conversation above', () => {
  render(<><p>Timeline</p><ReplyComposer /></>);
  const body = screen.getByLabelText('Message body');
  act(() => body.focus());
  fireEvent.change(body, { target: { value: 'Half a reply' } });
  fireEvent.pointerDown(screen.getByText('Timeline'));
  act(() => body.blur());
  expect(card()).toHaveAttribute('data-compact', 'false');
});

test('a kept draft opens the box with its text', () => {
  localStorage.setItem('colliedesk.reply-draft:w:a:c-compact', JSON.stringify({ kind: 'outgoing', body: 'Kept text', saved_at: Date.now() }));
  render(<ReplyComposer draftKey="w:a:c-compact" />);
  expect(screen.getByLabelText('Message body')).toHaveValue('Kept text');
  expect(card()).toHaveAttribute('data-compact', 'false');
  localStorage.clear();
});

test('the R and N shortcuts and the saved replies key open the box', () => {
  render(<><p>Timeline</p><ReplyComposer savedReplies={[]} /></>);
  fireEvent.keyDown(document.body, { key: 'n' });
  expect(card()).toHaveAttribute('data-compact', 'false');
  fireEvent.pointerDown(screen.getByText('Timeline'));
  act(() => screen.getByLabelText('Message body').blur());
  expect(card()).toHaveAttribute('data-compact', 'true');
  fireEvent.keyDown(document.body, { key: '/' });
  expect(card()).toHaveAttribute('data-compact', 'false');
});

test('leaving the box with the keyboard closes it when nothing is written', () => {
  render(<><ReplyComposer /><button type="button">Elsewhere</button></>);
  const body = screen.getByLabelText('Message body');
  act(() => body.focus());
  fireEvent.blur(body, { relatedTarget: screen.getByRole('button', { name: 'Elsewhere' }) });
  expect(card()).toHaveAttribute('data-compact', 'true');
});

test('a press on the compact box does not open it by itself: the box opens when the press focuses it, so the tap lands where it was aimed', () => {
  render(<ReplyComposer />);
  const body = screen.getByLabelText('Message body');
  // On a touch screen pointerdown comes before the focus; opening on it moved the box under the finger.
  fireEvent.pointerDown(body);
  expect(card()).toHaveAttribute('data-compact', 'true');
  act(() => body.focus());
  expect(card()).toHaveAttribute('data-compact', 'false');
});

test('the compact box offers a private note in one press: it opens the box on a note with the cursor in it', () => {
  render(<><p>Timeline</p><ReplyComposer /></>);
  fireEvent.click(screen.getByRole('button', { name: 'Write a private note' }));
  expect(screen.getByRole('button', { name: 'Private note', pressed: true })).toBeInTheDocument();
  expect(screen.getByLabelText('Message body')).toHaveFocus();
  expect(card()).toHaveAttribute('data-compact', 'false');
  // Open, the tabs say which kind it is; the shortcut button is gone.
  expect(screen.queryByRole('button', { name: 'Write a private note' })).not.toBeInTheDocument();
});

test('a box already on a note offers no second way to it', () => {
  localStorage.setItem('colliedesk.reply-draft:w:a:c-note', JSON.stringify({ kind: 'note', body: '', saved_at: Date.now() }));
  render(<ReplyComposer draftKey="w:a:c-note" />);
  expect(card()).toHaveAttribute('data-compact', 'true');
  expect(screen.queryByRole('button', { name: 'Write a private note' })).not.toBeInTheDocument();
  localStorage.clear();
});

test('focus on the private note button keeps the box compact until the press lands, so the button is still there for it', () => {
  render(<ReplyComposer />);
  const button = screen.getByRole('button', { name: 'Write a private note' });
  // Chrome focuses a button on mousedown, before the click.
  act(() => button.focus());
  expect(card()).toHaveAttribute('data-compact', 'true');
  fireEvent.click(button);
  expect(screen.getByRole('button', { name: 'Private note', pressed: true })).toBeInTheDocument();
});

test('the box says it is being written in only after its text box took the focus, not when one of its buttons does', () => {
  localStorage.setItem('colliedesk.reply-draft:w:a:c-kept', JSON.stringify({ kind: 'outgoing', body: 'Kept text', saved_at: Date.now() }));
  render(<ReplyComposer draftKey="w:a:c-kept" />);
  // A kept draft holds the box open, but nobody is typing: a short screen keeps its bars (components.css).
  expect(card()).toHaveAttribute('data-writing', 'false');
  act(() => screen.getByRole('button', { name: 'Send reply' }).focus());
  expect(card()).toHaveAttribute('data-writing', 'false');
  act(() => screen.getByLabelText('Message body').focus());
  expect(card()).toHaveAttribute('data-writing', 'true');
  localStorage.clear();
});
