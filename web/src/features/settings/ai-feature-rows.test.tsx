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
