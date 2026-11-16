import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { adminRoleRef, permissionsOf } from '../../test-roles';
import { TagsRoute } from './tags-route';

const mocks = vi.hoisted(() => ({
  api: { me: vi.fn(), config: vi.fn(), tagsWithCounts: vi.fn(), renameTag: vi.fn(), deleteTag: vi.fn() },
}));
// The settings frame asks for the router to guard against leaving unsaved text; there is none here.
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn(), useRouter: () => undefined, useBlocker: () => undefined }));
vi.mock('../../api/client', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../api/client')>()), api: mocks.api }));

const billing = { id: '55555555-5555-4555-8555-555555555555', name: 'billing', color: '#64748b', conversation_count: 3 };
const vip = { id: '66666666-6666-4666-8666-666666666666', name: 'vip', color: '#64748b', conversation_count: 1 };

function setUp(permissions: string[]) {
  vi.clearAllMocks();
  mocks.api.me.mockResolvedValue({ agent: { id: '11111111-1111-4111-8111-111111111111', email: 'ada@example.com', display_name: 'Ada', roles: [adminRoleRef], timezone_override: null },
    permissions, workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC', token: null, plan: null });
  mocks.api.config.mockResolvedValue({ billing: { enabled: false } });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><TagsRoute /></QueryClientProvider>);
}

test('an admin deletes a tag and the list loads again without it', async () => {
  mocks.api.tagsWithCounts.mockResolvedValueOnce({ items: [billing, vip] }).mockResolvedValue({ items: [vip] });
  mocks.api.deleteTag.mockResolvedValue(undefined);
  setUp(permissionsOf('admin'));
  expect(await screen.findByRole('heading', { level: 1, name: 'Tags' })).toBeInTheDocument();
  fireEvent.click(await screen.findByRole('button', { name: 'Delete billing' }));
  fireEvent.click(within(screen.getByRole('group', { name: 'Delete billing' })).getByRole('button', { name: 'Delete tag' }));
  await vi.waitFor(() => expect(mocks.api.deleteTag).toHaveBeenCalledWith(billing.id));
  await vi.waitFor(() => expect(screen.queryByText('billing', { selector: 'strong' })).not.toBeInTheDocument());
  expect(screen.getByText('vip', { selector: 'strong' })).toBeInTheDocument();
});

test('a role that only reads tags sees them with their counts but cannot rename or delete them', async () => {
  mocks.api.tagsWithCounts.mockResolvedValue({ items: [billing, vip] });
  setUp(permissionsOf('agent').filter((name) => name !== 'tags:update' && name !== 'tags:delete'));
  expect(await screen.findByText('3 conversations')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Rename billing' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Delete billing' })).not.toBeInTheDocument();
});

test('a list that could not load says so and loads again on Try again', async () => {
  mocks.api.tagsWithCounts.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ items: [vip] });
  setUp(permissionsOf('admin'));
  expect(await screen.findByRole('alert')).toHaveTextContent('The tags could not be loaded.');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(await screen.findByText('vip', { selector: 'strong' })).toBeInTheDocument();
});

test('a failed rename reads the list again, and a failed refresh keeps the list on screen', async () => {
  mocks.api.tagsWithCounts.mockResolvedValueOnce({ items: [billing, vip] }).mockRejectedValue(new Error('offline'));
  mocks.api.renameTag.mockRejectedValue(new Error('API 404'));
  setUp(permissionsOf('admin'));
  fireEvent.click(await screen.findByRole('button', { name: 'Rename vip' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'New name for vip' }), { target: { value: 'VIP' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await vi.waitFor(() => expect(mocks.api.tagsWithCounts).toHaveBeenCalledTimes(2));
  await vi.waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('“vip” was deleted by someone else.'));
  expect(screen.getByText('billing', { selector: 'strong' })).toBeInTheDocument();
});
