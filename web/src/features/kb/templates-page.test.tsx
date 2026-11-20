import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { TemplatesRoute } from './templates-page';
import { change, json, me, refusal, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const howTo = {
  id: '99999999-9999-4999-8999-000000000001', name: 'How-to', description: 'Steps to get one thing done', body: '## Before you start\n\n## Steps\n',
  created_at: '2026-10-02T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', last_change: change,
};
const faq = { ...howTo, id: '99999999-9999-4999-8999-000000000002', name: 'FAQ', description: 'Short answers', body: '## Question\n' };

function open() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><TemplatesRoute /></QueryClientProvider>);
}

const base = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/templates': () => json(200, { items: [howTo, faq] }),
};

test('the templates are listed with when to use them, and a new one is saved once with its key', async () => {
  const added = { ...howTo, id: '99999999-9999-4999-8999-000000000003', name: 'Release note', description: 'What changed', body: '## What changed\n' };
  const calls = serve({ ...base, 'POST /api/v1/kb/templates': () => json(201, added) });
  open();
  const list = await screen.findByRole('list', { name: 'Article templates' });
  expect(within(list).getByText('How-to')).toBeInTheDocument();
  expect(within(list).getByText('Steps to get one thing done')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Templates' })).toHaveAttribute('aria-current', 'page');

  fireEvent.click(screen.getByRole('button', { name: 'New template' }));
  const form = screen.getByRole('form', { name: 'Add a template' });
  fireEvent.click(within(form).getByRole('button', { name: 'Save template' }));
  expect(await within(form).findByRole('alert')).toHaveTextContent('Give the template a name and some text.');
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Release note' } });
  fireEvent.change(within(form).getByRole('textbox', { name: 'When to use it' }), { target: { value: 'What changed' } });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: '## What changed\n' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save template' }));
  await waitFor(() => expect(screen.queryByRole('form', { name: 'Add a template' })).not.toBeInTheDocument());
  const posted = calls.filter((call) => call.method === 'POST');
  expect(posted).toHaveLength(1);
  expect(posted[0]!.body).toEqual({ name: 'Release note', description: 'What changed', body: '## What changed\n' });
  expect(posted[0]!.headers['Idempotency-Key']).toMatch(/.+/);
});

test('an edit sends the version it was opened on, and a refusal keeps the form with the reason', async () => {
  let refuse = true;
  const calls = serve({ ...base, 'PATCH /api/v1/kb/templates/99999999-9999-4999-8999-000000000001': () => (refuse
    ? refusal(409, 'kb_template_name_taken', 'A template named FAQ already exists.') : json(200, { ...howTo, name: 'Step by step' })) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Edit How-to' }));
  const form = screen.getByRole('form', { name: 'Edit How-to' });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'FAQ' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save template' }));
  expect(await within(form).findByRole('alert')).toHaveTextContent('A template named FAQ already exists.');
  refuse = false;
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Step by step' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save template' }));
  await waitFor(() => expect(screen.queryByRole('form', { name: 'Edit How-to' })).not.toBeInTheDocument());
  expect(calls.filter((call) => call.method === 'PATCH').at(-1)!.body).toEqual({
    name: 'Step by step', description: 'Steps to get one thing done', body: '## Before you start\n\n## Steps\n', updated_at: '2026-10-02T00:00:00Z',
  });
});

test('a deleted template goes to the trash after a confirm, and comes back from it', async () => {
  const trashed = { ...faq, deleted_at: '2026-10-03T00:00:00Z', deleted_by: { agent_id: null, name: 'Ada' }, purges_at: '2026-11-02T00:00:00Z' };
  const calls = serve({
    ...base,
    'DELETE /api/v1/kb/templates/99999999-9999-4999-8999-000000000002': () => json(204),
    'GET /api/v1/kb/templates/trash': () => json(200, { items: [trashed], next_cursor: null }),
    'POST /api/v1/kb/templates/99999999-9999-4999-8999-000000000002/restore': () => json(200, faq),
  });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Delete FAQ' }));
  const confirm = screen.getByRole('group', { name: 'Delete FAQ' });
  expect(confirm).toHaveTextContent('Collections that start from it go back to blank.');
  fireEvent.click(within(confirm).getByRole('button', { name: 'Delete' }));
  await waitFor(() => expect(calls.some((call) => call.method === 'DELETE' && call.search.get('updated_at') === '2026-10-02T00:00:00Z')).toBe(true));

  fireEvent.click(screen.getByRole('button', { name: 'Template trash' }));
  const trash = await screen.findByRole('region', { name: 'Template trash' });
  fireEvent.click(await within(trash).findByRole('button', { name: /FAQ/ }));
  await waitFor(() => expect(calls.some((call) => call.path.endsWith('/restore'))).toBe(true));
  const restore = calls.find((call) => call.path.endsWith('/restore'))!;
  expect(restore.body).toEqual({ updated_at: '2026-10-02T00:00:00Z' });
  expect(restore.headers['Idempotency-Key']).toMatch(/.+/);
});

test('the history of a template lists its versions and restores an older one', async () => {
  const version = (n: number, kind: string, name: string) => ({ ...change, version: n, kind, name, description: '', body: `text ${n}` });
  const calls = serve({
    ...base,
    'GET /api/v1/kb/templates/99999999-9999-4999-8999-000000000001/versions': () => json(200, { items: [version(2, 'edit', 'How-to'), version(1, 'create', 'Steps')], next_before_version: null }),
    'POST /api/v1/kb/templates/99999999-9999-4999-8999-000000000001/versions/1/restore': () => json(200, howTo),
  });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'History of How-to' }));
  const history = await screen.findByRole('region', { name: 'History of How-to' });
  fireEvent.click(await within(history).findByRole('button', { name: /1/ }));
  await waitFor(() => expect(calls.some((call) => call.path.endsWith('/versions/1/restore'))).toBe(true));
  const restore = calls.find((call) => call.path.endsWith('/versions/1/restore'))!;
  expect(restore.body).toEqual({ updated_at: '2026-10-02T00:00:00Z' });
  expect(restore.headers['Idempotency-Key']).toMatch(/.+/);
});
