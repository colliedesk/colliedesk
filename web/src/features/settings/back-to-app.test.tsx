import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { BackToApp, beforeSettings, forgetBeforeSettings, rememberBeforeSettings } from './back-to-app';

beforeEach(() => sessionStorage.clear());
afterEach(cleanup);

// press sends Escape the way the browser does for a real key: to the focused element, capture phase first.
function press(target: Element = document.body, init: KeyboardEventInit = {}) {
  fireEvent.keyDown(target, { key: 'Escape', ...init });
}

function setup() {
  const onBack = vi.fn();
  render(<>
    <aside className="settings-rail">
      <BackToApp href="/conversations/c1" onBack={onBack} />
      <nav className="settings-nav" aria-label="Menu"><a href="/settings/general">General</a></nav>
    </aside>
    <main>
      <input aria-label="Name" />
      <textarea aria-label="Note" />
      <select aria-label="Zone"><option>UTC</option></select>
      <input type="checkbox" aria-label="Notify" />
      <button type="button">Plain</button>
      <button type="button" data-tip="Plain tip">Tipped</button>
    </main>
  </>);
  return onBack;
}

test('Esc with focus on the page body leaves Settings', () => {
  const onBack = setup();
  press(document.body);
  expect(onBack).toHaveBeenCalledTimes(1);
});

test('Esc with focus in the settings menu, or on Back to app, leaves', () => {
  const onBack = setup();
  press(screen.getByRole('link', { name: 'General' }));
  press(screen.getByRole('link', { name: 'Back to app' }));
  expect(onBack).toHaveBeenCalledTimes(2);
});

test('Esc with focus in the page content stays there: buttons, checkboxes and fields all keep it for their own panels', () => {
  const onBack = setup();
  for (const control of [screen.getByRole('button', { name: 'Plain' }), screen.getByRole('checkbox', { name: 'Notify' }),
    screen.getByLabelText('Name'), screen.getByLabelText('Note'), screen.getByLabelText('Zone')]) {
    control.focus();
    press(control);
  }
  expect(onBack).not.toHaveBeenCalled();
});

test('Esc is looked at before the page handles it, so a dialog that closes on Esc is still open to be seen', () => {
  // Whatever closes the dialog on Esc (React handles it at the document too) is added before the app's listener.
  const dialog = document.createElement('div');
  dialog.setAttribute('role', 'dialog');
  const closeOnEsc = () => dialog.remove();
  document.addEventListener('keydown', closeOnEsc);
  const onBack = setup();
  document.body.append(dialog);
  press(document.body);
  document.removeEventListener('keydown', closeOnEsc);
  expect(dialog.isConnected).toBe(false);
  expect(onBack).not.toHaveBeenCalled();
});

test('with a dialog, a menu or a confirm panel open, Esc never leaves, whatever has the focus', () => {
  const onBack = setup();
  for (const attrs of [{ role: 'dialog' }, { role: 'menu' }, { class: 'confirm-panel', role: 'group' }, { class: 'account-popover' }]) {
    const open = document.createElement('div');
    for (const [name, value] of Object.entries(attrs)) open.setAttribute(name, value);
    document.body.append(open);
    press(document.body);
    press(screen.getByRole('link', { name: 'General' }));
    open.remove();
  }
  expect(onBack).not.toHaveBeenCalled();
});

test('Esc on a focused control that shows a tooltip is left to dismiss the tooltip', () => {
  const onBack = setup();
  const tipped = screen.getByRole('button', { name: 'Tipped' });
  tipped.focus();
  press(tipped);
  expect(onBack).not.toHaveBeenCalled();
});

test('Esc with a modifier, or already handled, does not leave', () => {
  const onBack = setup();
  press(document.body, { shiftKey: true });
  press(document.body, { metaKey: true });
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

const ana = 'w-1/a-1';

test('Back to app goes to the page kept before Settings, and to the inbox when none is kept or the kept one is not an app page', () => {
  expect(beforeSettings(ana)).toBe('/');
  rememberBeforeSettings('/conversations/c1?tab=open', ana);
  expect(beforeSettings(ana)).toBe('/conversations/c1?tab=open');
  rememberBeforeSettings('//evil.example/path', ana);
  expect(beforeSettings(ana)).toBe('/');
  rememberBeforeSettings('https://evil.example/', ana);
  expect(beforeSettings(ana)).toBe('/');
  rememberBeforeSettings('/settings/members', ana);
  expect(beforeSettings(ana)).toBe('/');
});

test('an address kept by another person or workspace is never offered', () => {
  rememberBeforeSettings('/conversations/c1', ana);
  expect(beforeSettings('w-1/a-2')).toBe('/');
  expect(beforeSettings('w-2/a-1')).toBe('/');
  expect(beforeSettings(ana)).toBe('/conversations/c1');
});

test('forgetting the kept address leaves the inbox for everyone', () => {
  rememberBeforeSettings('/conversations/c1', ana);
  forgetBeforeSettings();
  expect(beforeSettings(ana)).toBe('/');
});
