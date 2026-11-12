import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, test, vi } from 'vitest';
import { MentionEmailSetting } from './mention-email-setting';

afterEach(() => vi.unstubAllGlobals());

const reply = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

test('Email me when someone mentions me shows the person\'s setting, on at first, and saves a change', async () => {
  const calls: Array<[string, RequestInit | undefined]> = [];
  vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
    calls.push([path, init]);
    if (path.startsWith('/api/v1/mentions')) return reply({ items: [], unread: 0, email: true, next_before: null });
    return reply({ email: false });
  }));
  render(<QueryClientProvider client={new QueryClient()}><MentionEmailSetting /></QueryClientProvider>);
  const box = await screen.findByRole('checkbox', { name: 'Email me when someone mentions me' });
  await waitFor(() => expect(box).toBeChecked());
  fireEvent.click(box);
  await waitFor(() => expect(calls.some(([path, init]) => path === '/api/v1/agents/me/mention-email' && init?.method === 'PATCH' && init.body === '{"email":false}')).toBe(true));
  await waitFor(() => expect(box).not.toBeChecked());
});
