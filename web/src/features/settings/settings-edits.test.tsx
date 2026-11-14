import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { RouterProvider, createMemoryHistory, createRootRoute, createRoute, createRouter, Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { SettingsEditsProvider, Refused, useConfirmDrop, useReleaseUnsaved, useSettingsDraft, useSettingsEdit, useSettingsSaved, useUnsavedText } from './settings-edits';
import { confirmDiscardUnsavedSettings } from './unsaved-probe';

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

test('a choice that saved at once says Saved with Undo, and Undo runs the restoring request once', async () => {
  const undo = vi.fn();
  render(<SettingsEditsProvider><Choice undo={undo} /></SettingsEditsProvider>);
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Choose' }));
  expect(screen.getByRole('status')).toHaveTextContent('Saved');
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  expect(undo).toHaveBeenCalledTimes(1);
  await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument());
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


// ---- tabs, drafts that outlive a panel, text outside the bar, and the bar's own manners

function Draft({ saved = '', onSave = () => Promise.resolve() }: { saved?: string; onSave?: (value: string) => Promise<unknown> }) {
  const [value, setValue] = useSettingsDraft('limit', saved, { label: 'Limit', save: onSave });
  return <input aria-label="Limit" value={value} onChange={(event) => setValue(event.target.value)} />;
}

function Panels({ onSave }: { onSave?: (value: string) => Promise<unknown> }) {
  const [tab, setTab] = useState('chat');
  return <><button type="button" onClick={() => setTab(tab === 'chat' ? 'usage' : 'chat')}>Switch</button>{tab === 'chat' ? <Draft onSave={onSave} /> : <p>Usage</p>}</>;
}

test('a draft outlives the panel that typed it: the text and the bar are still there when the tab comes back, and Save sends it from another tab', async () => {
  const onSave = vi.fn(() => Promise.resolve());
  render(<SettingsEditsProvider><Panels onSave={onSave} /></SettingsEditsProvider>);
  type('Limit', '12.5');
  fireEvent.click(screen.getByRole('button', { name: 'Switch' }));
  expect(screen.queryByLabelText('Limit')).not.toBeInTheDocument();
  expect(bar()).toHaveTextContent('Limit');
  fireEvent.click(screen.getByRole('button', { name: 'Switch' }));
  expect(screen.getByLabelText('Limit')).toHaveValue('12.5');
  fireEvent.click(screen.getByRole('button', { name: 'Switch' }));
  fireEvent.click(screen.getByRole('button', { name: /^Save/ }));
  await waitFor(() => expect(onSave).toHaveBeenCalledWith('12.5'));
  await waitFor(() => expect(bar()).not.toBeInTheDocument());
});

test('Discard drops a draft whose panel is not on screen', () => {
  render(<SettingsEditsProvider><Panels /></SettingsEditsProvider>);
  type('Limit', '3');
  fireEvent.click(screen.getByRole('button', { name: 'Switch' }));
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  fireEvent.click(screen.getByRole('button', { name: 'Switch' }));
  expect(screen.getByLabelText('Limit')).toHaveValue('');
  expect(bar()).not.toBeInTheDocument();
});

test('a tab of the same page (a changed ?tab=) is not leaving: nothing is asked and the text stays', async () => {
  const root = createRootRoute();
  const page = createRoute({ getParentRoute: () => root, path: '/settings/ai', validateSearch: (search: Record<string, unknown>) => ({ tab: typeof search.tab === 'string' ? search.tab : undefined }),
    component: () => <SettingsEditsProvider><Field label="Limit" /><Link to={'/settings/ai' as never} search={{ tab: 'external' } as never}>External</Link></SettingsEditsProvider> });
  const router = createRouter({ routeTree: root.addChildren([page]), history: createMemoryHistory({ initialEntries: ['/settings/ai'] }) });
  render(<RouterProvider router={router} />);
  await screen.findByLabelText('Limit');
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  type('Limit', '9');
  await act(async () => { fireEvent.click(screen.getByRole('link', { name: 'External' })); });
  expect(confirm).not.toHaveBeenCalled();
  expect(router.state.location.search).toEqual({ tab: 'external' });
  expect(screen.getByLabelText('Limit')).toHaveValue('9');
});

function Typing({ text }: { text: string }) {
  useUnsavedText('holiday', text !== '');
  return null;
}

test('text that has its own button still counts as unsaved for the leave question and the tab close, but draws no bar', async () => {
  const root = createRootRoute();
  const account = createRoute({ getParentRoute: () => root, path: '/settings/account', component: () => <SettingsEditsProvider><Typing text="New Year" /><Link to="/settings/general">General</Link></SettingsEditsProvider> });
  const general = createRoute({ getParentRoute: () => root, path: '/settings/general', component: () => <p>General page</p> });
  const router = createRouter({ routeTree: root.addChildren([account, general]), history: createMemoryHistory({ initialEntries: ['/settings/account'] }) });
  render(<RouterProvider router={router} />);
  const link = await screen.findByRole('link', { name: 'General' });
  expect(bar()).not.toBeInTheDocument();
  await waitFor(() => expect(window.dispatchEvent(new Event('beforeunload', { cancelable: true }))).toBe(false));
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  await act(async () => { fireEvent.click(link); });
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(router.state.location.pathname).toBe('/settings/account');
});

test('Ctrl+S does nothing while a dialog is open', () => {
  const onSave = vi.fn(() => Promise.resolve());
  render(<SettingsEditsProvider><Field label="A" onSave={onSave} /><div role="dialog" aria-label="Dialog" /></SettingsEditsProvider>);
  type('A', 'x');
  expect(fireEvent.keyDown(document.body, { key: 's', ctrlKey: true })).toBe(true);
  expect(onSave).not.toHaveBeenCalled();
});

test('the bar is announced: a polite live region names the changed fields', () => {
  const { container } = render(<SettingsEditsProvider><Field label="Display name" /></SettingsEditsProvider>);
  const live = () => container.querySelector('[aria-live="polite"]');
  expect(live()).toHaveTextContent('');
  type('Display name', 'Tu');
  expect(live()).toHaveTextContent('Unsaved changes: Display name');
});

test('after Save or Discard the focus goes to the page heading, never to the body where Esc would leave Settings', async () => {
  render(<SettingsEditsProvider><h1>Account</h1><Field label="A" /></SettingsEditsProvider>);
  type('A', 'x');
  screen.getByLabelText('A').focus();
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  expect(screen.getByRole('heading', { name: 'Account' })).toHaveFocus();
  type('A', 'y');
  fireEvent.click(screen.getByRole('button', { name: /^Save/ }));
  await waitFor(() => expect(screen.getByRole('heading', { name: 'Account' })).toHaveFocus());
});

test('an Undo that is refused says so in the toast and keeps it for another try', async () => {
  const undo = vi.fn().mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce(undefined);
  render(<SettingsEditsProvider><Choice undo={undo} /></SettingsEditsProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Choose' }));
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('The change could not be undone');
  expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument());
  expect(undo).toHaveBeenCalledTimes(2);
});

test('the toast sits above the save bar when both show', () => {
  render(<SettingsEditsProvider><Field label="A" /><Choice undo={() => undefined} /></SettingsEditsProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Choose' }));
  expect(screen.getByRole('status')).not.toHaveClass('settings-toast-raised');
  type('A', 'x');
  expect(screen.getByRole('status')).toHaveClass('settings-toast-raised');
});

function Dropper() {
  const confirmDrop = useConfirmDrop();
  const [moved, setMoved] = useState(false);
  return <><Field label="A" /><button type="button" onClick={() => { if (confirmDrop('A')) setMoved(true); }}>Move</button>{moved && <p>Moved</p>}</>;
}

test('an action that would drop typed text asks first, and only moves on when the person agrees', () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true);
  render(<SettingsEditsProvider><Dropper /></SettingsEditsProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Move' }));
  expect(confirm).not.toHaveBeenCalled();
  expect(screen.getByText('Moved')).toBeInTheDocument();
  cleanup();
  render(<SettingsEditsProvider><Dropper /></SettingsEditsProvider>);
  type('A', 'x');
  fireEvent.click(screen.getByRole('button', { name: 'Move' }));
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(screen.queryByText('Moved')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Move' }));
  expect(screen.getByText('Moved')).toBeInTheDocument();
});


function Releasing() {
  useUnsavedText('typed', true);
  const release = useReleaseUnsaved();
  return <button type="button" onClick={release}>Release</button>;
}

test('a page that opens the next one itself, right after its own save, releases the typed text first so it is not asked about', async () => {
  render(<SettingsEditsProvider><Releasing /></SettingsEditsProvider>);
  await waitFor(() => expect(window.dispatchEvent(new Event('beforeunload', { cancelable: true }))).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: 'Release' }));
  await waitFor(() => expect(window.dispatchEvent(new Event('beforeunload', { cancelable: true }))).toBe(true));
});

test('a Save that fails says why on the bar, next to the field name, even when the field is on a tab that is not shown', async () => {
  const onSave = vi.fn().mockRejectedValue(new Refused('Use a number such as 12.50.'));
  render(<SettingsEditsProvider><Panels onSave={onSave} /></SettingsEditsProvider>);
  type('Limit', 'abc');
  fireEvent.click(screen.getByRole('button', { name: 'Switch' }));
  fireEvent.click(screen.getByRole('button', { name: /^Save/ }));
  expect(await within(bar()!).findByText('Limit: Use a number such as 12.50.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  expect(screen.queryByText('Limit: Use a number such as 12.50.')).not.toBeInTheDocument();
});

test('a Save that fails without words of its own says so in general terms', async () => {
  render(<SettingsEditsProvider><Field label="A" onSave={() => Promise.reject(new Error('boom'))} /></SettingsEditsProvider>);
  type('A', 'x');
  fireEvent.click(screen.getByRole('button', { name: /^Save/ }));
  expect(await within(bar()!).findByText('A: could not be saved')).toBeInTheDocument();
});

test('signing out asks about unsaved text through the same question, and only while something is unsaved', async () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true);
  expect(confirmDiscardUnsavedSettings()).toBe(true);
  const { unmount } = render(<SettingsEditsProvider><Field label="A" /></SettingsEditsProvider>);
  expect(confirmDiscardUnsavedSettings()).toBe(true);
  expect(confirm).not.toHaveBeenCalled();
  type('A', 'x');
  await waitFor(() => expect(screen.getByRole('region', { name: 'Unsaved changes' })).toBeInTheDocument());
  expect(confirmDiscardUnsavedSettings()).toBe(false);
  expect(confirmDiscardUnsavedSettings()).toBe(true);
  expect(confirm).toHaveBeenCalledWith('You have unsaved changes. Leave anyway?');
  unmount();
  expect(confirmDiscardUnsavedSettings()).toBe(true);
});

test('the bar tells the page how tall it is, so the toast stacks above it whatever its length', () => {
  const height = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(88);
  const { unmount } = render(<SettingsEditsProvider><Field label="A" /></SettingsEditsProvider>);
  type('A', 'x');
  expect(document.documentElement.style.getPropertyValue('--settings-save-bar-height')).toBe('88px');
  unmount();
  expect(document.documentElement.style.getPropertyValue('--settings-save-bar-height')).toBe('');
  height.mockRestore();
});


test('releasing the typed text and opening the next page in the same handler is not asked about, though nothing has drawn again yet', async () => {
  const root = createRootRoute();
  function Step() {
    useUnsavedText('typed', true);
    const release = useReleaseUnsaved();
    const navigate = useNavigate();
    return <button type="button" onClick={() => { release(); void navigate({ to: '/settings/general' as never }); }}>Create</button>;
  }
  const account = createRoute({ getParentRoute: () => root, path: '/settings/account', component: () => <SettingsEditsProvider><Step /></SettingsEditsProvider> });
  const general = createRoute({ getParentRoute: () => root, path: '/settings/general', component: () => <p>General page</p> });
  const router = createRouter({ routeTree: root.addChildren([account, general]), history: createMemoryHistory({ initialEntries: ['/settings/account'] }) });
  render(<RouterProvider router={router} />);
  const create = await screen.findByRole('button', { name: 'Create' });
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  await act(async () => { fireEvent.click(create); });
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/general'));
  expect(confirm).not.toHaveBeenCalled();
});
