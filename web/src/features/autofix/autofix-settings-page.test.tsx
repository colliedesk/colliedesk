import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { AutofixSettings, GitHubRepo } from './autofix-api';
import { AutofixSettingsPage } from './autofix-settings-page';
import type { AutofixSettingsPageProps, SettingsActions } from './autofix-settings-page';
import { connectorOf, now, repoOf, settingsOf } from './autofix.test.fixture';

const actionsOf = (): SettingsActions => ({
  connectGitHub: vi.fn(async () => true), disconnectGitHub: vi.fn(async () => true), connectRepo: vi.fn(async () => true),
  disconnectRepo: vi.fn(async () => true), setDefault: vi.fn(async () => true), setNotes: vi.fn(async () => true), enable: vi.fn(async () => true),
  disable: vi.fn(async () => true), check: vi.fn(async () => true), setKey: vi.fn(async () => true), removeKey: vi.fn(async () => true),
});

const githubRepos: GitHubRepo[] = [
  { github_repo_id: 901, full_name: 'northwind/web', visibility: 'private', connected_repo_id: repoOf().id },
  { github_repo_id: 902, full_name: 'northwind/mobile', visibility: 'private', connected_repo_id: null },
  { github_repo_id: 903, full_name: 'northwind/docs', visibility: 'public', connected_repo_id: null },
  { github_repo_id: 904, full_name: 'northwind/infra', visibility: 'internal', connected_repo_id: null },
];

function show(settings: AutofixSettings | undefined = settingsOf(), props: Partial<AutofixSettingsPageProps> = {}) {
  const actions = actionsOf();
  render(<AutofixSettingsPage load="ready" available missingSettings={[]} isAdmin settings={settings} githubRepos={{ load: 'ready', items: githubRepos, onRetry: vi.fn() }}
    timezone="UTC" now={now} actions={actions} onRetry={vi.fn()} checklist={() => []} onChecklist={vi.fn()} {...props} />);
  return actions;
}

