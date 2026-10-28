import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { WebFormSettings } from '../../api/schemas';
import { WebFormSection } from './web-form-section';

const off: WebFormSettings = { enabled: false, allowed_origins: [], submit_path: '/api/v1/public/forms/acme' };
const on: WebFormSettings = { ...off, enabled: true, allowed_origins: ['https://colliedesk.com', 'http://localhost:3000'] };

test('is off by default, shows the state and the address a site posts to', () => {
  render(<WebFormSection settings={off} canManage onSave={vi.fn()} />);
  expect(screen.getByRole('checkbox', { name: /accept web form submissions/i })).not.toBeChecked();
  expect(screen.getByText(/the web form is off/i)).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: /submit url/i })).toHaveValue(`${window.location.origin}/api/v1/public/forms/acme`);
});

test('turns the form on with its allowed origins, one per line', () => {
  const onSave = vi.fn();
  render(<WebFormSection settings={off} canManage onSave={onSave} />);
  fireEvent.click(screen.getByRole('checkbox', { name: /accept web form submissions/i }));
  fireEvent.change(screen.getByRole('textbox', { name: /allowed origins/i }), { target: { value: ' https://colliedesk.com \n\nhttp://localhost:3000\n' } });
  fireEvent.click(screen.getByRole('button', { name: /save web form/i }));
  expect(onSave).toHaveBeenCalledWith(true, ['https://colliedesk.com', 'http://localhost:3000']);
});

test('turns the form off again and keeps the origins', () => {
  const onSave = vi.fn();
  render(<WebFormSection settings={on} canManage onSave={onSave} />);
  expect(screen.getByRole('checkbox', { name: /accept web form submissions/i })).toBeChecked();
  expect(screen.getByText(/the web form is on/i)).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: /allowed origins/i })).toHaveValue('https://colliedesk.com\nhttp://localhost:3000');
  fireEvent.click(screen.getByRole('checkbox', { name: /accept web form submissions/i }));
  fireEvent.click(screen.getByRole('button', { name: /save web form/i }));
  expect(onSave).toHaveBeenCalledWith(false, ['https://colliedesk.com', 'http://localhost:3000']);
});

test('agents see the state read-only', () => {
  render(<WebFormSection settings={on} canManage={false} />);
  expect(screen.getByText(/the web form is on/i)).toBeInTheDocument();
  expect(screen.getByText('https://colliedesk.com')).toBeInTheDocument();
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /save web form/i })).not.toBeInTheDocument();
});

test('the save shows Saving…, refuses a second submit, then Saved until edited', () => {
  const onSave = vi.fn();
  const { rerender } = render(<WebFormSection settings={off} canManage onSave={onSave} save={{ pending: true }} />);
  expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled();
  fireEvent.submit(screen.getByRole('textbox', { name: /allowed origins/i }));
  expect(onSave).not.toHaveBeenCalled();
  rerender(<WebFormSection settings={off} canManage onSave={onSave} save={{ saved: true }} />);
  expect(screen.getByText('Saved')).toHaveAttribute('role', 'status');
  fireEvent.click(screen.getByRole('checkbox', { name: /accept web form submissions/i }));
  expect(screen.queryByText('Saved')).not.toBeInTheDocument();
});

