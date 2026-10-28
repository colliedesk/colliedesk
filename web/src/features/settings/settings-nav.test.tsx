import { render } from '@testing-library/react';
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