describe('AutofixSettingsPage', () => {
  it('names the settings the operator still needs on a server without the GitHub App', () => {
    show(undefined, { available: false, missingSettings: ['GITHUB_APP_ID', 'GITHUB_APP_PRIVATE_KEY'] });
    expect(screen.getByRole('note')).toHaveTextContent('GITHUB_APP_ID, GITHUB_APP_PRIVATE_KEY');
    expect(screen.queryByRole('heading', { name: 'GitHub' })).not.toBeInTheDocument();
  });

  it('says “Only select repositories” before GitHub opens', async () => {
    const actions = show(settingsOf({ installations: [], repos: [] }));
    expect(screen.getByRole('heading', { name: 'Repositories' }).closest('section')).toHaveTextContent('After GitHub');
    fireEvent.click(screen.getByRole('button', { name: 'Connect GitHub' }));
    const dialog = screen.getByRole('dialog', { name: 'On GitHub, choose “Only select repositories”' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Continue to GitHub' }));
    await waitFor(() => expect(actions.connectGitHub).toHaveBeenCalled());
  });

  it('locks the repositories while the installation reaches all of them', () => {
    const settings = settingsOf();
    show({ ...settings, installations: [{ ...settings.installations[0], repository_selection: 'all', status: 'blocked_all_repositories' }] });
    expect(screen.getByText('Blocked: all repositories')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('It unlocks by itself');
    expect(screen.queryByRole('button', { name: 'Connect' })).not.toBeInTheDocument();
  });

  it('greys public and internal repositories with the reason, and connects a private one', async () => {
    const actions = show();
    expect(screen.getByText(/customer tickets would become public/)).toBeInTheDocument();
    expect(screen.getByText(/everyone in your GitHub enterprise/)).toBeInTheDocument();
    const mobile = screen.getByText('northwind/mobile', { exact: false }).closest('li')!;
    fireEvent.click(within(mobile).getByRole('button', { name: 'Connect' }));
    await waitFor(() => expect(actions.connectRepo).toHaveBeenCalledWith(expect.anything(), githubRepos[1]));
  });

  it('asks before letting notes go, and turns them off at once', async () => {
    const actions = show(settingsOf({ repos: [repoOf({ include_notes: false })] }));
    fireEvent.click(screen.getByRole('switch', { name: 'Private notes may be sent' }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Allow notes' }));
    await waitFor(() => expect(actions.setNotes).toHaveBeenCalledWith(expect.objectContaining({ id: repoOf().id }), true));
  });

  it('shows a paused repository with why', () => {
    show(settingsOf({ repos: [repoOf({ paused_reason: 'became_public', visibility: 'public' })] }));
    expect(screen.getByText(/Became public. Make it private on GitHub to resume. A fix not yet sent was cancelled; an issue already created stays/)).toBeInTheDocument();
  });

  it('turns an agent on only after the admin agrees to what leaves', async () => {
    const actions = show(settingsOf({ connectors: [connectorOf({ enabled: false, consent: null, last_check: null })] }));
    const card = screen.getByRole('article', { name: 'Claude' });
    const turnOn = within(card).getByRole('button', { name: 'Turn on Claude' });
    expect(turnOn).toBeDisabled();
    fireEvent.click(within(card).getByRole('checkbox', { name: /the reviewed title and text go into a GitHub issue/ }));
    fireEvent.click(turnOn);
    await waitFor(() => expect(actions.enable).toHaveBeenCalledWith('claude_issue'));
  });

  it('never shows a character of the Cursor key, only who set it and when', () => {
    show();
    const card = screen.getByRole('article', { name: 'Cursor' });
    expect(card).toHaveTextContent('Key set');
    expect(card).toHaveTextContent('Mai Tran');
    expect(within(card).queryByRole('textbox')).not.toBeInTheDocument();
  });

  it('asks for a new key when Cursor refused the old one', async () => {
    const actions = show(settingsOf({ connectors: [connectorOf({ kind: 'cursor_api', credential: { set_by_name: 'Mai Tran', set_at: '2026-10-14T10:00:00Z' },
      last_check: { at: '2026-10-27T11:02:00Z', result: 'key_invalid', message: null } })] }));
    const card = screen.getByRole('article', { name: 'Cursor' });
    expect(within(card).getByText('Key refused')).toBeInTheDocument();
    fireEvent.change(within(card).getByLabelText('New Cursor API key'), { target: { value: 'key_0123456789' } });
    fireEvent.click(within(card).getByRole('button', { name: 'Save key' }));
    await waitFor(() => expect(actions.setKey).toHaveBeenCalledWith('key_0123456789'));
  });

  it('keeps the vendor checklist as the admin’s own ticks', () => {
    const onChecklist = vi.fn();
    show(settingsOf({ connectors: [connectorOf({ enabled: false, consent: null })] }), { onChecklist });
    fireEvent.click(screen.getByRole('checkbox', { name: /Add the secret ANTHROPIC_API_KEY/ }));
    expect(onChecklist).toHaveBeenCalledWith('claude_issue', [false, true, false]);
    expect(screen.getAllByText(/kept in this browser only/).length).toBeGreaterThan(0);
  });

  it('lets someone who is not an admin read it, with no button that changes it', () => {
    show(settingsOf(), { isAdmin: false });
    expect(screen.getByText(/Only admins change Autofix settings/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Disconnect' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Turn off' })).not.toBeInTheDocument();
    expect(screen.getByRole('switch', { name: 'Private notes may be sent' })).toBeDisabled();
  });

  it('disconnects GitHub after saying what it cancels', async () => {
    const actions = show();
    fireEvent.click(screen.getAllByRole('button', { name: 'Disconnect' })[0]);
    const dialog = screen.getByRole('alertdialog', { name: 'Disconnect GitHub?' });
    expect(dialog).toHaveTextContent('Fixes in progress on them are cancelled');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Disconnect' }));
    await waitFor(() => expect(actions.disconnectGitHub).toHaveBeenCalled());
  });
});

describe('turning Cursor on', () => {
  it('stops when the key is refused, so Cursor is never on without a key', async () => {
    const actions = show(settingsOf({ connectors: [connectorOf({ kind: 'cursor_api', enabled: false, consent: null, credential: null, last_check: null })] }));
    vi.mocked(actions.setKey).mockResolvedValue(false);
    const card = screen.getByRole('article', { name: 'Cursor' });
    fireEvent.change(within(card).getByLabelText('Cursor API key'), { target: { value: 'key_0123456789' } });
    fireEvent.click(within(card).getByRole('checkbox', { name: /go to Cursor/ }));
    fireEvent.click(within(card).getByRole('button', { name: 'Save and turn on' }));
    await waitFor(() => expect(actions.setKey).toHaveBeenCalledWith('key_0123456789'));
    expect(actions.enable).not.toHaveBeenCalled();
  });
});

describe('the Cursor key while Cursor is off', () => {
  it('can still be replaced or removed, since it stays until removed', async () => {
    const actions = show(settingsOf({ connectors: [connectorOf({ kind: 'cursor_api', enabled: false, credential: { set_by_name: 'Mai Tran', set_at: '2026-10-14T10:00:00Z' } })] }));
    const card = screen.getByRole('article', { name: 'Cursor' });
    fireEvent.click(within(card).getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(actions.removeKey).toHaveBeenCalled());
    expect(within(card).getByRole('button', { name: 'Replace' })).toBeInTheDocument();
  });
});
