import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { HelpCenterDomainSection } from './help-center-domain';
import { json, serve } from './kb.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const none = {
  hostname: null, status: 'none', records: [], created_at: null, verified_at: null, checked_at: null, check_error: null, certificate_requested_at: null,
  failing_since: null, pending_reason: null,
};
const records = [
  { type: 'CNAME', name: 'help.acme.example', value: 'hc-0123456789abcdef01234567.help.example.net', purpose: 'route_and_verify' },
  { type: 'TXT', name: '_help-center-verify.help.acme.example', value: '0123456789abcdef0123456789abcdef', purpose: 'verify' },
];
const pending = { ...none, hostname: 'help.acme.example', status: 'pending', records, created_at: '2026-10-22T08:00:00Z' };
const active = { ...pending, status: 'active', verified_at: '2026-10-22T08:05:00Z', checked_at: '2026-10-22T08:05:00Z' };

function open(canManage: boolean) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <HelpCenterDomainSection canManage={canManage} timezone="Asia/Tokyo" /></QueryClientProvider>);
}

test('an admin sets a domain, sees the records to copy, checks it and it becomes active', async () => {
  const writeText = vi.fn(async () => undefined);
  vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
  let checks = 0;
  const calls = serve({
    'GET /api/v1/help-center/domain': () => json(200, none),
    'PUT /api/v1/help-center/domain': () => json(200, pending),
    'POST /api/v1/help-center/domain/check': () => json(200, ++checks === 1
      ? { ...pending, checked_at: '2026-10-22T08:01:00Z', check_error: 'help.acme.example has no CNAME record pointing at hc-0123456789abcdef01234567.help.example.net.' }
      : active),
  });
  open(true);
  expect(await screen.findByText('No custom domain is set.')).toHaveAttribute('role', 'status');
  fireEvent.change(screen.getByRole('textbox', { name: 'Host name' }), { target: { value: ' help.acme.example ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Set domain' }));
  expect(await screen.findByText(/help.acme.example is waiting for its DNS records/)).toHaveAttribute('role', 'status');
  expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ hostname: 'help.acme.example' });

  const txt = screen.getByRole('group', { name: 'TXT record: proves the domain is yours' });
  expect(within(txt).getByRole('textbox', { name: 'Value' })).toHaveValue('0123456789abcdef0123456789abcdef');
  fireEvent.click(within(txt).getByRole('button', { name: 'Copy the value of the TXT record' }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith('0123456789abcdef0123456789abcdef'));
  const cname = screen.getByRole('group', { name: 'CNAME record: sends visitors here and proves the domain is yours' });
  fireEvent.click(within(cname).getByRole('button', { name: 'Copy the name of the CNAME record' }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith('help.acme.example'));

  fireEvent.click(screen.getByRole('button', { name: 'Check now' }));
  expect(await screen.findByText(/Last check: help.acme.example has no CNAME record/)).toBeInTheDocument();
  // The time of the check shows in the agent's timezone.
  expect(screen.getByText(/Last checked Oct 22.*5:01\sPM/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Check now' }));
  expect(await screen.findByText('The help center is served on help.acme.example.')).toHaveAttribute('role', 'status');
  expect(screen.queryByText(/Last check:/)).not.toBeInTheDocument();
});

test('an active domain warns when the daily check fails, and Remove asks before it removes', async () => {
  const calls = serve({
    'GET /api/v1/help-center/domain': () => json(200, { ...active, check_error: 'help.acme.example points somewhere else.' }),
    'DELETE /api/v1/help-center/domain': () => json(200, none),
  });
  open(true);
  expect(await screen.findByText('Last check: help.acme.example points somewhere else.')).toHaveClass('warning-note');
  fireEvent.click(screen.getByRole('button', { name: 'Remove domain' }));
  const question = screen.getByRole('group', { name: 'Remove the custom domain?' });
  expect(within(question).getByRole('button', { name: 'Cancel' })).toHaveFocus();
  fireEvent.keyDown(question, { key: 'Escape' });
  await waitFor(() => expect(screen.getByRole('button', { name: 'Remove domain' })).toHaveFocus());
  expect(calls.some((call) => call.method === 'DELETE')).toBe(false);

  fireEvent.click(screen.getByRole('button', { name: 'Remove domain' }));
  fireEvent.click(within(screen.getByRole('group', { name: 'Remove the custom domain?' })).getByRole('button', { name: 'Remove' }));
  expect(await screen.findByText('No custom domain is set.')).toBeInTheDocument();
  expect(calls.filter((call) => call.method === 'DELETE')).toHaveLength(1);
});

test('a refused name shows the server reason; a check of a host another workspace still proves is named as taken', async () => {
  let attempt = 0;
  serve({
    'GET /api/v1/help-center/domain': () => json(200, none),
    'PUT /api/v1/help-center/domain': () => ++attempt === 1
      ? json(422, { error: { code: 'validation_failed', message: 'An IP address cannot be a custom domain. Nothing was saved.', fields: { hostname: 'An IP address cannot be a custom domain.' } } })
      : json(200, pending),
    'POST /api/v1/help-center/domain/check': () => json(409, { error: { code: 'domain_taken', message: "Another workspace's help center already serves on this host name." } }),
  });
  open(true);
  const input = await screen.findByRole('textbox', { name: 'Host name' });
  fireEvent.change(input, { target: { value: '192.0.2.1' } });
  fireEvent.click(screen.getByRole('button', { name: 'Set domain' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('An IP address cannot be a custom domain. Nothing was saved.');
  fireEvent.change(input, { target: { value: 'help.acme.example' } });
  fireEvent.click(screen.getByRole('button', { name: 'Set domain' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Check now' }));
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent("Another workspace's help center already serves on this host name."));
});

test('an active domain whose checks fail says when it stops serving; a released one says why', async () => {
  let state: object = { ...active, check_error: 'help.acme.example has no CNAME record.', failing_since: '2026-10-20T03:00:00Z' };
  serve({ 'GET /api/v1/help-center/domain': () => json(200, state) });
  open(true);
  // Dates show in the agent's timezone: 7 days after the first failed check.
  expect(await screen.findByText(/No DNS check has found these records since Oct 20.*12:00\sPM\. The help center stops serving on help\.acme\.example if none finds them by Oct 27.*12:00\sPM\./))
    .toHaveClass('warning-note');
  cleanup();
  state = { ...pending, check_error: 'help.acme.example has no CNAME record.', pending_reason: 'proof_lost' };
  open(true);
  expect(await screen.findByText('help.acme.example stopped serving the help center: no DNS check found its records for 7 days. Its other addresses serve it again until a check finds them.'))
    .toHaveClass('warning-note');
  cleanup();
  state = { ...pending, pending_reason: 'taken_over' };
  open(true);
  expect(await screen.findByText("help.acme.example stopped serving the help center: another workspace's DNS records proved this host name while these were missing. Set them again and press Check now."))
    .toHaveClass('warning-note');
});

test('an agent sees the domain and its records but cannot change it', async () => {
  serve({ 'GET /api/v1/help-center/domain': () => json(200, active) });
  open(false);
  expect(await screen.findByText('The help center is served on help.acme.example.')).toBeInTheDocument();
  expect(screen.getByRole('group', { name: 'TXT record: proves the domain is yours' })).toBeInTheDocument();
  expect(screen.queryByRole('textbox', { name: 'Host name' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Check now' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Remove domain' })).not.toBeInTheDocument();
  expect(screen.getByText('Only admins can set or remove the custom domain.')).toBeInTheDocument();
});
