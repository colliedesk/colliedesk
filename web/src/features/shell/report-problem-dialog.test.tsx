import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { recordClientError, recordFailedCall, resetDiagnostics } from '../../lib/diagnostics';
import { track } from '../../lib/product-analytics';
import { ReportProblemDialog } from './report-problem-dialog';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);

beforeEach(() => {
  resetDiagnostics();
  window.history.pushState({}, '', '/conversations/abc?q=zed');
  recordFailedCall('GET', '/api/v1/conversations?q=zed', 500, '9f3ac1d2b7e4');
  recordClientError('window.error', new TypeError('x is undefined'));
  track('conversation_opened', { source: 'list' });
});
afterEach(() => fetchMock.mockReset());

const json = (status: number, body: unknown, headers: Record<string, string> = {}) => new Response(JSON.stringify(body), { status, headers });
const describe = (text: string) => fireEvent.change(screen.getByLabelText('What went wrong?'), { target: { value: text } });
const sentBody = () => JSON.parse(fetchMock.mock.calls[0][1].body as string) as Record<string, any>;

test('it is a labelled modal dialog with the description field focused', () => {
  render(<ReportProblemDialog appVersion="0.0.41" onClose={() => undefined} />);
  const dialog = screen.getByRole('dialog', { name: 'Report a problem' });
  expect(dialog).toHaveAttribute('aria-modal', 'true');
  expect(screen.getByLabelText('What went wrong?')).toHaveFocus();
  expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
});

test('the preview lists every field that would be sent, with its value', () => {
  render(<ReportProblemDialog appVersion="0.0.41" onClose={() => undefined} />);
  describe('Inbox is empty');
  const preview = screen.getByText('What will be sent').closest('details') as HTMLElement;
  expect(preview).not.toHaveAttribute('open');
  for (const field of ['Your description', 'Screenshots', 'App version', 'Browser', 'Window size', 'Page', 'Recent errors', 'Recent failed requests', 'Recent actions']) {
    expect(within(preview).getByText(new RegExp(`^${field}`))).toBeInTheDocument();
  }
  expect(preview).toHaveTextContent('Inbox is empty');
  expect(preview).toHaveTextContent('0.0.41');
  expect(preview).toHaveTextContent(navigator.userAgent);
  expect(preview).toHaveTextContent('/conversations/abc');
  expect(preview).not.toHaveTextContent('q=zed');
  expect(preview).toHaveTextContent('TypeError: x is undefined');
  expect(preview).toHaveTextContent('GET /api/v1/conversations');
  expect(preview).toHaveTextContent('request id 9f3ac1d2b7e4');
  expect(preview).toHaveTextContent('conversation_opened');
  expect(preview).toHaveTextContent('your agent id, name, email and role');
});

test('Send posts the description with the same diagnostics the preview shows, then shows the reference', async () => {
  fetchMock.mockResolvedValue(json(201, { reference: '5f1c9a0e7b22' }));
  render(<ReportProblemDialog appVersion="0.0.41" onClose={() => undefined} />);
  describe('  Replies never arrive  ');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(await screen.findByRole('status')).toHaveTextContent('5f1c9a0e7b22');
  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [url, init] = fetchMock.mock.calls[0];
  expect(url).toBe('/api/v1/support/problem-reports');
  expect(init.method).toBe('POST');
  const body = sentBody();
  expect(body.description).toBe('Replies never arrive');
  expect(body.diagnostics.app_version).toBe('0.0.41');
  expect(body.diagnostics.page_path).toBe('/conversations/abc');
  expect(body.diagnostics.failed_api_calls[0]).toMatchObject({ route: '/api/v1/conversations', status: 500, request_id: '9f3ac1d2b7e4' });
  expect(body.diagnostics.client_errors[0].message).toBe('TypeError: x is undefined');
  expect(body.diagnostics.product_events).toContain('conversation_opened');
  // The name of the action is kept for the next report; no properties exist to send.
  expect(JSON.stringify(body)).not.toContain('"source":"list"');
});

test('after a successful send the event problem_reported is tracked by name only', async () => {
  fetchMock.mockResolvedValue(json(201, { reference: 'abc' }));
  render(<ReportProblemDialog onClose={() => undefined} />);
  describe('x');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  await screen.findByRole('status');
  const { collectDiagnostics } = await import('../../lib/diagnostics');
  expect(collectDiagnostics().product_events?.at(-1)).toBe('problem_reported');
});

test('screenshots go as multipart parts next to the description and the diagnostics', async () => {
  fetchMock.mockResolvedValue(json(201, { reference: 'abc' }));
  render(<ReportProblemDialog onClose={() => undefined} />);
  describe('Button does nothing');
  const png = new File([new Uint8Array([137, 80, 78, 71])], 'screen.png', { type: 'image/png' });
  fireEvent.change(screen.getByLabelText(/Screenshots/), { target: { files: [png] } });
  expect(screen.getByText('What will be sent').closest('details')).toHaveTextContent('screen.png');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  await screen.findByRole('status');
  const form = fetchMock.mock.calls[0][1].body as FormData;
  expect(form).toBeInstanceOf(FormData);
  expect(form.get('description')).toBe('Button does nothing');
  expect(JSON.parse(form.get('diagnostics') as string).failed_api_calls[0].request_id).toBe('9f3ac1d2b7e4');
  expect((form.getAll('screenshots')[0] as File).name).toBe('screen.png');
  expect(fetchMock.mock.calls[0][1].headers?.['Content-Type']).toBeUndefined();
});

