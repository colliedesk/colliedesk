import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { KnowledgeRoute } from './knowledge-page';
import type { KnowledgeSearch } from './knowledge-page';
import { collection, collections, hardwareCenter, ids, json, me, primaryCenter, serve, summary } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const base = (centers = [primaryCenter, hardwareCenter]) => ({
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  'GET /api/v1/kb/articles': () => json(200, { items: [summary(1, 'Reset your password')], next_cursor: null }),
  'GET /api/v1/help-center': () => json(200, { enabled: true, title: '', locale: 'en', require_approval: false, content_version: 1, updated_at: null, show_tags: false, url: primaryCenter.url }),
  'GET /api/v1/help-centers': () => json(200, { help_centers: centers, limit: 20 }),
});
const guides = collections.find((item) => item.id === ids.guides)!;

function open(search: KnowledgeSearch) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><KnowledgeRoute search={search} /></QueryClientProvider>);
}

test('a top-level collection chooses the help centers it is shown on, and sends only a changed choice', async () => {
  const calls = serve({ ...base(), [`PATCH /api/v1/kb/collections/${ids.guides}`]: () => json(200, { ...guides, help_center_ids: [primaryCenter.id, hardwareCenter.id] }) });
  open({ collection: ids.guides });
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Guides' }));
  const shownOn = await within(screen.getByRole('form', { name: 'Edit Guides' })).findByRole('group', { name: 'Shown on' });
  const primary = within(shownOn).getByRole('checkbox', { name: 'Acme help' });
  const hardware = within(shownOn).getByRole('checkbox', { name: 'Hardware' });
  expect(primary).toBeChecked();
  expect(primary).toBeDisabled();
  expect(hardware).not.toBeChecked();
  fireEvent.click(hardware);
  expect(primary).toBeEnabled();
  fireEvent.click(screen.getByRole('button', { name: 'Save collection' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: guides.updated_at, help_center_ids: [primaryCenter.id, hardwareCenter.id] }));
});

test('a collection inside another is shown where its top-level collection is, so it has no choice', async () => {
  serve(base());
  open({ collection: ids.setup });
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Setup' }));
  const form = screen.getByRole('form', { name: 'Edit Setup' });
  expect(await within(form).findByText('Shown where its top-level collection is.')).toBeInTheDocument();
  expect(within(form).queryByRole('group', { name: 'Shown on' })).not.toBeInTheDocument();
});

test('with only the primary help center there is nothing to choose', async () => {
  serve(base([primaryCenter]));
  open({ collection: ids.guides });
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Guides' }));
  await screen.findByRole('form', { name: 'Edit Guides' });
  await waitFor(() => expect(screen.queryByRole('group', { name: 'Shown on' })).not.toBeInTheDocument());
  expect(screen.queryByText('Shown where its top-level collection is.')).not.toBeInTheDocument();
});

test('a new top-level collection starts on the primary and can go on another help center instead', async () => {
  const calls = serve({ ...base(), 'POST /api/v1/kb/collections': () => json(201, { ...collection(ids.billing, 'Routers', null), help_center_ids: [hardwareCenter.id] }) });
  open({});
  fireEvent.click((await screen.findAllByRole('button', { name: 'New collection' }))[0]!);
  const form = screen.getByRole('form', { name: 'New collection' });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Routers' } });
  const shownOn = await within(form).findByRole('group', { name: 'Shown on' });
  expect(within(shownOn).getByRole('checkbox', { name: 'Acme help' })).toBeChecked();
  fireEvent.click(within(shownOn).getByRole('checkbox', { name: 'Hardware' }));
  fireEvent.click(within(shownOn).getByRole('checkbox', { name: 'Acme help' }));
  fireEvent.click(within(form).getByRole('button', { name: 'Create collection' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ name: 'Routers', parent_id: null, help_center_ids: [hardwareCenter.id] }));
});
