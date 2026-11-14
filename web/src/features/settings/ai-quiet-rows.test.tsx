import { act, fireEvent, screen, within } from '@testing-library/react';
import { expect, test } from 'vitest';
import { closeSheet, openSheet, page, provider, providerID, server, settings, slot } from './ai-fixtures';

// Each row of Built-in AI keeps its label and one line of 60 characters or less. What it used to say below that line
// is the accessible description of the control it is about, in a help tip.

const triageOn = () => settings({ server, slots: [slot({ source: 'server', model: 'claude-haiku-5-5', enabled: true, active: true }), slot({ slot: 'draft' })] });

function openAddProvider() {
  const handlers = page();
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  return { ...handlers, dialog: screen.getByRole('dialog', { name: 'Add a provider' }) };
}

test('Automatic runs says in one short line what the limits cover, and defines automatic in the tip of its first field', () => {
  page({ settings: triageOn() });
  openSheet('Triage suggestions');
  fireEvent.click(screen.getByRole('button', { name: 'Change the automatic limits' }));
  const dialog = screen.getByRole('dialog', { name: 'Automatic runs' });
  expect(within(dialog).getByText('Limits what Colliedesk starts, not what an agent asks for.')).toBeVisible();
  const day = within(dialog).getByLabelText('A day (UTC)');
  expect(day).toHaveAccessibleDescription('Work Colliedesk starts on its own, such as a suggestion for each new ticket.');
  expect(within(dialog).getByRole('button', { name: 'About A day (UTC)' })).toHaveAccessibleDescription(/such as a suggestion for each new ticket/);
});

test('the daily drafts limit keeps its short line, and the order and the per-conversation cap sit in its tip', () => {
  page({ settings: triageOn() });
  openSheet('Triage suggestions');
  fireEvent.click(screen.getByRole('button', { name: 'Change the automatic limits' }));
  const dialog = screen.getByRole('dialog', { name: 'Automatic runs' });
  expect(within(dialog).getByText('Counted inside the runs a day.')).toBeVisible();
  const field = within(dialog).getByLabelText('Automatic drafts a day');
  expect(field).toHaveAccessibleDescription(/Counted inside the runs a day\..*Triage runs first when both wait\. Each conversation gets at most 5 automatic drafts in 24 hours\./);
  expect(within(dialog).getByRole('button', { name: 'About Automatic drafts a day' })).toBeInTheDocument();
});

test('a self-hosted address shows one example line, and the key and models of a new provider say the rest in tips', async () => {
  const { dialog } = openAddProvider();
  fireEvent.click(within(dialog).getByRole('radio', { name: /Other or self-hosted/ }));
  expect(within(dialog).getByLabelText('Base URL')).toHaveAccessibleDescription('The API address, such as https://openrouter.ai/api/v1.');
  expect(within(dialog).getByText('Enter the address and key, then load and tick the models.')).toBeVisible();
  await act(async () => { fireEvent.click(within(dialog).getByRole('radio', { name: /Anthropic/ })); });
  expect(within(dialog).getByText('Paste the key, then load the models and tick yours.')).toBeVisible();
});

test('typed model names keep "Separate with commas." and the one-word check request sits in the field’s tip', async () => {
  const { dialog } = openAddProvider();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Type model names instead' }));
  expect(within(dialog).getByText('Separate with commas.')).toBeVisible();
  expect(within(dialog).getByLabelText('Model names')).toHaveAccessibleDescription(/Separate with commas\..*The check sends a one-word request to the first model\./);
});

test('picked models say how many in one line, and which is the default and what the check sends in the tip', async () => {
  const { dialog, onListModels } = openAddProvider();
  onListModels.mockResolvedValueOnce(['claude-opus-5-5', 'claude-sonnet-5-5', 'claude-haiku-5-5']);
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Team key' } });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'sk-ant-1' } });
  await act(async () => { fireEvent.blur(within(dialog).getByLabelText('API key')); });
  expect(within(dialog).getByText(/^1 of at most \d+ picked\.$/)).toBeVisible();
  expect(within(dialog).getByRole('group', { name: 'Models' })).toHaveAccessibleDescription(
    /1 of at most \d+ picked\..*The first one picked is the default; the check sends it a one-word request\./);
});

test('changing a key says in one line when the old key stops working', () => {
  page({ settings: settings({ providers: [provider()] }) });
  fireEvent.click(screen.getByRole('tab', { name: /Providers/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Change key' }));
  expect(within(screen.getByRole('dialog', { name: 'Change the key of Team key' })).getByText('The current key works until the new one passes a check.')).toBeVisible();
});

test('the usage tab keeps one line about its rows, and who pays for calls on a key sits in a tip', () => {
  page({ usage: { month: '2026-10', allowance: null, rows: [] } });
  fireEvent.click(screen.getByRole('tab', { name: 'Usage' }));
  expect(screen.getByText('One row per call to a provider, failed calls included.')).toBeVisible();
  expect(screen.getByRole('button', { name: 'About Usage by feature' })).toHaveAccessibleDescription(
    'Calls on your own keys are billed by that provider, so they have no cost here.');
});

test('the history tab keeps one line, and that keys are never shown sits in a tip', () => {
  page({ settings: settings({ providers: [provider()] }) });
  fireEvent.click(screen.getByRole('tab', { name: 'History' }));
  expect(screen.getByText('Who changed a provider, a key or a feature, and when.')).toBeVisible();
  expect(screen.getByRole('button', { name: 'About History' })).toHaveAccessibleDescription('Keys are never shown, only their last 4 characters.');
});

test('the dialogs that ask before AI does more by itself lead with one line, and the lists below keep the details', async () => {
  const on = slot({ source: 'provider', provider_id: providerID, model: 'claude-haiku-5-5', enabled: true, active: true, default_level: 'suggest' });
  page({ settings: settings({ providers: [provider({ used_by: ['triage'] })], slots: [on, slot({ slot: 'draft' })] }) });
  openSheet('Triage suggestions');
  fireEvent.click(within(screen.getByRole('group', { name: 'Triage: default for channels' })).getByRole('radio', { name: 'Auto' }));
  const triage = await screen.findByRole('dialog', { name: 'Apply triage suggestions automatically?' });
  expect(within(triage).getByText('New tickets get AI’s tags and priority on arrival.')).toBeVisible();
  expect(within(triage).getByText(/Each change says “Applied by AI” on the ticket, with Undo\./)).toBeVisible();
  fireEvent.click(within(triage).getByRole('button', { name: 'Cancel' }));
  closeSheet();
});

test('the dialog before automatic drafts says in one line that AI never sends', async () => {
  const drafts = slot({ slot: 'draft', source: 'server', model: 'claude-sonnet-5-5', enabled: true, active: true });
  page({ settings: settings({ server, slots: [slot(), drafts], allowance: { limit_micro_usd: 9_000_000, used_micro_usd: 0, resets_at: '2026-11-01T00:00:00Z', drafts_left: 140 },
    auto_draft_estimate: { customer_messages: 412, draft_cost_micro_usd: 64_000 } }) });
  openSheet('Reply drafts');
  fireEvent.click(within(screen.getByRole('group', { name: 'Reply drafts: default for channels' })).getByRole('radio', { name: 'Suggest' }));
  const dialog = await screen.findByRole('dialog', { name: 'Write a draft for every customer message?' });
  expect(within(dialog).getByText('AI never sends; a person reads each draft and presses Send.')).toBeVisible();
});
