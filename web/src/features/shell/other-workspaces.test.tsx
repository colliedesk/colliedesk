import { act, render } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { OtherWorkspaces } from './other-workspaces';

// Clerk (the identity provider) is the external boundary: these are the ways it can fail to give a token.
const clerk = vi.hoisted(() => ({ isLoaded: true, isSignedIn: true, token: 'clerk-session-token' as string | null }));
vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  useAuth: () => ({ getToken: () => Promise.resolve(clerk.token), isLoaded: clerk.isLoaded, isSignedIn: clerk.isSignedIn }),
}));
const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => { Object.assign(clerk, { isLoaded: true, isSignedIn: true, token: 'clerk-session-token' }); fetchMock.mockReset(); vi.useRealTimers(); });

test('a signed-in identity without a token is a failure, not an empty list', async () => {
  const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
  clerk.token = null;
  const onResult = vi.fn();
  render(<OtherWorkspaces publishableKey="pk" onResult={onResult} />);
  await vi.waitFor(() => expect(onResult).toHaveBeenCalledWith({ status: 'failed', reason: 'no-token' }));
  expect(fetchMock).not.toHaveBeenCalled();
  warn.mockRestore();
});

test('an identity provider that never loads is a failure after ten seconds', async () => {
  const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
  vi.useFakeTimers();
  clerk.isLoaded = false;
  const onResult = vi.fn();
  render(<OtherWorkspaces publishableKey="pk" onResult={onResult} />);
  act(() => { vi.advanceTimersByTime(9_999); });
  expect(onResult).not.toHaveBeenCalled();
  act(() => { vi.advanceTimersByTime(1); });
  expect(onResult).toHaveBeenCalledWith({ status: 'failed', reason: 'not-loaded' });
  warn.mockRestore();
});
