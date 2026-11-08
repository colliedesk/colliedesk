import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import type { ConsoleData } from './app';
import type { Setting } from './api';
import { ConfigPage } from './config-page';
import type { Draft } from './guards';
import { emptyDraft } from './guards';

const setting = (key: string, group: string, extra: Partial<Setting> = {}): Setting => ({ key, group: group as Setting['group'], secret: false, hot: true, guards: [], set: false, source: 'default', editable: true, ...extra });

let shown: Draft = emptyDraft();
function page(group: string, settings: Setting[], totp = false) {
  const data = {
    me: { operator: { id: 'o1', email: 'ops@example.com', totp_enabled: totp }, session: { id: 's1' }, console: { origin: '', path_mode: true } },
    status: { desired_version: 4 },
    config: { version: 4, settings, bootstrap: [], key_ring: { configured: true, key_ids: ['k1'] }, features: [] },
    reload: async () => {},
  } as unknown as ConsoleData;
  function Page() {
    const [draft, setDraft] = useState<Draft>(emptyDraft());
    shown = draft;
    return <ConfigPage group={group} data={data} draft={draft} setDraft={setDraft} />;
  }
  return render(<Page />);
}

function answer(...bodies: unknown[]) {
  const fetch = vi.fn();
  for (const body of bodies) fetch.mockResolvedValueOnce(new Response(JSON.stringify(body), { status: (body as { status?: number }).status ?? 200 }));
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

afterEach(() => { vi.unstubAllGlobals(); });

test('save checks first: a change that cannot be saved says why beside its setting, and no dialog opens', async () => {
  const fetch = answer({ ok: false, errors: { 'internal_signup.emails': 'not an address' } });
  page('signups', [setting('internal_signup.emails', 'signups'), setting('internal_signup.networks', 'signups')]);
  fireEvent.click(screen.getByRole('button', { name: 'Add to Team addresses' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Team addresses 1' }), { target: { value: 'nobody' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByText('not an address')).toBeInTheDocument();
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(fetch).toHaveBeenCalledTimes(1);
});

test('a change that asks for nothing more is saved at once, without a dialog', async () => {
  const fetch = answer({ ok: true, errors: {}, guards: [] }, { version: 5, applies: 'at_once' });
  page('signups', [setting('internal_signup.emails', 'signups', { set: true, value: '@team.example', source: 'console' }), setting('internal_signup.networks', 'signups')]);
  fireEvent.click(screen.getByRole('button', { name: 'Add to Team addresses' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Team addresses 2' }), { target: { value: 'ops@else.example' } });
  expect(shown.set['internal_signup.emails']).toBe('@team.example,ops@else.example');
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByText(/Saved v5/)).toBeInTheDocument();
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  const patch = (fetch.mock.calls[1] as unknown as [string, RequestInit]);
  expect(patch[1].method).toBe('PATCH');
});

test('a change that needs the authenticator code opens the review to ask for it', async () => {
  answer({ ok: true, errors: {}, guards: [] });
  page('features', [setting('features.autofix', 'features', { guards: ['reauth'] })], true);
  fireEvent.click(screen.getByRole('switch', { name: /Autofix/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  const dialog = await screen.findByRole('dialog');
  await waitFor(() => expect(dialog).toHaveTextContent(/authenticator code/i));
});

test('a size is typed in MB or GB and kept in bytes', () => {
  page('limits', [setting('kb_video.max_bytes', 'limits', { default: '209715200' }), setting('mail_image_cache.max_bytes', 'limits', { default: '1073741824' })]);
  expect(screen.getByLabelText('Largest knowledge base video')).toHaveValue('200');
  expect(screen.getByLabelText('Email image cache')).toHaveValue('1');
  fireEvent.change(screen.getByLabelText('Largest knowledge base video'), { target: { value: '500' } });
  expect(shown.set['kb_video.max_bytes']).toBe(String(500 * 1024 * 1024));
});
