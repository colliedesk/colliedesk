import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { RouterProvider, createMemoryHistory, createRootRoute, createRoute, createRouter, Link } from '@tanstack/react-router';
import { useState } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { SettingsEditsProvider, useSettingsEdit, useSettingsSaved } from './settings-edits';

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

// Field is a text setting the way a page writes one: a draft over the saved value, registered while it differs.
function Field({ label, saved = '', onSave = () => Promise.resolve() }: { label: string; saved?: string; onSave?: (value: string) => Promise<unknown> }) {
  const [draft, setDraft] = useState(saved);
  useSettingsEdit(label, draft === saved ? undefined : { label, save: () => onSave(draft), discard: () => setDraft(saved) });
  return <input aria-label={label} value={draft} onChange={(event) => setDraft(event.target.value)} />;
}

function Choice({ undo }: { undo: () => void }) {
  const saved = useSettingsSaved();
  return <button type="button" onClick={() => saved(undo)}>Choose</button>;
}

const bar = () => screen.queryByRole('region', { name: 'Unsaved changes' });
const type = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });

test('the save bar names every changed field and is absent while nothing changed', () => {
  render(<SettingsEditsProvider><Field label="Display name" /><Field label="Workspace name" /></SettingsEditsProvider>);
  expect(bar()).not.toBeInTheDocument();
  type('Display name', 'Tu');
  expect(bar()).toHaveTextContent('Unsaved changes');
  expect(bar()).toHaveTextContent('Display name');
  expect(bar()).not.toHaveTextContent('Workspace name');
  type('Workspace name', 'Northwind');
  expect(bar()).toHaveTextContent('Display name, Workspace name');
});

test('Discard puts every changed field back to its saved value and removes the bar', () => {
  render(<SettingsEditsProvider><Field label="Display name" saved="Ana" /><Field label="Workspace name" saved="Acme" /></SettingsEditsProvider>);
  type('Display name', 'Tu');
  type('Workspace name', 'Northwind');
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  expect(screen.getByLabelText('Display name')).toHaveValue('Ana');
  expect(screen.getByLabelText('Workspace name')).toHaveValue('Acme');
  expect(bar()).not.toBeInTheDocument();
});

test('Save runs the saves of the changed fields one after the other, and only those', async () => {
  const order: string[] = [];
  const slow = (name: string) => async () => { order.push(`${name} start`); await Promise.resolve(); order.push(`${name} end`); };
  render(<SettingsEditsProvider><Field label="A" onSave={slow('a')} /><Field label="B" onSave={slow('b')} /><Field label="C" onSave={slow('c')} /></SettingsEditsProvider>);
  type('A', 'x');
  type('C', 'y');
  fireEvent.click(screen.getByRole('button', { name: /^Save/ }));
  await waitFor(() => expect(order).toEqual(['a start', 'a end', 'c start', 'c end']));
});

test('Ctrl+S and Cmd+S save, and the browser does not open its own save dialog', async () => {
  const onSave = vi.fn(() => Promise.resolve());
  render(<SettingsEditsProvider><Field label="Display name" onSave={onSave} /></SettingsEditsProvider>);
  // Nothing changed: the key is left to the browser.
  expect(fireEvent.keyDown(document.body, { key: 's', ctrlKey: true })).toBe(true);
  expect(onSave).not.toHaveBeenCalled();
  type('Display name', 'Tu');
  expect(fireEvent.keyDown(screen.getByLabelText('Display name'), { key: 's', ctrlKey: true })).toBe(false);
  await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
  expect(fireEvent.keyDown(document.body, { key: 'S', metaKey: true })).toBe(false);
  await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2));
});

test('a refused save stops the run and keeps the bar, so the typed text is not lost', async () => {
  const later = vi.fn(() => Promise.resolve());
  render(<SettingsEditsProvider><Field label="A" onSave={() => Promise.reject(new Error('refused'))} /><Field label="B" onSave={later} /></SettingsEditsProvider>);
  type('A', 'x');
  type('B', 'y');
  fireEvent.click(screen.getByRole('button', { name: /^Save/ }));
  await waitFor(() => expect(screen.getByRole('button', { name: /^Save/ })).toBeEnabled());
  expect(later).not.toHaveBeenCalled();
  expect(bar()).toHaveTextContent('A, B');
  expect(screen.getByLabelText('A')).toHaveValue('x');
});

test('closing the tab with unsaved text asks the browser to confirm, and a clean page does not', () => {
  render(<SettingsEditsProvider><Field label="Display name" /></SettingsEditsProvider>);
  expect(window.dispatchEvent(new Event('beforeunload', { cancelable: true }))).toBe(true);
  type('Display name', 'Tu');
  expect(window.dispatchEvent(new Event('beforeunload', { cancelable: true }))).toBe(false);
});

// pageWith mounts the provider on /settings/account of a router that also has /settings/general.
async function pageWith(label = 'Display name') {
  const root = createRootRoute();
  const account = createRoute({ getParentRoute: () => root, path: '/settings/account', component: () => <SettingsEditsProvider>
    <Field label={label} /><Link to="/settings/general">General</Link><Link to="/settings/account" hash="top">Top</Link></SettingsEditsProvider> });
  const general = createRoute({ getParentRoute: () => root, path: '/settings/general', component: () => <p>General page</p> });
  const router = createRouter({ routeTree: root.addChildren([account, general]), history: createMemoryHistory({ initialEntries: ['/settings/account'] }) });
  render(<RouterProvider router={router} />);
  await screen.findByLabelText(label);
  return router;
}

test('opening another page with unsaved text asks once; staying keeps the text', async () => {
  const router = await pageWith();
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  type('Display name', 'Tu');
  await act(async () => { fireEvent.click(screen.getByRole('link', { name: 'General' })); });
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(confirm).toHaveBeenCalledWith('You have unsaved changes. Leave anyway?');
  expect(router.state.location.pathname).toBe('/settings/account');
  expect(screen.getByLabelText('Display name')).toHaveValue('Tu');
});

test('choosing to leave opens the page and drops the text', async () => {
  const router = await pageWith();
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
  type('Display name', 'Tu');
  await act(async () => { fireEvent.click(screen.getByRole('link', { name: 'General' })); });
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/general'));
  expect(confirm).toHaveBeenCalledTimes(1);
});

test('a page with nothing changed, or a move that stays on the page, asks nothing', async () => {
  const router = await pageWith();
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  await act(async () => { fireEvent.click(screen.getByRole('link', { name: 'General' })); });
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/general'));
  expect(confirm).not.toHaveBeenCalled();
});

test('a link to a section of the same page keeps the text and asks nothing', async () => {
  await pageWith();
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  type('Display name', 'Tu');
  await act(async () => { fireEvent.click(screen.getByRole('link', { name: 'Top' })); });
  expect(confirm).not.toHaveBeenCalled();
  expect(screen.getByLabelText('Display name')).toHaveValue('Tu');
});

test('a choice that saved at once says Saved with Undo, and Undo runs the restoring request once', () => {
  const undo = vi.fn();
  render(<SettingsEditsProvider><Choice undo={undo} /></SettingsEditsProvider>);
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Choose' }));
  expect(screen.getByRole('status')).toHaveTextContent('Saved');
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  expect(undo).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('the Saved toast goes away on its own', () => {
  vi.useFakeTimers();
  try {
    render(<SettingsEditsProvider><Choice undo={() => undefined} /></SettingsEditsProvider>);
    fireEvent.click(screen.getByRole('button', { name: 'Choose' }));
    expect(screen.getByRole('status')).toBeInTheDocument();
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  } finally { vi.useRealTimers(); }
});
