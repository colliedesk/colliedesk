import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { ConsoleData } from './app';
import type { Setting } from './api';
import type { Draft } from './guards';

// In Vietnamese, an error the server sends with a code shows translated beside its setting; one without a code
// shows the server's English (codes.ts).
beforeEach(() => { vi.resetModules(); });
afterEach(() => { vi.unstubAllGlobals(); localStorage.clear(); document.documentElement.lang = 'en'; });

const setting = (key: string): Setting => ({ key, group: 'limits', secret: false, hot: true, guards: [], set: false, source: 'default', editable: true, default: '209715200' } as Setting);

test('a coded error of a setting shows in Vietnamese', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
    ok: false,
    errors: { 'kb_video.max_bytes': 'kb_video.max_bytes must be a whole number of bytes above zero', 'mail_image_cache.max_bytes': 'words without a code' },
    field_errors: { 'kb_video.max_bytes': { code: 'kb_video_max_invalid' } },
  }), { status: 200 })));
  localStorage.setItem('colliedesk.operator.lang', 'vi');
  await (await import('./lang')).loadLang();
  const { ConfigPage } = await import('./config-page');
  const { emptyDraft } = await import('./guards');
  const { fields } = await import('./fields');
  const { m } = await import('./messages');
  const data = {
    me: { operator: { id: 'o1', email: 'ops@example.com', totp_enabled: false }, session: { id: 's1' }, console: { origin: '', path_mode: true } },
    status: { desired_version: 4, warnings: [] },
    config: { version: 4, settings: [setting('kb_video.max_bytes'), setting('mail_image_cache.max_bytes')], bootstrap: [], key_ring: { configured: true, key_ids: ['k1'] }, features: [] },
    reload: async () => {},
  } as unknown as ConsoleData;
  function Page() {
    const [draft, setDraft] = useState<Draft>(emptyDraft());
    return <ConfigPage group="limits" data={data} draft={draft} setDraft={setDraft} />;
  }
  render(<Page />);
  fireEvent.change(screen.getByLabelText(fields['kb_video.max_bytes'].label), { target: { value: '0' } });
  fireEvent.click(screen.getByRole('button', { name: m.save }));
  expect(await screen.findByText('kb_video.max_bytes phải là số byte nguyên lớn hơn 0')).toBeInTheDocument();
  expect(screen.getByText('words without a code')).toBeInTheDocument();
});
