import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { expect, test, vi } from 'vitest';
import { AnsweredWithoutSecret, useCreateAction, useCreateMutation } from './use-create';

interface Call { input: { name: string }; key: string; settle: (made: string) => void; fail: (error: unknown) => void }

// A create the test answers by hand, recording each request with its key.
function fakeCreate() {
  const calls: Call[] = [];
  const create = vi.fn((input: { name: string }, key: string) => new Promise<string>((resolve, reject) => {
    calls.push({ input, key, settle: resolve, fail: reject });
  }));
  return { calls, create };
}

const wrapper = ({ children }: { children: ReactNode }) =>
  <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>{children}</QueryClientProvider>;

test('two presses of one input in flight send one request and both get its answer', async () => {
  const { calls, create } = fakeCreate();
  const { result } = renderHook(() => useCreateAction(create));
  let first!: Promise<string>, second!: Promise<string>;
  act(() => { first = result.current.run({ name: 'Billing' }); second = result.current.run({ name: 'Billing' }); });
  expect(create).toHaveBeenCalledTimes(1);
  await act(async () => { calls[0]!.settle('tag-1'); });
  await expect(first).resolves.toBe('tag-1');
  await expect(second).resolves.toBe('tag-1');
});

test('pending is true only while a create is in flight', async () => {
  const { calls, create } = fakeCreate();
  const { result } = renderHook(() => useCreateAction(create));
  expect(result.current.pending).toBe(false);
  let sent!: Promise<string>;
  act(() => { sent = result.current.run({ name: 'Billing' }); });
  expect(result.current.pending).toBe(true);
  await act(async () => { calls[0]!.settle('tag-1'); await sent; });
  expect(result.current.pending).toBe(false);
});

test('a retry after a failure goes under the same key', async () => {
  const { calls, create } = fakeCreate();
  const { result } = renderHook(() => useCreateAction(create));
  let sent!: Promise<string>;
  act(() => { sent = result.current.run({ name: 'Billing' }); });
  await act(async () => { calls[0]!.fail(new Error('network')); await sent.catch(() => undefined); });
  act(() => { sent = result.current.run({ name: 'Billing' }); });
  expect(calls).toHaveLength(2);
  expect(calls[1]!.key).toBe(calls[0]!.key);
  await act(async () => { calls[1]!.settle('tag-1'); await sent; });
});

test('another input, or the same input pressed again after a success, gets a new key', async () => {
  const { calls, create } = fakeCreate();
  const { result } = renderHook(() => useCreateAction(create));
  let sent!: Promise<string>;
  act(() => { sent = result.current.run({ name: 'Billing' }); });
  await act(async () => { calls[0]!.settle('tag-1'); await sent; });
  act(() => { sent = result.current.run({ name: 'Billing' }); });
  await act(async () => { calls[1]!.settle('tag-2'); await sent; });
  act(() => { sent = result.current.run({ name: 'Sales' }); });
  await act(async () => { calls[2]!.settle('tag-3'); await sent; });
  expect(new Set(calls.map((call) => call.key)).size).toBe(3);
});

test('a repeat answered without its secret rotates the key, so the next press is a new create', async () => {
  const { calls, create } = fakeCreate();
  const { result } = renderHook(() => useCreateAction(create));
  let sent!: Promise<string>;
  act(() => { sent = result.current.run({ name: 'Ops' }); });
  await act(async () => { calls[0]!.fail(new AnsweredWithoutSecret()); await sent.catch(() => undefined); });
  await expect(sent).rejects.toBeInstanceOf(AnsweredWithoutSecret);
  act(() => { sent = result.current.run({ name: 'Ops' }); });
  expect(calls[1]!.key).not.toBe(calls[0]!.key);
  await act(async () => { calls[1]!.settle('token-1'); await sent; });
});

test('useCreateMutation sends one request for two mutate calls of one input in flight', async () => {
  const { calls, create } = fakeCreate();
  const onSuccess = vi.fn();
  const { result } = renderHook(() => useCreateMutation({ create, onSuccess }), { wrapper });
  act(() => { result.current.mutate({ name: 'Billing' }); result.current.mutate({ name: 'Billing' }); });
  await waitFor(() => expect(result.current.isPending).toBe(true));
  expect(create).toHaveBeenCalledTimes(1);
  await act(async () => { calls[0]!.settle('tag-1'); });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toBe('tag-1');
  expect(onSuccess).toHaveBeenCalledTimes(1);
});
