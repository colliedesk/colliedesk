import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { openSheet, page, provider, providerID, server, settings, slot } from './ai-fixtures';

// A switch of Built-in AI is a choice: it saves when it is made, says "Saved" and offers Undo, which puts the
// previous value back with one request. Turning on chat intake asks the person to agree first.

const drafts = slot({ slot: 'draft', source: 'provider', provider_id: providerID, model: 'claude-sonnet-5-5' });
const withDrafts = () => settings({ providers: [provider({ used_by: ['draft'] })], slots: [slot(), drafts, slot({ slot: 'chat' })] });
const intakeOff = slot({ slot: 'intake', source: 'provider', provider_id: providerID, model: 'claude-haiku-5-5' });
const withIntake = (consents: string[] = []) => settings({ providers: [provider({ used_by: ['intake'] })], slots: [slot(), slot({ slot: 'draft' }), intakeOff],
  source_consents: consents.map((source_class) => ({ source_class, consented_by: null, consented_at: '2026-10-25T08:00:00Z' })) });

test('a feature switch says Saved with Undo, and Undo sends the previous value in one request', async () => {
  const onSetSlot = vi.fn().mockResolvedValue(undefined);
  page({ settings: withDrafts(), onSetSlot });
  fireEvent.click(screen.getByRole('switch', { name: 'Reply drafts on' }));
  const saved = await screen.findByText('Saved');
  expect(onSetSlot).toHaveBeenCalledTimes(1);
  fireEvent.click(within(saved.closest('.settings-toast') as HTMLElement).getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(onSetSlot).toHaveBeenCalledTimes(2));
  expect(onSetSlot).toHaveBeenNthCalledWith(1, 'draft', { source: 'provider', provider_id: providerID, model: 'claude-sonnet-5-5', effort: 'low', enabled: true });
  expect(onSetSlot).toHaveBeenNthCalledWith(2, 'draft', { source: 'provider', provider_id: providerID, model: 'claude-sonnet-5-5', effort: 'low', enabled: false });
  await waitFor(() => expect(screen.queryByText('Saved')).toBeNull());
});

test('a feature switch the server refuses says nothing is saved', async () => {
  const onSetSlot = vi.fn().mockRejectedValue(new Error('refused'));
  page({ settings: withDrafts(), onSetSlot });
  fireEvent.click(screen.getByRole('switch', { name: 'Reply drafts on' }));
  await waitFor(() => expect(onSetSlot).toHaveBeenCalled());
  await new Promise((resolve) => { setTimeout(resolve, 30); });
  expect(screen.queryByText('Saved')).toBeNull();
});

test('an Undo the server refuses says so and keeps the button', async () => {
  const onSetSlot = vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('refused'));
  page({ settings: withDrafts(), onSetSlot });
  fireEvent.click(screen.getByRole('switch', { name: 'Reply drafts on' }));
  fireEvent.click(within((await screen.findByText('Saved')).closest('.settings-toast') as HTMLElement).getByRole('button', { name: 'Undo' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/could not be undone/);
  expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
});

test('a source switch of chat intake says Saved, and Undo gives the consent back or takes it again', async () => {
  const onSourceConsent = vi.fn().mockResolvedValue(undefined);
  page({ chatChannels: true, onSourceConsent, settings: withIntake() });
  openSheet('Chat intake');
  const group = screen.getByRole('group', { name: 'What chat intake may send besides the messages' });
  fireEvent.click(within(group).getByRole('switch', { name: 'Images customers sent' }));
  expect(onSourceConsent).toHaveBeenLastCalledWith('images', true);
  fireEvent.click(within((await screen.findByText('Saved')).closest('.settings-toast') as HTMLElement).getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(onSourceConsent).toHaveBeenLastCalledWith('images', false));
  expect(onSourceConsent).toHaveBeenCalledTimes(2);
});

test('turning chat intake on asks the person to agree first, and nothing is saved until they do', async () => {
  const onSetSlot = vi.fn().mockResolvedValue(undefined);
  page({ chatChannels: true, onSetSlot, settings: withIntake() });
  fireEvent.click(screen.getByRole('switch', { name: 'Chat intake on' }));
  const sheet = screen.getByRole('dialog', { name: 'Turn on Chat intake' });
  expect(onSetSlot).not.toHaveBeenCalled();
  expect(within(sheet).getByText('What goes to the model')).toBeInTheDocument();
  expect(within(sheet).getByText('Who processes it')).toBeInTheDocument();
  expect(within(sheet).getByText('Team key, model claude-haiku-5-5.')).toBeInTheDocument();
  expect(within(sheet).getByText('Turning it off')).toBeInTheDocument();
  const agree = within(sheet).getByRole('button', { name: 'Agree and turn on' });
  expect(agree).toBeDisabled();
  expect(within(sheet).getByText('Stays off until you agree')).toBeInTheDocument();
  fireEvent.click(within(sheet).getByRole('checkbox', { name: /I agree that messages in linked chat channels are sent to Team key/ }));
  expect(agree).toBeEnabled();
  fireEvent.click(agree);
  await waitFor(() => expect(onSetSlot).toHaveBeenCalledWith('intake', { source: 'provider', provider_id: providerID, model: 'claude-haiku-5-5', effort: 'low', enabled: true }));
  expect(screen.queryByRole('dialog', { name: 'Turn on Chat intake' })).toBeNull();
  expect(await screen.findByText('Saved')).toBeInTheDocument();
});

test('closing the consent step leaves chat intake off', () => {
  const onSetSlot = vi.fn();
  page({ chatChannels: true, onSetSlot, settings: withIntake() });
  fireEvent.click(screen.getByRole('switch', { name: 'Chat intake on' }));
  fireEvent.keyDown(screen.getByRole('dialog', { name: 'Turn on Chat intake' }), { key: 'Escape' });
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(onSetSlot).not.toHaveBeenCalled();
  expect(screen.getByRole('switch', { name: 'Chat intake on' })).not.toBeChecked();
});

test('the consent step names the server provider when the feature runs on it, and turning intake off needs no agreement', async () => {
  const onSetSlot = vi.fn().mockResolvedValue(undefined);
  const on = slot({ slot: 'intake', source: 'server', enabled: true, active: true });
  page({ chatChannels: true, onSetSlot, settings: settings({ server: { ...server, models: { ...server.models, intake: ['claude-haiku-5-5'] } }, slots: [slot(), slot({ slot: 'draft' }), on] }) });
  fireEvent.click(screen.getByRole('switch', { name: 'Chat intake on' }));
  expect(screen.queryByRole('dialog')).toBeNull();
  await waitFor(() => expect(onSetSlot).toHaveBeenCalledWith('intake', { source: 'server', enabled: false, effort: 'low' }));
});
