import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { permissionsOf, rolesOf } from '../../test-roles';
import { HelpCenterSettingsRoute } from './help-center-settings-page';
import { json, serve } from './kb.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', roles: rolesOf(role), timezone_override: null }, permissions: permissionsOf(role),
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC', token: null, plan: null,
});
const off = { enabled: false, title: '', locale: 'en', content_version: 3, updated_at: null, url: 'https://desk.example.com/help/acme' };

function open(role: 'admin' | 'agent', permissions = permissionsOf(role)) {
  serve({ 'GET /api/v1/auth/me': () => json(200, me(role)), 'GET /api/v1/help-center': () => json(200, off) });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <PermissionsContext.Provider value={permissions}><HelpCenterSettingsRoute /></PermissionsContext.Provider></QueryClientProvider>);
}

// The help center's settings were a section of Settings; they live in the Help center area now (#528, S12-x).
test('the knowledge base has a Settings tab holding the help center card', async () => {
  open('admin');
  const tabs = screen.getByRole('navigation', { name: 'Knowledge base sections' });
  expect(within(tabs).getByRole('link', { name: 'Settings' })).toHaveAttribute('href', '/knowledge/settings');
  expect(within(tabs).getByRole('link', { name: 'Settings' })).toHaveAttribute('aria-current', 'page');
  const card = screen.getByRole('region', { name: 'Help center' });
  expect(card).toHaveTextContent('The public pages of your published knowledge base articles.');
  expect(await within(card).findByRole('checkbox', { name: 'Publish the help center' })).toBeInTheDocument();
});

test('an agent reads the help center settings there without changing them', async () => {
  open('agent');
  expect(await screen.findByText('Only admins can turn the help center on or off.')).toBeInTheDocument();
  expect(screen.queryByRole('checkbox', { name: 'Publish the help center' })).not.toBeInTheDocument();
});

test('the Settings tab shows only to a role that reads the help center', () => {
  open('agent', permissionsOf('agent').filter((name) => name !== 'help_center:read'));
  expect(within(screen.getByRole('navigation', { name: 'Knowledge base sections' })).queryByRole('link', { name: 'Settings' })).not.toBeInTheDocument();
});
