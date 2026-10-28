import { beforeEach, expect, test, vi } from 'vitest';
import { defaultAgentTimezone } from './browser-timezone-default';

const me = (override: string | null, workspace = 'UTC') => ({ agent: { id: 'agent-1', timezone_override: override }, workspace: { timezone: workspace } });

beforeEach(() => window.localStorage.clear());

test('an agent who never chose a timezone, in a workspace still on UTC, gets the browser\'s zone once', async () => {
  const save = vi.fn().mockResolvedValue(undefined);
  expect(await defaultAgentTimezone(me(null), 'Asia/Ho_Chi_Minh', save)).toBe(true);
  expect(save).toHaveBeenCalledWith('Asia/Ho_Chi_Minh');
  // The agent may later go back to the workspace's zone; this browser never sets it again.
  expect(await defaultAgentTimezone(me(null), 'Asia/Ho_Chi_Minh', save)).toBe(false);
  expect(save).toHaveBeenCalledTimes(1);
});

test('a chosen zone, a workspace zone someone set, a UTC browser or an unknown zone are left alone', async () => {
  const save = vi.fn().mockResolvedValue(undefined);
  expect(await defaultAgentTimezone(me('Europe/Berlin'), 'Asia/Ho_Chi_Minh', save)).toBe(false);
  expect(await defaultAgentTimezone(me(null, 'America/New_York'), 'Asia/Ho_Chi_Minh', save)).toBe(false);
  expect(await defaultAgentTimezone(me(null), 'UTC', save)).toBe(false);
  expect(await defaultAgentTimezone(me(null), '', save)).toBe(false);
  expect(save).not.toHaveBeenCalled();
});

test('a refused save is tried again on the next sign-in', async () => {
  const save = vi.fn().mockRejectedValueOnce(new Error('API 500')).mockResolvedValueOnce(undefined);
  expect(await defaultAgentTimezone(me(null), 'Asia/Tokyo', save)).toBe(false);
  expect(await defaultAgentTimezone(me(null), 'Asia/Tokyo', save)).toBe(true);
});
