import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { permissionsOf, rolesOf } from '../../test-roles';
import { HelpCenterSettingsRoute } from './help-center-settings-page';
import { hardwareCenter, json, primaryCenter, refusal, serve } from './kb.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', roles: rolesOf(role), timezone_override: null }, permissions: permissionsOf(role),
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC', token: null, plan: null,
});
const primarySettings = { enabled: true, title: '', locale: 'en', require_approval: false, content_version: 3, updated_at: null, show_tags: false, url: primaryCenter.url };

function open(role: 'admin' | 'agent', routes: Parameters<typeof serve>[0]) {
  const calls = serve({ 'GET /api/v1/auth/me': () => json(200, me(role)), 'GET /api/v1/help-center': () => json(200, primarySettings), ...routes });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <PermissionsContext.Provider value={permissionsOf(role)}><HelpCenterSettingsRoute /></PermissionsContext.Provider></QueryClientProvider>);
  return calls;
}

test('the help centers are listed, the primary first, each with its address and whether it is on', async () => {
  open('admin', { 'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter, hardwareCenter], limit: 20 }) });
  const list = await screen.findByRole('list', { name: 'Help centers' });
  const rows = within(list).getAllByRole('listitem');
  expect(rows).toHaveLength(2);
  expect(rows[0]).toHaveTextContent('Acme help');
  expect(rows[0]).toHaveTextContent('Primary');
  expect(rows[0]).toHaveTextContent('On');
  expect(within(rows[1]!).getByRole('link', { name: hardwareCenter.url })).toHaveAttribute('href', hardwareCenter.url);
  expect(rows[1]).toHaveTextContent('Hardware');
  expect(rows[1]).toHaveTextContent('Off');
  expect(within(rows[0]!).queryByRole('button', { name: /Delete/ })).not.toBeInTheDocument();
});

test('an admin adds a help center by name and handle, and it joins the list', async () => {
  let centers = [primaryCenter];
  const calls = open('admin', {
    'GET /api/v1/help-centers': () => json(200, { help_centers: centers, limit: 20 }),
    'POST /api/v1/help-centers': () => { centers = [primaryCenter, hardwareCenter]; return json(201, hardwareCenter); },
  });
  fireEvent.click(await screen.findByRole('button', { name: 'New help center' }));
  const form = screen.getByRole('form', { name: 'New help center' });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Hardware' } });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Handle' }), { target: { value: 'hardware' } });
  expect(within(form).getByText('https://desk.example.com/help/acme/hardware')).toBeInTheDocument();
  fireEvent.click(within(form).getByRole('button', { name: 'Create help center' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ name: 'Hardware', handle: 'hardware', locale: 'en' }));
  expect(await within(screen.getByRole('list', { name: 'Help centers' })).findByText('Hardware')).toBeInTheDocument();
  expect(screen.queryByRole('form', { name: 'New help center' })).not.toBeInTheDocument();
});

test('a create sent again after a failed answer goes under the same Idempotency-Key, so the server makes one', async () => {
  let answers = 0;
  const calls = open('admin', {
    'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter], limit: 20 }),
    'POST /api/v1/help-centers': () => (++answers === 1 ? refusal(503, 'unavailable', 'Try again.') : json(201, hardwareCenter)),
  });
  fireEvent.click(await screen.findByRole('button', { name: 'New help center' }));
  const form = screen.getByRole('form', { name: 'New help center' });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Hardware' } });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Handle' }), { target: { value: 'hardware' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Create help center' }));
  expect(await within(form).findByRole('alert')).toBeInTheDocument();
  fireEvent.click(within(form).getByRole('button', { name: 'Create help center' }));
  await waitFor(() => expect(screen.queryByRole('form', { name: 'New help center' })).not.toBeInTheDocument());
  const keys = calls.filter((call) => call.method === 'POST').map((call) => call.headers['Idempotency-Key']);
  expect(keys).toHaveLength(2);
  expect(keys[0]).toMatch(/^.{16,}$/);
  expect(keys[1]).toBe(keys[0]);
});

