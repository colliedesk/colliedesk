import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { BackToApp, beforeSettings, rememberBeforeSettings } from './back-to-app';

beforeEach(() => sessionStorage.clear());
afterEach(cleanup);

function setup() {
  const onBack = vi.fn();
  render(<>
    <BackToApp href="/conversations/c1" onBack={onBack} />
    <input aria-label="Name" />
    <textarea aria-label="Note" />
    <select aria-label="Zone"><option>UTC</option></select>
    <input type="checkbox" aria-label="Notify" />
    <button type="button">Plain</button>
  </>);
  return onBack;
}

test('Esc on the page leaves Settings', () => {
  const onBack = setup();
  fireEvent.keyDown(document.body, { key: 'Escape' });
  expect(onBack).toHaveBeenCalledTimes(1);
});

test('Esc on a button or a checkbox leaves too: they hold no text', () => {
  const onBack = setup();
  fireEvent.keyDown(screen.getByRole('button', { name: 'Plain' }), { key: 'Escape' });
  fireEvent.keyDown(screen.getByRole('checkbox', { name: 'Notify' }), { key: 'Escape' });
  expect(onBack).toHaveBeenCalledTimes(2);
});

test('Esc in a text field, a text area or a select stays with it', () => {
  const onBack = setup();
  for (const name of ['Name', 'Note', 'Zone']) {
    const field = screen.getByLabelText(name);
    field.focus();
    fireEvent.keyDown(field, { key: 'Escape' });
  }
  expect(onBack).not.toHaveBeenCalled();
});

test('Esc while a dialog or a menu is open belongs to it', () => {
  const onBack = setup();
  const dialog = document.createElement('div');
  dialog.setAttribute('role', 'dialog');
  document.body.append(dialog);
  fireEvent.keyDown(document.body, { key: 'Escape' });
  dialog.remove();
  const menu = document.createElement('div');
  menu.setAttribute('role', 'menu');
  document.body.append(menu);
  fireEvent.keyDown(document.body, { key: 'Escape' });
  menu.remove();
  expect(onBack).not.toHaveBeenCalled();
  fireEvent.keyDown(document.body, { key: 'Escape' });
  expect(onBack).toHaveBeenCalledTimes(1);
});

test('Esc with a modifier, or already handled, does not leave', () => {
  const onBack = setup();
  fireEvent.keyDown(document.body, { key: 'Escape', shiftKey: true });
  fireEvent.keyDown(document.body, { key: 'Escape', metaKey: true });
  const handled = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
  handled.preventDefault();
  document.body.dispatchEvent(handled);
  expect(onBack).not.toHaveBeenCalled();
});

test('a plain click on Back to app leaves through the app; a click to open in a new tab keeps the link', () => {
  const onBack = setup();
  const link = screen.getByRole('link', { name: 'Back to app' });
  expect(link).toHaveAttribute('href', '/conversations/c1');
  fireEvent.click(link);
  expect(onBack).toHaveBeenCalledTimes(1);
  fireEvent.click(link, { ctrlKey: true });
  expect(onBack).toHaveBeenCalledTimes(1);
});

test('Back to app goes to the page kept before Settings, and to the inbox when none is kept or the kept one is not an app page', () => {
  expect(beforeSettings()).toBe('/');
  rememberBeforeSettings('/conversations/c1?tab=open');
  expect(beforeSettings()).toBe('/conversations/c1?tab=open');
  rememberBeforeSettings('//evil.example/path');
  expect(beforeSettings()).toBe('/');
  rememberBeforeSettings('https://evil.example/');
  expect(beforeSettings()).toBe('/');
  rememberBeforeSettings('/settings/members');
  expect(beforeSettings()).toBe('/');
});
