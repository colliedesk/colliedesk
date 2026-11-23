import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { HelpCenterSettings } from './help-center-card';
import { json, serve } from './kb.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const off = { enabled: false, title: '', locale: 'pt-BR', require_approval: false, content_version: 3, updated_at: null, show_tags: false, feedback_conversations: false, url: 'https://desk.example.com/help/acme' };

function open(canManage: boolean) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><HelpCenterSettings canManage={canManage} workspaceName="Acme" /></QueryClientProvider>);
}

test('an admin turns the help center on with a title; the stored language is sent back unchanged', async () => {
  const calls = serve({ 'GET /api/v1/help-center': () => json(200, off), 'PUT /api/v1/help-center': (call) => json(200, { ...off, ...(call.body as object), content_version: 4 }) });
  open(true);
  expect(await screen.findByText(/The help center is off/)).toHaveAttribute('role', 'status');
  fireEvent.click(screen.getByRole('checkbox', { name: 'Publish the help center' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Title' }), { target: { value: ' Acme help ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save help center' }));
  await waitFor(() => expect(screen.getAllByRole('status')[0]).toHaveTextContent('The help center is on'));
  expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ enabled: true, title: 'Acme help', locale: 'pt-BR' });
});

test('an agent sees whether it is on but cannot change it', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, { ...off, enabled: true }) });
  open(false);
  expect(await screen.findByText(/The help center is on/)).toHaveAttribute('role', 'status');
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
  expect(screen.getByText('Only admins can turn the help center on or off.')).toBeInTheDocument();
});

test('everyone sees the public address as a link, also while the help center is off', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, off) });
  open(false);
  const link = await screen.findByRole('link', { name: 'https://desk.example.com/help/acme' });
  expect(link).toHaveAttribute('href', 'https://desk.example.com/help/acme');
  expect(link).toHaveAttribute('rel', 'noopener noreferrer');
});

test('the help center settings opened at #help-center-heading (the reply box links there) scroll to the card once it is there', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, off) });
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  window.history.replaceState(null, '', '/knowledge/settings#help-center-heading');
  try {
    render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <h2 id="help-center-heading">Help center</h2><HelpCenterSettings canManage workspaceName="Acme" /></QueryClientProvider>);
    await screen.findByText(/The help center is off/);
    await waitFor(() => expect(scrolled).toHaveBeenCalledOnce());
    expect(scrolled.mock.contexts[0]).toBe(screen.getByRole('heading', { name: 'Help center' }));
  } finally {
    window.history.replaceState(null, '', '/');
    delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView;
  }
});

test('an admin who may publish turns on approval before publishing in one click; the rest of the settings are sent back unchanged', async () => {
  const calls = serve({ 'GET /api/v1/help-center': () => json(200, off), 'PUT /api/v1/help-center': (call) => json(200, { ...off, ...(call.body as object) }) });
  open(true);
  const box = await screen.findByRole('checkbox', { name: 'Require approval before publishing' });
  expect(box).not.toBeChecked();
  expect(box).toHaveAccessibleDescription('A teammate who may publish approves each new or changed article before it goes on the help center. Collections can override it.');
  fireEvent.click(box);
  await waitFor(() => expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ enabled: false, title: '', locale: 'pt-BR', require_approval: true }));
  await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Require approval before publishing' })).toBeChecked());
});

test('an admin who may not publish sees whether approval is required but cannot change it', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, { ...off, require_approval: true }) });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <PermissionsContext.Provider value={['help_center:read', 'help_center:update']}><HelpCenterSettings canManage workspaceName="Acme" /></PermissionsContext.Provider></QueryClientProvider>);
  expect(await screen.findByText('Approval before publishing: required.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'About Approval before publishing' })).toHaveAccessibleDescription('Only a role that may publish articles changes it.');
  expect(screen.queryByRole('checkbox', { name: 'Require approval before publishing' })).not.toBeInTheDocument();
});

test('the title says where it shows and what an empty one uses, in one line', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, off) });
  open(true);
  expect(await screen.findByText('Shown on top of every page. Empty uses the workspace name.')).toBeInTheDocument();
});

test('an admin shows article tags to readers with one click; only show_tags is sent with the settings as they are', async () => {
  const calls = serve({ 'GET /api/v1/help-center': () => json(200, off), 'PUT /api/v1/help-center': (call) => json(200, { ...off, ...(call.body as object) }) });
  open(true);
  const box = await screen.findByRole('checkbox', { name: 'Show tags on article pages' });
  expect(box).not.toBeChecked();
  fireEvent.click(box);
  await waitFor(() => expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ enabled: false, title: '', locale: 'pt-BR', show_tags: true }));
  await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Show tags on article pages' })).toBeChecked());
  fireEvent.click(screen.getByRole('checkbox', { name: 'Show tags on article pages' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PUT').at(-1)?.body).toEqual({ enabled: false, title: '', locale: 'pt-BR', show_tags: false }));
});

test('the tags switch keeps one short line and explains the rest in its tip', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, off) });
  open(true);
  const box = await screen.findByRole('checkbox', { name: 'Show tags on article pages' });
  expect(box).toHaveAccessibleDescription('Readers see an article\'s tags under its title. Search on the help center finds articles by their tags either way.');
  expect(screen.getByRole('button', { name: 'About Show tags on article pages' })).toBeInTheDocument();
  expect(screen.queryByText('Readers see an article\'s tags under its title. Search on the help center finds articles by their tags either way.', { selector: 'small' })).not.toBeInTheDocument();
});

test('an agent reads whether readers see tags', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, { ...off, show_tags: true }) });
  open(false);
  expect(await screen.findByText('Article pages show their tags to readers.')).toBeInTheDocument();
});

test('an admin who may change channels turns a "No" vote with an address into a conversation; only feedback_conversations is sent with the settings as they are', async () => {
  const calls = serve({ 'GET /api/v1/help-center': () => json(200, off), 'PUT /api/v1/help-center': (call) => json(200, { ...off, ...(call.body as object) }) });
  open(true);
  const box = await screen.findByRole('checkbox', { name: 'Open a conversation from a "No" vote' });
  expect(box).not.toBeChecked();
  expect(box).toHaveAccessibleDescription('A reader who answers No, writes what was missing and leaves an email address opens a conversation, and the article\'s author is asked on it. Replies go to that address.');
  fireEvent.click(box);
  await waitFor(() => expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ enabled: false, title: '', locale: 'pt-BR', feedback_conversations: true }));
  await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Open a conversation from a "No" vote' })).toBeChecked());
});

test('an admin who may not change channels reads whether a "No" vote opens a conversation but cannot change it', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, { ...off, feedback_conversations: true }) });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <PermissionsContext.Provider value={['help_center:read', 'help_center:update']}><HelpCenterSettings canManage workspaceName="Acme" /></PermissionsContext.Provider></QueryClientProvider>);
  expect(await screen.findByText('A "No" vote that leaves an address opens a conversation.')).toBeInTheDocument();
  expect(screen.queryByRole('checkbox', { name: 'Open a conversation from a "No" vote' })).not.toBeInTheDocument();
});