test('only small PNG, JPEG or WebP screenshots are accepted, at most three', () => {
  render(<ReportProblemDialog onClose={() => undefined} />);
  const input = screen.getByLabelText(/Screenshots/);
  expect(input).toHaveAttribute('accept', 'image/png,image/jpeg,image/webp');
  fireEvent.change(input, { target: { files: [new File(['%PDF'], 'doc.pdf', { type: 'application/pdf' })] } });
  expect(screen.getByRole('alert')).toHaveTextContent('PNG, JPEG or WebP');
  const png = (name: string) => new File(['x'], name, { type: 'image/png' });
  fireEvent.change(input, { target: { files: [png('1.png'), png('2.png'), png('3.png'), png('4.png')] } });
  expect(screen.getByRole('alert')).toHaveTextContent('at most 3');
  const big = new File([new Uint8Array(3 << 20)], 'big.png', { type: 'image/png' });
  fireEvent.change(input, { target: { files: [big, new File([new Uint8Array(3 << 20)], 'big2.png', { type: 'image/png' })] } });
  expect(screen.getByRole('alert')).toHaveTextContent('5 MB');
  expect(screen.getByText('What will be sent').closest('details')).not.toHaveTextContent('big.png');
});

test('a server without a support desk gets a prefilled GitHub issue and nothing else is sent', async () => {
  recordClientError('window.error', new Error('Failed for ada@acme.example.com'));
  fetchMock.mockResolvedValue(json(404, { error: { code: 'not_configured', message: 'This server does not receive problem reports.' } }));
  render(<ReportProblemDialog appVersion="0.0.41" onClose={() => undefined} />);
  describe('The inbox spins');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  const link = await screen.findByRole('link', { name: 'Open a GitHub issue' });
  expect(fetchMock).toHaveBeenCalledTimes(1);
  expect(screen.getByRole('dialog')).toHaveTextContent('Nothing was sent to HC Labs');
  const url = new URL(link.getAttribute('href') as string);
  expect(url.origin + url.pathname).toBe('https://github.com/colliedesk/colliedesk/issues/new');
  expect(url.searchParams.get('title')).toContain('The inbox spins');
  const body = url.searchParams.get('body') as string;
  for (const want of ['The inbox spins', '0.0.41', '/conversations/abc', 'TypeError: x is undefined', '9f3ac1d2b7e4', '/api/v1/conversations', 'conversation_opened']) {
    expect(body).toContain(want);
  }
  expect(body).not.toContain('ada@acme.example.com');
  expect(body).not.toContain('@');
  expect(link).toHaveAttribute('target', '_blank');
  expect(link.getAttribute('rel')).toContain('noopener');
});

test('a very long report still fits in a GitHub address', async () => {
  for (let index = 0; index < 20; index++) recordClientError('window.error', new Error(`${index} ${'e'.repeat(480)}`));
  fetchMock.mockResolvedValue(json(404, { error: { code: 'not_configured', message: 'No.' } }));
  render(<ReportProblemDialog onClose={() => undefined} />);
  describe('y'.repeat(5000));
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  const link = await screen.findByRole('link', { name: 'Open a GitHub issue' });
  expect((link.getAttribute('href') as string).length).toBeLessThan(7500);
});

test('a refused report keeps the text and says why, and can be sent again', async () => {
  fetchMock.mockResolvedValueOnce(json(429, { error: { code: 'rate_limited', message: 'You can send 5 problem reports an hour. Try again later.' } }, { 'Retry-After': '120' }));
  fetchMock.mockResolvedValueOnce(json(201, { reference: 'second' }));
  render(<ReportProblemDialog onClose={() => undefined} />);
  describe('Please look');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('You can send 5 problem reports an hour');
  expect(screen.getByLabelText('What went wrong?')).toHaveValue('Please look');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(await screen.findByRole('status')).toHaveTextContent('second');
});

test('a server error is reported in plain words', async () => {
  fetchMock.mockResolvedValue(json(500, { error: { code: 'internal_error', message: 'x' } }));
  render(<ReportProblemDialog onClose={() => undefined} />);
  describe('Please look');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('could not be sent');
  expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled();
});

test('Cancel and Escape close the dialog without sending anything', () => {
  const onClose = vi.fn();
  render(<ReportProblemDialog onClose={onClose} />);
  describe('Never sent');
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
  expect(onClose).toHaveBeenCalledTimes(2);
  expect(fetchMock).not.toHaveBeenCalled();
});

test('Tab wraps inside the dialog so the page behind it is not reached', async () => {
  render(<ReportProblemDialog onClose={() => undefined} />);
  const dialog = screen.getByRole('dialog');
  const cancel = screen.getByRole('button', { name: 'Cancel' });
  cancel.focus();
  fireEvent.keyDown(dialog, { key: 'Tab' });
  await waitFor(() => expect(screen.getByLabelText('What went wrong?')).toHaveFocus());
  fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true });
  await waitFor(() => expect(cancel).toHaveFocus());
});

test('a click on the backdrop keeps the focus in the dialog, so Escape still closes it', () => {
  const onClose = vi.fn();
  render(<ReportProblemDialog appVersion="0.0.41" onClose={onClose} />);
  const dialog = screen.getByRole('dialog');
  expect(fireEvent.mouseDown(dialog.parentElement!)).toBe(false);
  expect(fireEvent.mouseDown(screen.getByLabelText('What went wrong?'))).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape' });
  expect(onClose).toHaveBeenCalled();
});
