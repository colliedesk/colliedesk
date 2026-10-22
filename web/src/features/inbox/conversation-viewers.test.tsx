import { render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { ConversationViewer } from '../../api/schemas';
import { ConversationViewers } from './conversation-viewers';

const person = (name: string, extra: Partial<ConversationViewer> = {}): ConversationViewer => ({
  agent_id: '44444444-4444-4444-8444-444444444444', name, writing: false, writing_since: null, edited_at: null, agent_token: null, ...extra,
});

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-22T09:10:00Z')); });
afterEach(() => vi.useRealTimers());

test('nobody else: nothing is shown', () => {
  const { container } = render(<ConversationViewers viewers={[]} timezone="UTC" />);
  expect(container).toBeEmptyDOMElement();
});

test('says who is writing a reply, who is viewing, and which AI agent works through which token', () => {
  render(<ConversationViewers timezone="UTC" viewers={[
    person('Lan', { writing: true, writing_since: '2026-10-22T09:08:00Z', edited_at: '2026-10-22T09:09:30Z' }),
    person('Ben'),
    person('Ben', { agent_token: { id: '66666666-6666-4666-8666-666666666666', label: 'Claude Code', scope: 'read', agent_name: 'Ben' } }),
  ]} />);
  const status = screen.getByRole('status');
  expect(status).toHaveTextContent('Lan is writing a reply · Ben is viewing · Ben via Claude Code is viewing');
  expect(status.querySelectorAll('.avatar')).toHaveLength(3);
  expect(status.querySelector('[data-writing]')).toHaveTextContent('Lan is writing a reply');
});

test('a reply untouched for more than two minutes is a draft left open, with the time in the viewer timezone', () => {
  render(<ConversationViewers timezone="Asia/Ho_Chi_Minh" viewers={[
    person('Lan', { writing: true, writing_since: '2026-10-22T08:00:00Z', edited_at: '2026-10-22T09:07:30Z' }),
  ]} />);
  expect(screen.getByRole('status')).toHaveTextContent('Lan has a reply not sent yet, untouched since Oct 22, 4:07 PM');
});
