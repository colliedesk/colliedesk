import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import type { ConsoleData } from './app';
import type { Setting } from './api';
import { Review } from './config-page';

afterEach(() => vi.restoreAllMocks());

const setting = (key: string, extra: Partial<Setting> = {}): Setting => ({ key, group: 'email', secret: false, hot: false, guards: [], set: false, source: 'default', editable: true, ...extra });
const data = {
  me: { operator: { id: 'o1', email: 'ops@example.com', totp_enabled: true }, session: { id: 's1' }, console: { origin: '', path_mode: true } },
  status: { desired_version: 4 },
  config: { version: 4, settings: [setting('smtp.host', { guards: ['reauth', 'reenter_secret'], reenter: 'smtp.password', value: 'old.example.net', set: true }),
    setting('smtp.password', { secret: true, set: true, guards: ['reauth'] })], bootstrap: [], key_ring: { configured: true, key_ids: ['k1'] } },
  reload: async () => {},
} as unknown as ConsoleData;
const draft = { set: { 'smtp.host': 'new.example.net' }, unset: [], secrets: {} };

function answer(status: number, body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }));
}

// server answers the review's check of the change with guards, and every other call with the next answer.
function server(guards: unknown[], ...answers: Array<() => Promise<Response>>) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    if (String(input).endsWith('/config/check')) return answer(200, { ok: true, errors: {}, guards });
    const next = answers.shift();
    return next ? next() : answer(500, {});
  });
}
const saves = (spy: ReturnType<typeof server>) => spy.mock.calls.filter(([input]) => !String(input).endsWith('/config/check'));

test('the review asks for every check, and its button works only once each is filled', async () => {
  const change = server([], () => answer(200, { version: 5, applies: 'in_seconds' }));
  const saved = vi.fn();
  render(<Review data={data} draft={draft} onClose={() => {}} onSaved={saved} onInvalid={() => {}} onConflict={() => {}} />);
  expect(screen.getByText('Will be saved as v5.')).toBeInTheDocument();
  const save = screen.getByRole('button', { name: 'Save and apply' });
  expect(save).toBeDisabled();
  expect(await screen.findByText(/Still needed: enter smtp.password again, your 6-digit code/)).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText(/Enter the secret again/), { target: { value: 'pw' } });
  fireEvent.change(screen.getByLabelText(/Authenticator code/), { target: { value: '123456' } });
  expect(save).toBeEnabled();
  fireEvent.click(save);
  await waitFor(() => expect(saved).toHaveBeenCalledWith(5, 'in_seconds'));
  const body = JSON.parse(String(saves(change)[0][1]?.body));
  expect(body).toMatchObject({ base_version: 4, set: { 'smtp.host': 'new.example.net' }, secrets: { 'smtp.password': 'pw' }, reauth_code: '123456' });
});

test('Escape closes the review and applies nothing', () => {
  const fetch = server([]);
  const closed = vi.fn();
  render(<Review data={data} draft={draft} onClose={closed} onSaved={() => {}} onInvalid={() => {}} onConflict={() => {}} />);
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
  expect(closed).toHaveBeenCalled();
  expect(saves(fetch)).toEqual([]);
});

test('what a change does and the phrase to type come from the server', async () => {
  const change = server([{ key: 'smtp.host', preview: 'Billing turns on: 3 workspaces lock at once.', phrase: 'billing on' }], () => answer(200, { version: 5, applies: 'in_seconds' }));
  render(<Review data={data} draft={draft} onClose={() => {}} onSaved={() => {}} onInvalid={() => {}} onConflict={() => {}} />);
  expect(await screen.findByText('Billing turns on: 3 workspaces lock at once.')).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText(/Enter the secret again/), { target: { value: 'pw' } });
  fireEvent.change(screen.getByLabelText(/Authenticator code/), { target: { value: '123456' } });
  expect(screen.getByRole('button', { name: 'Save and apply' })).toBeDisabled();
  fireEvent.change(screen.getByLabelText('Type billing on to confirm'), { target: { value: 'Billing On' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save and apply' }));
  await waitFor(() => expect(saves(change)).toHaveLength(1));
  expect(JSON.parse(String(saves(change)[0][1]?.body)).confirm).toEqual({ 'smtp.host': 'Billing On' });
});

test('a wrong code keeps the other checks; a newer version keeps the change', async () => {
  server([], () => answer(400, { error: { code: 'reauth_code_invalid', message: 'no' } }), () => answer(409, { error: { code: 'config_changed', message: 'newer' } }));
  const conflict = vi.fn();
  render(<Review data={data} draft={draft} onClose={() => {}} onSaved={() => {}} onInvalid={() => {}} onConflict={conflict} />);
  await screen.findByText(/Still needed/);
  fireEvent.change(screen.getByLabelText(/Enter the secret again/), { target: { value: 'pw' } });
  fireEvent.change(screen.getByLabelText(/Authenticator code/), { target: { value: '123456' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save and apply' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Nothing was saved; your changes and the other checks are kept.');
  expect(screen.getByLabelText(/Enter the secret again/)).toHaveValue('pw');
  fireEvent.change(screen.getByLabelText(/Authenticator code/), { target: { value: '654321' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save and apply' }));
  await waitFor(() => expect(conflict).toHaveBeenCalled());
});