test('a handle the server refuses says why and keeps the form', async () => {
  const reason = 'Another help center of this workspace already uses the handle hardware.';
  open('admin', {
    'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter, hardwareCenter], limit: 20 }),
    'POST /api/v1/help-centers': () => refusal(409, 'duplicate_handle', reason),
  });
  fireEvent.click(await screen.findByRole('button', { name: 'New help center' }));
  const form = screen.getByRole('form', { name: 'New help center' });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Devices' } });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Handle' }), { target: { value: 'hardware' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Create help center' }));
  expect(await within(form).findByRole('alert')).toHaveTextContent(reason);
  expect(within(form).getByRole('textbox', { name: 'Handle' })).toHaveValue('hardware');
});

test('deleting a help center asks first, says it cannot be undone, and takes it off the list', async () => {
  let centers = [primaryCenter, hardwareCenter];
  const calls = open('admin', {
    'GET /api/v1/help-centers': () => json(200, { help_centers: centers, limit: 20 }),
    [`DELETE /api/v1/help-centers/${hardwareCenter.id}`]: () => { centers = [primaryCenter]; return new Response(null, { status: 204 }); },
  });
  fireEvent.click(await screen.findByRole('button', { name: 'Delete Hardware' }));
  const confirm = screen.getByRole('group', { name: 'Delete Hardware' });
  expect(confirm).toHaveTextContent('cannot be undone');
  expect(calls.some((call) => call.method === 'DELETE')).toBe(false);
  fireEvent.click(within(confirm).getByRole('button', { name: 'Delete help center' }));
  await waitFor(() => expect(within(screen.getByRole('list', { name: 'Help centers' })).getAllByRole('listitem')).toHaveLength(1));
  expect(calls.filter((call) => call.method === 'DELETE').map((call) => call.path)).toEqual([`/api/v1/help-centers/${hardwareCenter.id}`]);
});

test('an admin turns another help center on from its row; the primary is turned on in its settings below', async () => {
  let centers = [primaryCenter, hardwareCenter];
  const calls = open('admin', {
    'GET /api/v1/help-centers': () => json(200, { help_centers: centers, limit: 20 }),
    [`PATCH /api/v1/help-centers/${hardwareCenter.id}`]: () => { centers = [primaryCenter, { ...hardwareCenter, enabled: true }]; return json(200, centers[1]); },
  });
  const list = await screen.findByRole('list', { name: 'Help centers' });
  expect(within(list).queryByRole('button', { name: 'Turn off Acme help' })).not.toBeInTheDocument();
  fireEvent.click(within(list).getByRole('button', { name: 'Turn on Hardware' }));
  expect(await within(list).findByRole('button', { name: 'Turn off Hardware' })).toBeInTheDocument();
  expect(calls.filter((call) => call.method === 'PATCH').map((call) => [call.path, call.body])).toEqual([[`/api/v1/help-centers/${hardwareCenter.id}`, { enabled: true }]]);
});

test('a refused turn on says why on the row', async () => {
  open('admin', {
    'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter, hardwareCenter], limit: 20 }),
    [`PATCH /api/v1/help-centers/${hardwareCenter.id}`]: () => refusal(403, 'permission_missing', 'Turning a help center on needs kb_articles:publish.'),
  });
  const list = await screen.findByRole('list', { name: 'Help centers' });
  fireEvent.click(within(list).getByRole('button', { name: 'Turn on Hardware' }));
  expect(await within(list).findByRole('alert')).toHaveTextContent('needs kb_articles:publish');
});

test('at the limit there is no New help center, and the page says how to add one', async () => {
  open('admin', { 'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter, hardwareCenter], limit: 2 }) });
  expect(await screen.findByText('At most 2 help centers. Delete one to add another.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'New help center' })).not.toBeInTheDocument();
});

test('an agent reads the list without adding or deleting', async () => {
  open('agent', { 'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter, hardwareCenter], limit: 20 }) });
  expect(await screen.findByRole('list', { name: 'Help centers' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'New help center' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Delete Hardware' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Turn on Hardware' })).not.toBeInTheDocument();
});
