import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { ExportRefusedError } from '../../api/workspace-export';
import { ExportAllData } from './export-all-data';

const api = vi.hoisted(() => ({ requestExportLink: vi.fn() }));
vi.mock('../../api/client', () => ({ api }));
const assign = vi.fn();

function show() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}><ExportAllData /></QueryClientProvider>);
}
beforeEach(() => {
  vi.clearAllMocks();
  Object.defineProperty(window, 'location', { configurable: true, value: { assign } });
});

test('the browser is sent to the one-time link, so the file streams to disk', async () => {
  api.requestExportLink.mockResolvedValue('/api/v1/export/download/abc');
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('/api/v1/export/download/abc'));
  expect(await screen.findByRole('status')).toHaveTextContent(/download is starting/i);
});

test('while the link is requested the button is disabled', async () => {
  let finish!: (value: string) => void;
  api.requestExportLink.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Export all data' })).toBeDisabled());
  finish('/x');
});

test('429 says when to try again, using the wait the server gave, and nothing is opened', async () => {
  api.requestExportLink.mockRejectedValue(new ExportRefusedError('server_busy', 120));
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('The server is busy with other exports. Try again in about 2 minutes.');
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeEnabled();
  expect(assign).not.toHaveBeenCalled();
});

test('409 and a failed request each get their own sentence', async () => {
  api.requestExportLink.mockRejectedValueOnce(new ExportRefusedError('busy'));
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/already running/i);
  api.requestExportLink.mockRejectedValueOnce(new ExportRefusedError('failed'));
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/could not be started/i));
});
