import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Fix, FixGroup } from './autofix-api';
import { fixOf, now, repoOf, withPR } from './autofix.test.fixture';
import { FixesPage } from './fixes-page';
import type { FixGroupRows } from './fixes-page';

const rows = (items: Fix[], more = false): FixGroupRows => ({ items, load: 'ready', more, loadingMore: false, onMore: vi.fn() });
const link = (id: string, label: string) => <a href={`/conversations/${id}`}>{label}</a>;
const named = (label: string) => <a href="/x">{label}</a>;

function show(groups: Record<FixGroup, FixGroupRows>, props: Partial<Parameters<typeof FixesPage>[0]> = {}) {
  const onFilter = vi.fn();
  render(<FixesPage groups={groups} counts={{ needs_person: 2, running: 1 }} repos={[repoOf()]} onFilter={onFilter} timezone="UTC" now={now} onRetry={vi.fn()}
    conversationLink={link} settingsLink={named} inboxLink={named} {...props} />);
  return { onFilter };
}

describe('FixesPage', () => {
  it('lists fixes in three groups with the conversation, state and why', () => {
    show({
      needs_person: rows([fixOf({ id: '9b020000-0000-4000-8000-000000000001', state: 'failed', outcome_reason: 'pr_closed', ...withPR, group: 'needs_person' }),
        fixOf({ id: '2e440000-0000-4000-8000-000000000002', state: 'merged', ...withPR, connector: 'cursor_api', group: 'needs_person', conversation_subject: 'Signatures break' })]),
      running: rows([fixOf({ state: 'working', group: 'running' })]),
      done: rows([fixOf({ id: '7a210000-0000-4000-8000-000000000003', state: 'deployed', deployed_at: '2026-10-27T11:05:00Z', group: 'done', created_by_kind: 'token' })]),
    });
    const needs = screen.getByRole('region', { name: /Needs a person/ });
    expect(within(needs).getByText('2')).toBeInTheDocument();
    expect(within(needs).getByText('PR #412 closed without merge')).toBeInTheDocument();
    expect(within(needs).getByRole('link', { name: 'Signatures break' })).toHaveAttribute('href', '/conversations/11111111-1111-4111-8111-111111111111');
    expect(within(needs).getByText('not marked released')).toBeInTheDocument();
    expect(within(screen.getByRole('region', { name: /Running/ })).getByText('Agent running')).toBeInTheDocument();
    const done = screen.getByRole('region', { name: /Done/ });
    expect(within(done).getByText('Released')).toBeInTheDocument();
    expect(within(done).getByText('AI agent · approved by Mai Tran')).toBeInTheDocument();
  });

  it('filters by repository and agent on the server', () => {
    const { onFilter } = show({ needs_person: rows([fixOf()]), running: rows([]), done: rows([]) });
    fireEvent.change(screen.getByLabelText('Agent'), { target: { value: 'cursor_api' } });
    expect(onFilter).toHaveBeenCalledWith(undefined, 'cursor_api');
    fireEvent.change(screen.getByLabelText('Repository'), { target: { value: repoOf().id } });
    expect(onFilter).toHaveBeenCalledWith(repoOf().id, undefined);
  });

  it('says what to do when nothing was sent, and when a filter finds nothing', () => {
    const empty = { needs_person: rows([]), running: rows([]), done: rows([]) };
    const first = render(<FixesPage groups={empty} repos={[repoOf()]} onFilter={vi.fn()} timezone="UTC" now={now} onRetry={vi.fn()} conversationLink={link} settingsLink={named} inboxLink={named} />);
    expect(screen.getByText('No fix sent yet')).toBeInTheDocument();
    first.unmount();
    const { onFilter } = show(empty, { connector: 'cursor_api', repoId: repoOf().id });
    expect(screen.getByText('No Cursor fix in northwind/web')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Show all' }));
    expect(onFilter).toHaveBeenCalledWith(undefined, undefined);
  });

  it('loads more of a group', () => {
    const groups = { needs_person: rows([fixOf()], true), running: rows([]), done: rows([]) };
    show(groups);
    fireEvent.click(screen.getByRole('button', { name: 'Show more' }));
    expect(groups.needs_person.onMore).toHaveBeenCalled();
  });

  it('says when the list did not load', () => {
    const onRetry = vi.fn();
    render(<FixesPage groups={{ needs_person: { ...rows([]), load: 'error' }, running: rows([]), done: rows([]) }} repos={[]} onFilter={vi.fn()} timezone="UTC" now={now}
      onRetry={onRetry} conversationLink={link} settingsLink={named} inboxLink={named} />);
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalled();
  });
});
