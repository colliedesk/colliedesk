import { render, screen, within } from '@testing-library/react';
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

// Email and Web form are named places under Channels: Settings → Email is one click from any settings page.
test('Channels lists Email and Web form under it, each opening its type home', () => {
  const shown: Shown = { general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false };
  render(<SettingsNav page="/settings/channels/type/email" shown={shown} />);
  const channels = screen.getByRole('link', { name: 'Channels' });
  expect(channels).toHaveAttribute('href', '/settings/channels');
  const group = screen.getByRole('list', { name: 'Channels' });
  expect(within(group).getAllByRole('link').map((link) => [link.textContent, link.getAttribute('href')])).toEqual([
    ['Email', '/settings/channels/type/email'], ['Web form', '/settings/channels/type/web_form'],
  ]);
  expect(within(group).getByRole('link', { name: 'Email' })).toHaveAttribute('aria-current', 'page');
  expect(channels).not.toHaveAttribute('aria-current');
});

test('the channel list itself stays the current page at /settings/channels', () => {
  render(<SettingsNav page="/settings/channels" shown={{ general: false, team: false, channels: true, savedReplies: true, exportData: false, billing: false }} />);
  expect(screen.getByRole('link', { name: 'Channels' })).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('link', { name: 'Email' })).not.toHaveAttribute('aria-current');
});
