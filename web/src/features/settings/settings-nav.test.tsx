import { render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, test, vi } from 'vitest';
import { SettingsNav } from './settings-nav';
import type { Shown } from './settings-nav';

afterEach(() => { delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView; });

// On a narrow screen the nav is one sideways row; the page's own link is brought into it, also when the
// links before it arrive later (the workspace sections show once who is signed in is known).
test('the current page is brought into the row again when links appear before it', () => {
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  const few: Shown = { general: false, team: false, channels: true, savedReplies: true, exportData: false, billing: false };
  const { rerender } = render(<SettingsNav page="/settings/channels" shown={few} />);
  const calls = scrolled.mock.calls.length;
  rerender(<SettingsNav page="/settings/channels" shown={{ ...few, general: true, team: true, ai: true }} />);
  expect(scrolled.mock.calls.length).toBeGreaterThan(calls);
  expect(scrolled.mock.contexts.at(-1)).toHaveTextContent('Channels');
});

// Email, Web form and Help chat are named places under Channels: Settings → Email is one click from any settings page.
test('Channels lists Email, Web form and Help chat under it, each opening its type home', () => {
  const shown: Shown = { general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false };
  render(<SettingsNav page="/settings/channels/type/email" shown={shown} />);
  const channels = screen.getByRole('link', { name: 'Channels' });
  expect(channels).toHaveAttribute('href', '/settings/channels');
  const group = screen.getByRole('list', { name: 'Channels' });
  expect(within(group).getAllByRole('link').map((link) => [link.textContent, link.getAttribute('href')])).toEqual([
    ['Email', '/settings/channels/type/email'], ['Web form', '/settings/channels/type/web_form'], ['Help chat', '/settings/channels/type/help_chat'], ['Zalo OA', '/settings/channels/type/zalo'],
  ]);
  expect(within(group).getByRole('link', { name: 'Email' })).toHaveAttribute('aria-current', 'page');
  expect(channels).not.toHaveAttribute('aria-current');
});

test('the channel list itself stays the current page at /settings/channels', () => {
  render(<SettingsNav page="/settings/channels" shown={{ general: false, team: false, channels: true, savedReplies: true, exportData: false, billing: false }} />);
  expect(screen.getByRole('link', { name: 'Channels' })).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('link', { name: 'Email' })).not.toHaveAttribute('aria-current');
});

// A type the server does not offer this person (Zalo OA outside Vietnam) and their workspace does not
// have is not a place under Channels; once offered, or once the workspace has one, it is.
test('Channels leaves out a type the server does not offer and the workspace does not have', async () => {
  const shown: Shown = { general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false };
  const counts = (available: boolean, count: number) => ({ types: [
    { type: 'email', available: true, count: 1 }, { type: 'web_form', available: true, count: 0 },
    { type: 'help_chat', available: true, count: 0 }, { type: 'zalo', available, count } ] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(['channels', 'counts'], counts(false, 0));
  const { rerender } = render(<QueryClientProvider client={client}><SettingsNav page="/settings/channels" shown={shown} /></QueryClientProvider>);
  const group = () => screen.getByRole('list', { name: 'Channels' });
  expect(within(group()).getAllByRole('link').map((link) => link.textContent)).toEqual(['Email', 'Web form', 'Help chat']);

  client.setQueryData(['channels', 'counts'], counts(true, 1));
  rerender(<QueryClientProvider client={client}><SettingsNav page="/settings/channels" shown={shown} /></QueryClientProvider>);
  expect(await within(group()).findByRole('link', { name: 'Zalo OA' })).toHaveAttribute('href', '/settings/channels/type/zalo');
});
