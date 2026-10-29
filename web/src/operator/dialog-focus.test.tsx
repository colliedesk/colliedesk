import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { ConsoleData } from './app';
import type { Setting } from './api';
import { Review } from './config-page';

// UI review risk a: a console dialog takes the focus when it opens, so Escape closes it at once and Tab stays inside.
const setting = (key: string, extra: Partial<Setting> = {}): Setting => ({ key, group: 'email', secret: false, hot: false, guards: [], set: false, source: 'default', editable: true, ...extra });
const data = {
  me: { operator: { id: 'o1', email: 'ops@example.com', totp_enabled: true }, session: { id: 's1' }, console: { origin: '', path_mode: true } },
  status: { desired_version: 1 },
  config: { version: 1, settings: [setting('smtp.host', { guards: ['reauth'] })], bootstrap: [], key_ring: { configured: true, key_ids: ['k1'] } },
  reload: async () => {},
} as unknown as ConsoleData;

test('the review takes the focus on its first field, and Escape from there closes it', () => {
  const opener = document.createElement('button');
  document.body.append(opener);
  opener.focus();
  const closed = vi.fn();
  render(<Review data={data} draft={{ set: { 'smtp.host': 'mail.example.org' }, unset: [], secrets: {} }} onClose={closed} onSaved={() => {}} onInvalid={() => {}} onConflict={() => {}} />);
  expect(document.activeElement).toBe(screen.getByLabelText(/Authenticator code/));
  fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
  expect(closed).toHaveBeenCalled();
  opener.remove();
});
