import { fireEvent, render, screen } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { expect, test, vi } from 'vitest';
import { CHANNEL, gathering } from './intake.test.fixture';
import { ReviewPage } from './review-page';

const OTHER = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';

const props = (overrides: Partial<ComponentProps<typeof ReviewPage>> = {}): ComponentProps<typeof ReviewPage> => ({
  available: true, tab: 'waiting', onTab: vi.fn(), filter: '', onFilter: vi.fn(),
  scopes: [{ channel_id: CHANNEL, channel_name: 'Zalo HC Labs', scope: 'group' }, { channel_id: OTHER, channel_name: 'Zalo Shop', scope: 'group' }],
  items: [gathering()], renderCard: (item) => <article key={item.id} aria-label={item.lane_key}>{item.preview}</article>, ...overrides,
});

test('the page names itself, counts what waits and switches lists', () => {
  const handlers = props({ waitingCount: 3 });
  render(<ReviewPage {...handlers} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Review' })).toBeInTheDocument();
  expect(screen.getByRole('tab', { name: /^Waiting\s?3$/ })).toHaveAttribute('aria-selected', 'true');
  fireEvent.click(screen.getByRole('tab', { name: 'AI dismissed' }));
  expect(handlers.onTab).toHaveBeenCalledWith('dismissed');
  expect(screen.getByRole('tabpanel')).toHaveAccessibleName(/^Waiting/);
});

test('cards that wait for AI are counted under Waiting, not listed', () => {
  const { rerender } = render(<ReviewPage {...props({ decidingCount: 2 })} />);
  expect(screen.getByText('2 more cards wait for AI to decide; the rules decide what it does not in time.')).toBeInTheDocument();
  rerender(<ReviewPage {...props({ decidingCount: 0 })} />);
  expect(screen.queryByText(/wait for AI/)).not.toBeInTheDocument();
});

test('it filters by channel and scope, all chat channels first', () => {
  const handlers = props();
  render(<ReviewPage {...handlers} />);
  expect(screen.getByRole('radio', { name: 'All chat channels' })).toBeChecked();
  fireEvent.click(screen.getByRole('radio', { name: 'Zalo Shop · Groups' }));
  expect(handlers.onFilter).toHaveBeenCalledWith(`${OTHER}:group`);
});

test('with many channels and scopes the filter is a list', () => {
  const handlers = props({ scopes: ['a', 'b', 'c', 'd'].map((name, index) => ({ channel_id: `${index}${OTHER.slice(1)}`, channel_name: `Zalo ${name}`, scope: 'group' })) });
  render(<ReviewPage {...handlers} />);
  fireEvent.change(screen.getByRole('combobox', { name: 'Channel' }), { target: { value: `2${OTHER.slice(1)}:group` } });
  expect(handlers.onFilter).toHaveBeenCalledWith(`2${OTHER.slice(1)}:group`);
});

test('a card that waits behind another of its customer comes right under it', () => {
  const items = [
    gathering({ id: '1', lane_key: 'lane-a', preview: 'first of Lan' }),
    gathering({ id: '2', lane_key: 'lane-b', preview: 'Minh' }),
    gathering({ id: '3', lane_key: 'lane-a', review_reason: 'lane_blocked', preview: 'second of Lan' }),
  ];
  render(<ReviewPage {...props({ items })} />);
  expect(screen.getAllByRole('article').map((card) => card.textContent)).toEqual(['first of Lan', 'second of Lan', 'Minh']);
});

test('the counts of the last 7 days, AI corrections only when AI decided, and nothing when they failed', () => {
  const stats = { days: 7, gatherings: 9, by_state: { created: 5, attached: 1, ignored: 3 }, decided_by: { ai: 4, agent: 5 }, fallback: 0, ai_decisions: 4, ai_corrected: 1 };
  const { rerender } = render(<ReviewPage {...props({ stats })} />);
  expect(screen.getByText('Last 7 days: 9 cards, 6 became conversations')).toBeInTheDocument();
  expect(screen.getByText('People changed 1 of 4 AI decisions')).toBeInTheDocument();
  rerender(<ReviewPage {...props({ stats: { ...stats, gatherings: 0, by_state: {}, ai_decisions: 0, ai_corrected: 0 } })} />);
  expect(screen.getByText('Last 7 days: no cards yet')).toBeInTheDocument();
  expect(screen.queryByText(/AI decision/)).not.toBeInTheDocument();
  rerender(<ReviewPage {...props({ stats: undefined })} />);
  expect(screen.queryByText(/Last 7 days/)).not.toBeInTheDocument();
});

test('loading, a failure with Try again, an empty list per tab, and a workspace with no chat channel', () => {
  const onRetry = vi.fn();
  const { rerender } = render(<ReviewPage {...props({ items: undefined, loading: true })} />);
  expect(screen.getByText('Loading cards…')).toHaveAttribute('role', 'status');
  rerender(<ReviewPage {...props({ items: undefined, failed: true, onRetry })} />);
  expect(screen.getByRole('alert')).toHaveTextContent('could not be loaded');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(onRetry).toHaveBeenCalled();
  rerender(<ReviewPage {...props({ items: [] })} />);
  expect(screen.getByText(/Nothing waits for a person/)).toBeInTheDocument();
  rerender(<ReviewPage {...props({ items: [], tab: 'decided' })} />);
  expect(screen.getByText('No card was decided yet.')).toBeInTheDocument();
  rerender(<ReviewPage {...props({ available: false, items: [] })} />);
  expect(screen.getByText(/Email and web forms open a conversation for every message/)).toBeInTheDocument();
  expect(screen.queryByRole('tablist')).not.toBeInTheDocument();
});

test('more cards load on request', () => {
  const onMore = vi.fn();
  render(<ReviewPage {...props({ hasMore: true, onMore })} />);
  fireEvent.click(screen.getByRole('button', { name: 'Show more' }));
  expect(onMore).toHaveBeenCalled();
});

test('one conversation reads in the singular', () => {
  render(<ReviewPage {...props({ stats: { days: 7, gatherings: 1, by_state: { created: 1 }, decided_by: {}, fallback: 0, ai_decisions: 0, ai_corrected: 0 } })} />);
  expect(screen.getByText('Last 7 days: 1 card, 1 became a conversation')).toBeInTheDocument();
});
