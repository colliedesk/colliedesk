import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render as renderPlain, screen, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { expect, test, vi } from 'vitest';
import type { Role, TeamMember, WebFormSettings } from '../../api/schemas';
import { builtInRoles, permissionsOf, rolesOf } from '../../test-roles';
import { ExportAllData } from './export-all-data';
import { RolesSection } from './roles/role-list';
import { SavedReplyTrash } from './saved-reply-panels';
import { SavedRepliesSection } from './saved-replies-section';
import { SessionsSection } from './sessions-section';
import { SettingsEditsProvider } from './settings-edits';
import { SettingsPage } from './settings-page';
import { TeamSection } from './team-section';
import { WebFormSection } from './web-form-section';

// "Quiet by default": each Settings section keeps its heading and one line of 60 characters or less. What the line
// used to say beyond that is the accessible description of the tip next to it.

const render = (ui: ReactElement) => renderPlain(ui, { wrapper: SettingsEditsProvider });
function line(text: string) {
  expect(text.length).toBeLessThanOrEqual(60);
  return screen.getByText(text);
}
const tip = (label: string) => screen.getByRole('button', { name: `About ${label}` });

test('General says what it holds in one line, and the tip says who the default timezone is for', () => {
  render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace workspaceName="Acme" workspaceTimezone="UTC" />);
  line('The workspace name and default timezone.');
  expect(tip('General')).toHaveAccessibleDescription('The default timezone applies to everyone who has not chosen their own.');
});

test('the workspace name says where the team sees it, and the tip says what customers and replies show', () => {
  render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace workspaceName="Acme" workspaceTimezone="UTC" />);
  const input = screen.getByRole('textbox', { name: 'Workspace name' });
  expect(input).toHaveAccessibleDescription(/^Your team sees this name in Colliedesk\./);
  line('Your team sees this name in Colliedesk.');
  expect(input).toHaveAccessibleDescription(/Customers see it as the help center title while the help center has no title of its own\. Replies use each email channel's sender name, not this name\.$/);
});

const member = (overrides: Partial<TeamMember> & Pick<TeamMember, 'id' | 'email'>): TeamMember => ({
  display_name: '', roles: rolesOf('agent'), manageable: true, timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0, ...overrides,
});

test('Team keeps one line about who is listed, and the tip says what a deactivated person is', () => {
  const ada = member({ id: '11111111-1111-4111-8111-111111111111', email: 'ada@example.com', roles: rolesOf('admin') });
  render(<TeamSection members={[ada]} currentAgentId={ada.id} canManage={false} timezone="UTC" />);
  line('Everyone who can sign in to this workspace.');
  expect(tip('Team')).toHaveAccessibleDescription('A deactivated person cannot sign in and does not use a seat.');
});

test('Invites keeps the one-time link in one line, and the tip says who gets which roles', () => {
  render(<SettingsPage page="members" membersTab="invites" sessions={[]} invites={[]} timezone="UTC" canCreateInvites inviteRoles={builtInRoles} />);
  line('A one-time sign-in link for a new teammate.');
  expect(tip('Invites')).toHaveAccessibleDescription('The person joins with the roles you choose; someone already in the workspace gets them added to theirs.');
});

test('Sessions keeps one line, and the tip says what revoking does', () => {
  render(<SessionsSection sessions={[]} timezone="UTC" />);
  line('Browsers and clients signed in.');
  expect(tip('Sessions')).toHaveAccessibleDescription('Revoke one you no longer use; on this device that signs you out.');
});

test('Web form keeps one line, and the tip says the visitor gets no email', () => {
  const settings: WebFormSettings = { enabled: false, allowed_origins: [], submit_path: '/api/v1/public/forms/acme' };
  render(<WebFormSection settings={settings} canManage onSave={vi.fn()} />);
  line('Let a form on your website open a conversation.');
  expect(tip('Web form')).toHaveAccessibleDescription('The visitor gets no email from Colliedesk.');
});

test('Saved replies keeps one line, and the tip names the two variables', () => {
  render(<SavedRepliesSection replies={[]} onCreate={vi.fn()} onUpdate={vi.fn()} onDelete={vi.fn()} onReload={vi.fn()} loadHistory={vi.fn()} onRestoreVersion={vi.fn()}
    loadTrash={vi.fn()} onUndelete={vi.fn()} />);
  line('Anyone can add, edit or delete them.');
  expect(tip('Saved replies')).toHaveAccessibleDescription('{{customer.name}} and {{agent.name}} are filled in when a reply is inserted.');
});

test('the saved reply trash keeps one line, and the tip says what happens after 30 days', () => {
  render(<SavedReplyTrash timezone="UTC" now={new Date('2026-11-14T00:00:00Z')} load={async () => ({ items: [], next_cursor: null })} onUndelete={vi.fn()} />);
  line('Deleted saved replies stay here for 30 days.');
  expect(tip('Trash')).toHaveAccessibleDescription('Then they are deleted for good with their history.');
});

test('Export says the download started in one line, and the tip says who saves the file', async () => {
  const api = await import('../../api/client');
  vi.spyOn(api.api, 'requestExportLink').mockResolvedValue('/api/v1/export/download/abc');
  Object.defineProperty(window, 'location', { configurable: true, value: { assign: vi.fn() } });
  renderPlain(<QueryClientProvider client={new QueryClient()}><ExportAllData /></QueryClientProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  await screen.findByRole('status');
  line('Your download is starting. Keep this page open.');
  expect(tip('Download')).toHaveAccessibleDescription('The file is saved by your browser; keep this page open until it finishes.');
});

function rolesPage() {
  const me = { agent: { id: '11111111-1111-4111-8111-111111111111', email: 'ada@example.com', display_name: 'Ada', roles: rolesOf('admin'), timezone_override: null },
    permissions: permissionsOf('admin'), workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC', token: null, plan: null };
  return { me, roles: builtInRoles as Role[] };
}

test('with only the built-in roles, one line says what a role is for and the tip gives the example', async () => {
  const { me, roles } = rolesPage();
  const api = await import('../../api/client');
  vi.spyOn(api.api, 'me').mockResolvedValue(me as never);
  vi.spyOn(api.api, 'roles').mockImplementation((deleted?: boolean) => Promise.resolve({ items: deleted ? [] : roles }) as never);
  renderPlain(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><RolesSection /></QueryClientProvider>);
  await screen.findByText('Make a role for a part of the job.');
  line('Make a role for a part of the job.');
  expect(within(screen.getByText('Make a role for a part of the job.').closest('p')!).getByRole('link', { name: 'Make a role' })).toHaveAttribute('href', '/settings/roles/new');
  expect(tip('Make a role')).toHaveAccessibleDescription('For example Knowledge base editor, then give it to people in Team.');
});
