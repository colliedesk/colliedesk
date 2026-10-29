import { act, render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import type { Trial } from './api';
import TrialStrip, { announcement, clock } from './trial';

afterEach(() => vi.useRealTimers());

const trial = (expires: number): Trial => ({ version: 43, parent_version: 42, started_at: new Date(0).toISOString(), expires_at: new Date(expires).toISOString(),
  started_by: 'ops@example.com', keys: ['trusted_proxies'], evidence: { your_ip: '203.0.113.7', client_ips: 1, cloud_sign_ins_ok: 0, cloud_sign_ins_refused: 0 } });

// UI review risk b: the visible countdown is not live; a polite region speaks only at 5 minutes, 1 minute and
// 30 seconds, so a screen reader is not interrupted every second.
test('the countdown speaks at thresholds, never every second', () => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
  render(<TrialStrip trial={trial(600_000)} totp onDecided={() => {}} />);
  expect(screen.getByRole('timer')).toHaveAttribute('aria-live', 'off');
  expect(screen.getByRole('timer')).toHaveTextContent('10:00');
  const live = document.querySelector('[aria-live="polite"]')!;
  expect(live.textContent).toBe('');
  // Each change of the region's text is one announcement: record the text whenever it differs from before.
  const spoken: string[] = [];
  let last = live.textContent ?? '';
  for (let second = 0; second < 600; second++) {
    act(() => { vi.advanceTimersByTime(1000); });
    if ((live.textContent ?? '') !== last) { last = live.textContent ?? ''; spoken.push(last); }
  }
  expect(screen.getByRole('timer')).toHaveTextContent('0:00');
  expect(spoken).toEqual([
    'The trial reverts in 5 minutes unless confirmed.', 'The trial reverts in 1 minute unless confirmed.',
    'The trial reverts in 30 seconds unless confirmed.', 'The trial time is up; the server reverts it.']);
});

test('the clock and the words', () => {
  expect(clock(61)).toBe('1:01');
  expect(clock(-3)).toBe('0:00');
  expect(announcement(301)).toBe('');
  expect(announcement(45)).toBe('The trial reverts in 1 minute unless confirmed.');
});

test('one client address for everyone warns before keeping the trial', () => {
  render(<TrialStrip trial={trial(Date.now() + 600_000)} totp onDecided={() => {}} />);
  expect(screen.getByText(/Your address reads as 203.0.113.7; 1 address seen/)).toBeInTheDocument();
  act(() => { screen.getByRole('button', { name: 'Confirm v43' }).click(); });
  expect(screen.getByRole('alert')).toHaveTextContent('one client address for everyone');
});