test('a refused save shows the message', () => {
  render(<WebFormSection settings={off} canManage save={{ error: 'Invalid origin.' }} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Invalid origin.');
});

test('the Embed tab copies the submit URL and shows the code a page posts with, ready to copy', async () => {
  const writeText = vi.fn(() => Promise.resolve());
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
  render(<WebFormSection part="embed" settings={on} canManage onSave={vi.fn()} />);
  const url = `${window.location.origin}/api/v1/public/forms/acme`;
  fireEvent.click(screen.getByRole('button', { name: 'Copy submit URL' }));
  expect(writeText).toHaveBeenLastCalledWith(url);
  expect(await screen.findByText('URL copied')).toHaveAttribute('role', 'status');
  const code = screen.getByRole('region', { name: 'Code for your page' });
  expect(code).toHaveTextContent(`fetch('${url}'`);
  expect(code).toHaveTextContent("website: ''");
  fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
  expect(writeText).toHaveBeenLastCalledWith(expect.stringContaining(`fetch('${url}'`));
});

test('without the clipboard, the Embed copy buttons say the copy failed', async () => {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
  render(<WebFormSection part="embed" settings={on} canManage onSave={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Copy submit URL' }));
  expect(await screen.findByText('Could not copy. Select the text and copy it yourself.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
  // One message at a time: the second failure replaces the first.
  await waitFor(() => {
    const failures = screen.getAllByText('Could not copy. Select the text and copy it yourself.');
    expect(failures).toHaveLength(1);
    expect(failures[0].previousElementSibling).toHaveTextContent('Copy code');
  });
});

test('one copy message at a time: copying the code replaces what copying the URL said', async () => {
  const writeText = vi.fn(() => Promise.resolve());
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
  render(<WebFormSection part="embed" settings={on} canManage onSave={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Copy submit URL' }));
  expect(await screen.findByText('URL copied')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
  expect(await screen.findByText('Code copied')).toBeInTheDocument();
  expect(screen.queryByText('URL copied')).toBeNull();
});

test('a form that also answers the workspace\'s older address shows one Submit URL, its own, and keeps the older one under a detail', () => {
  render(<WebFormSection settings={{ ...on, channel_id: '55555555-5555-4555-8555-555555555555', legacy_slug: true }} canManage onSave={vi.fn()} part="embed" />);
  const own = `${window.location.origin}/api/v1/public/web-forms/55555555-5555-4555-8555-555555555555`;
  expect(screen.getByRole('textbox', { name: /^Submit URL/ })).toHaveValue(own);
  expect(screen.getByRole('region', { name: 'Code for your page' })).toHaveTextContent(own);
  const older = screen.getByText('Older address').closest('details') as HTMLElement;
  expect(older).not.toHaveAttribute('open');
  expect(older).toHaveTextContent(`${window.location.origin}${on.submit_path}`);
  expect(older).toHaveTextContent(/only if your website already sends to it/);
  expect(screen.queryByText(/older one, which this form keeps/)).toBeNull();
});

test('Allowed origins says in plain words which websites it means, with an example', () => {
  render(<WebFormSection settings={off} canManage onSave={vi.fn()} />);
  expect(screen.getByRole('textbox', { name: /Allowed origins/i })).toHaveAccessibleDescription(/websites that may send this form.*https:\/\/www\.example\.com/i);
});

test('the form explains itself in plain words first and lets the team try it without a developer', async () => {
  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 202, headers: { 'Content-Type': 'application/json' } }));
  vi.stubGlobal('fetch', fetchMock);
  render(<WebFormSection settings={on} canManage part="embed" />);
  expect(screen.getByText(/Your website's contact form sends each visitor's email and message here/)).toBeInTheDocument();
  expect(screen.queryByText(/Post JSON/)).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox', { name: 'Your email' }), { target: { value: 'me@acme.test' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Message' }), { target: { value: 'Testing the form' } });
  fireEvent.click(screen.getByRole('button', { name: 'Try the form' }));
  expect(await screen.findByRole('status', { name: '' })).toHaveTextContent('It arrived: a conversation from me@acme.test is in your inbox.');
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(url).toBe('/api/v1/public/forms/acme');
  expect(JSON.parse(init.body as string)).toEqual({ email: 'me@acme.test', message: 'Testing the form', source: 'Test from Colliedesk', website: '' });
  vi.unstubAllGlobals();
});

test('a tried form that is refused says why', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'validation_failed', message: 'Check the highlighted fields.' } }), { status: 422, headers: { 'Content-Type': 'application/json' } })));
  render(<WebFormSection settings={on} canManage part="embed" />);
  fireEvent.change(screen.getByRole('textbox', { name: 'Your email' }), { target: { value: 'nope' } });
  fireEvent.click(screen.getByRole('button', { name: 'Try the form' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Check the highlighted fields.');
  vi.unstubAllGlobals();
});

test('the code help explains the hidden website field in plain words', () => {
  render(<WebFormSection part="embed" settings={on} canManage onSave={vi.fn()} />);
  const code = screen.getByRole('region', { name: 'Code for your page' });
  expect(code).toHaveTextContent('Keep the field named website empty. It is a trap for spam bots: a message with it filled in is thrown away.');
  expect(code).not.toHaveTextContent('as if stored');
});
