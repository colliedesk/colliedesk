import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { ExportRefusedError } from '../../api/workspace-export';
import { ExportAllData } from './export-all-data';

const api = vi.hoisted(() => ({ exportWorkspace: vi.fn() }));
const save = vi.hoisted(() => vi.fn());
vi.mock('../../api/client', () => ({ api }));
vi.mock('../../api/workspace-export', async (original) => ({ ...await original<typeof import('../../api/workspace-export')>(), saveArchive: save }));

function show() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}><ExportAllData /></QueryClientProvider>);
}
beforeEach(() => vi.clearAllMocks());

test('the file is saved and the person is told its name', async () => {
  const archive = { blob: new Blob(['zip']), filename: 'colliedesk-acme.zip', bytes: 3 };
  api.exportWorkspace.mockResolvedValue(archive);
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  await waitFor(() => expect(save).toHaveBeenCalledWith(archive));
  expect(await screen.findByRole('status')).toHaveTextContent('Export downloaded: colliedesk-acme.zip');
});

test('while it runs the button is disabled and the bytes received are shown', async () => {
  let finish!: (value: unknown) => void;
  api.exportWorkspace.mockImplementation((progress: (bytes: number) => void) => new Promise((resolve) => { progress(5 * 1024 * 1024); finish = resolve; }));
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  expect(await screen.findByRole('status')).toHaveTextContent(/preparing your export.*5\.0 MB/i);
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeDisabled();
  finish({ blob: new Blob([]), filename: 'x.zip', bytes: 0 });
});

test('429 says when to try again, using the wait the server gave', async () => {
  api.exportWorkspace.mockRejectedValue(new ExportRefusedError('server_busy', 120));
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('The server is busy with other exports. Try again in about 2 minutes.');
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeEnabled();
  expect(save).not.toHaveBeenCalled();
});

test('409 and a broken stream each get their own sentence', async () => {
  api.exportWorkspace.mockRejectedValueOnce(new ExportRefusedError('busy'));
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/already running/i);
  api.exportWorkspace.mockRejectedValueOnce(new ExportRefusedError('failed'));
  fireEvent.click(screen.getByRole('button', { name: 'Export all data' }));
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/did not finish, so no file was saved/i));
});
