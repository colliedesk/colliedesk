import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { expect, test } from 'vitest';
import type { ConsoleData } from './app';
import type { Feature, Setting } from './api';
import { ConfigPage } from './config-page';
import type { Draft } from './guards';
import { emptyDraft } from './guards';

const setting = (key: string, extra: Partial<Setting> = {}): Setting => ({ key, group: 'features', secret: false, hot: false, guards: ['reauth'], set: false, source: 'default', editable: true, ...extra });
const feature = (name: Feature['name'], extra: Partial<Feature> = {}): Feature => ({ name, label: name, switch_key: `features.${name}`, requested: 'unset', enabled: true, missing: [], ...extra });

function page(features: Feature[], settings: Setting[]) {
  const data = {
    me: { operator: { id: 'o1', email: 'ops@example.com', totp_enabled: true }, session: { id: 's1' }, console: { origin: '', path_mode: true } },
    status: { desired_version: 4 },
    config: { version: 4, settings: [...settings, setting('google.client_id', { group: 'mailboxes' }), setting('google.client_secret', { group: 'mailboxes', secret: true })],
      bootstrap: [], key_ring: { configured: true, key_ids: ['k1'] }, features },
    reload: async () => {},
  } as unknown as ConsoleData;
  function Page() {
    const [draft, setDraft] = useState<Draft>(emptyDraft());
    return <ConfigPage group="features" data={data} draft={draft} setDraft={setDraft} />;
  }
  return render(<Page />);
}

test('a feature missing settings cannot be switched on, and names what it needs with a way to each', () => {
  page([feature('gmail', { enabled: false, missing: ['google.client_id', 'google.client_secret'] }), feature('autofix')],
    [setting('features.gmail'), setting('features.autofix')]);
  const gmail = screen.getByRole('switch', { name: /Gmail mailboxes/ });
  expect(gmail).not.toBeChecked();
  expect(gmail).toBeDisabled();
  expect(gmail).toHaveAccessibleDescription(/Needs first:.*google\.client_id.*google\.client_secret/);
  expect(screen.getByRole('link', { name: 'Google client ID' })).toHaveAttribute('href', '/operator/config/mailboxes');
  const autofix = screen.getByRole('switch', { name: /Autofix/ });
  expect(autofix).toBeChecked();
  fireEvent.click(autofix);
  expect(autofix).not.toBeChecked();
  expect(screen.getByRole('region', { name: /1 unsaved change/ })).toBeInTheDocument();
});

test('a feature switched on whose settings went missing says it is off until they are set, and can be switched off', () => {
  page([feature('gmail', { requested: 'on', enabled: false, missing: ['google.client_id'] })], [setting('features.gmail', { set: true, value: 'true', source: 'console' })]);
  const gmail = screen.getByRole('switch', { name: /Gmail mailboxes/ });
  expect(gmail).toBeEnabled();
  expect(screen.getByText('Switched on, but off until these are set:')).toBeInTheDocument();
  fireEvent.click(gmail);
  expect(gmail).not.toBeChecked();
});
