import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { ConversationViewer } from '../../api/schemas';
import { ConversationViewers } from './presence-view';

const person = (name: string, extra: Partial<ConversationViewer> = {}): ConversationViewer => ({
  agent_id: `44444444-4444-4444-8444-${name.padEnd(12, '0').slice(0, 12).replace(/[^0-9a-f]/gi, '0')}`, name, writing: false, writing_since: null, edited_at: null, agent_token: null, ...extra,
});
const token = (of: ConversationViewer, label: string, id: string): ConversationViewer => ({ ...of, agent_token: { id, label, scope: 'read', agent_name: of.name } });

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-22T09:10:00Z')); });
afterEach(() => vi.useRealTimers());

test('one line per person, the one writing a reply first, an AI agent once per person with all its tokens', () => {
  const ben = person('Ben');
  render(<ConversationViewers timezone="UTC" viewers={[
    ben, token(ben, 'Claude Code', '66666666-6666-4666-8666-666666666661'), token(ben, 'Codex', '66666666-6666-4666-8666-666666666662'),
    person('Lan', { writing: true, writing_since: '2026-10-22T09:08:00Z', edited_at: '2026-10-22T09:09:30Z' }),
  ]} />);
  expect(screen.getAllByRole('listitem').map((item) => item.lastElementChild?.textContent)).toEqual(['Lan is writing a reply', 'Ben is viewing', 'Ben via Claude Code and Codex is viewing']);
  expect(document.querySelectorAll('.avatar-agent')).toHaveLength(1);
});

test('a reply untouched for more than two minutes is a draft left open: the time in the viewer timezone and the next step', () => {
  render(<ConversationViewers timezone="Asia/Ho_Chi_Minh" viewers={[
    person('Lan', { writing: true, writing_since: '2026-10-22T08:00:00Z', edited_at: '2026-10-22T09:07:30Z' }),
  ]} />);
  expect(screen.getByRole('listitem')).toHaveTextContent('Lan has a reply not sent yet, untouched since Oct 22, 4:07 PM. Ask them before replying.');
});

test('past three people the rest is one line, named for a screen reader and in the tip', () => {
  render(<ConversationViewers timezone="UTC" viewers={['Ann', 'Bob', 'Cy', 'Dee', 'Eve'].map((name) => person(name))} />);
  const items = screen.getAllByRole('listitem');
  expect(items).toHaveLength(4);
  expect(items[3]).toHaveTextContent('+2 more');
  expect(items[3]).toHaveAttribute('data-tip', 'Dee is viewing\nEve is viewing');
});

test('a screen reader hears what changed, not the whole list again', () => {
  const lan = person('Lan');
  const { rerender } = render(<ConversationViewers timezone="UTC" viewers={[lan]} />);
  const live = document.querySelector('[aria-live="polite"]')!;
  expect(live).toHaveTextContent('Lan is viewing');
  act(() => rerender(<ConversationViewers timezone="UTC" viewers={[lan, person('Ben')]} />));
  expect(live).toHaveTextContent(/^Ben is viewing$/);
  act(() => rerender(<ConversationViewers timezone="UTC" viewers={[{ ...lan, writing: true, writing_since: '2026-10-22T09:09:00Z', edited_at: '2026-10-22T09:09:50Z' }, person('Ben')]} />));
  expect(live).toHaveTextContent(/^Lan is writing a reply$/);
});
