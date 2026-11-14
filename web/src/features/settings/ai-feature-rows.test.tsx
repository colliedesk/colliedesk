import { fireEvent, screen, within } from '@testing-library/react';
import { expect, test } from 'vitest';
import { page, provider, providerID, settings, slot } from './ai-fixtures';

const onTeamKey = () => settings({ providers: [provider({ used_by: ['triage'] })], slots: [
  slot({ source: 'provider', provider_id: providerID, model: 'claude-haiku-5-5', enabled: true, active: true, default_level: 'suggest' }),
  slot({ slot: 'draft', source: 'provider', provider_id: providerID, model: 'claude-sonnet-5-5' }), slot({ slot: 'chat' })] });

test('each feature is one row with its name, one short line, its state and its switch, and no provider or model select', () => {
  page({ settings: onTeamKey() });
  const rows = screen.getByRole('list', { name: 'Features' });
  const triage = within(rows).getAllByRole('listitem')[0];
  expect(within(triage).getByText('Triage suggestions')).toBeInTheDocument();
  expect(within(triage).getByText('Suggests tags, priority and an assignee for new tickets.')).toBeInTheDocument();
  expect(within(triage).getByText('On · Suggest')).toBeInTheDocument();
  expect(within(triage).getByRole('switch', { name: 'Triage suggestions on' })).toBeChecked();
  expect(within(rows).getAllByRole('listitem')).toHaveLength(3);
  expect(within(rows).getAllByText('Off', { selector: '.ai-state' })).toHaveLength(2);
  expect(screen.queryByRole('combobox')).toBeNull();
});

test('every feature line is 60 characters or less, and the longer explanation waits in its sheet', () => {
  page({ settings: onTeamKey(), chatChannels: true });
  for (const row of within(screen.getByRole('list', { name: 'Features' })).getAllByRole('listitem')) {
    const line = row.querySelector('.ai-row-line');
    expect(line?.textContent?.length ?? 0).toBeGreaterThan(0);
    expect(line?.textContent?.length ?? 0).toBeLessThanOrEqual(60);
  }
  expect(screen.queryByText(/An agent accepts or dismisses each one/)).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Triage suggestions settings' }));
  expect(within(screen.getByRole('dialog', { name: 'Triage suggestions' })).getByText(/An agent accepts or dismisses each one/)).toBeInTheDocument();
});

test('the switch saves at once with the same call as before', () => {
  const { onSetSlot } = page({ settings: onTeamKey() });
  fireEvent.click(screen.getByRole('switch', { name: 'Reply drafts on' }));
  expect(onSetSlot).toHaveBeenLastCalledWith('draft', { source: 'provider', provider_id: providerID, model: 'claude-sonnet-5-5', effort: 'low', enabled: true });
  expect(screen.queryByRole('dialog')).toBeNull();
});

test('the rest of the row opens its sheet with provider, model and automatic limits, and the model saves through the same call', () => {
  const { onSetSlot } = page({ settings: onTeamKey() });
  fireEvent.click(screen.getByRole('button', { name: 'Triage suggestions settings' }));
  const sheet = screen.getByRole('dialog', { name: 'Triage suggestions' });
  expect(within(sheet).getByRole('combobox', { name: 'Triage suggestions provider' })).toHaveValue(providerID);
  expect(within(sheet).getByText('300 a day · 30 a minute')).toBeInTheDocument();
  expect(within(sheet).getByRole('group', { name: 'Triage: default for channels' })).toBeInTheDocument();
  fireEvent.change(within(sheet).getByRole('combobox', { name: 'Triage suggestions model' }), { target: { value: 'claude-sonnet-5-5' } });
  expect(onSetSlot).toHaveBeenLastCalledWith('triage', { source: 'provider', provider_id: providerID, model: 'claude-sonnet-5-5', effort: 'low', enabled: true });
});

test('the sheet closes with Done and with Escape, and the row gets the focus back', () => {
  page({ settings: onTeamKey() });
  const open = screen.getByRole('button', { name: 'Reply drafts settings' });
  open.focus();
  fireEvent.click(open);
  fireEvent.click(within(screen.getByRole('dialog', { name: 'Reply drafts' })).getByRole('button', { name: 'Done' }));
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(document.activeElement).toBe(open);
  fireEvent.click(open);
  fireEvent.keyDown(screen.getByRole('dialog', { name: 'Reply drafts' }), { key: 'Escape' });
  expect(screen.queryByRole('dialog')).toBeNull();
});

test('with no provider one notice offers Add provider and the operator guide, and each switch says why it is off', () => {
  page();
  const notice = screen.getByRole('status');
  expect(within(notice).getByRole('button', { name: 'Add provider' })).toBeInTheDocument();
  expect(within(notice).getByRole('link', { name: 'Setup guide' })).toHaveAttribute('href', expect.stringContaining('environment-variables.md#ai-provider'));
  for (const name of ['Triage suggestions on', 'Reply drafts on']) {
    const off = screen.getByRole('switch', { name });
    expect(off).toBeDisabled();
    expect(off.closest('li')?.querySelector('.help-tip-button')).toHaveAccessibleDescription('Add a provider first, then turn this on.');
  }
});

test('a switch that is off for a missing provider says so, also once a provider exists and the feature has none picked', () => {
  page({ settings: settings({ providers: [provider()], slots: [slot(), slot({ slot: 'draft' })] }) });
  for (const name of ['Triage suggestions on', 'Reply drafts on']) {
    const off = screen.getByRole('switch', { name });
    expect(off).toBeDisabled();
    expect(off).toHaveAccessibleDescription('Open this row and pick a provider first.');
    expect(off.closest('li')?.querySelector('.help-tip-button')).toBeInTheDocument();
  }
});

test('a switch of a feature set to a server provider that is off says to pick another provider', () => {
  page({ settings: settings({ server_off: { label: 'Colliedesk AI' }, providers: [provider()],
    slots: [slot({ source: 'server', model: 'claude-haiku-5-5', enabled: true, active: false, off_reason: 'server_provider_off' }), slot({ slot: 'draft' })] }) });
  expect(screen.getByRole('switch', { name: 'Triage suggestions on' })).toHaveAccessibleDescription('Open this row and pick a provider first.');
});

test('every switch that is off for a person who is not an admin says who may turn it on', () => {
  page({ isAdmin: false, settings: onTeamKey() });
  expect(screen.getByRole('switch', { name: 'Reply drafts on' })).toHaveAccessibleDescription('Only admins can turn this on or off.');
});

test('a switch that works has no lock', () => {
  page({ settings: onTeamKey() });
  const on = screen.getByRole('switch', { name: 'Triage suggestions on' });
  expect(on).toBeEnabled();
  expect(on.closest('li')?.querySelector('.help-tip-button')).toBeNull();
});

test('the row button says that it opens a dialog', () => {
  page({ settings: onTeamKey() });
  expect(screen.getByRole('button', { name: 'Reply drafts settings' })).toHaveAttribute('aria-haspopup', 'dialog');
});

test('with no provider Add provider is offered once on Features, and stays on the Providers tab', () => {
  page();
  expect(screen.getAllByRole('button', { name: /Add provider/ })).toHaveLength(1);
  fireEvent.click(screen.getByRole('tab', { name: /Providers/ }));
  expect(screen.getAllByRole('button', { name: /Add provider/ })).toHaveLength(1);
});

test('with a provider the tab row keeps its Add provider on the Features tab', () => {
  page({ settings: onTeamKey() });
  expect(screen.getAllByRole('button', { name: /Add provider/ })).toHaveLength(1);
});
