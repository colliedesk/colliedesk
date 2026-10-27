import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { FixDetail } from './autofix-api';
import { AutofixBlockView } from './autofix-block';
import type { AutofixBlockProps } from './autofix-block';
import { detailOf, eventOf, now, repoOf, withPR } from './autofix.test.fixture';

const settingsLink = (label: string) => <a href="/settings/autofix">{label}</a>;

function show(fix: FixDetail | null, props: Partial<AutofixBlockProps> = {}) {
  const onAction = vi.fn();
  const onSend = vi.fn();
  render(<AutofixBlockView load="ready" fix={fix} target={{ repo: repoOf(), connector: 'claude_issue' }} customerName="Tomás Vidal" timezone="UTC" now={now}
    onSend={onSend} onAction={onAction} onRetryLoad={() => undefined} settingsLink={settingsLink} {...props} />);
  return { onAction, onSend, block: screen.getByRole('region', { name: 'Autofix' }) };
}

describe('AutofixBlockView', () => {
  it('offers to send a conversation that has no fix, with the repository and agent it would use', () => {
    const { onSend, block } = show(null);
    expect(within(block).getByText('Not sent')).toBeInTheDocument();
    expect(within(block).getByText('northwind/web')).toBeInTheDocument();
    expect(within(block).getByText('Claude')).toBeInTheDocument();
    fireEvent.click(within(block).getByRole('button', { name: 'Send to agent' }));
    expect(onSend).toHaveBeenCalled();
  });

  it('sends people to Settings when no repository or agent is ready', () => {
    show(null, { target: {} });
    expect(screen.getByText(/No repository is connected yet/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open Autofix settings' })).toHaveAttribute('href', '/settings/autofix');
    expect(screen.queryByRole('button', { name: 'Send to agent' })).not.toBeInTheDocument();
  });

  it('shows a sent fix with its issue, who created it and a way out', () => {
    const { onAction, block } = show(detailOf({ state: 'sent' }, [eventOf({ kind: 'created', actor_kind: 'person', actor_name: 'Mai Tran', to_state: 'sending', from_state: null })]));
    expect(within(block).getByText('Sent')).toBeInTheDocument();
    expect(within(block).getByRole('link', { name: /#87 CSV export stops at 500 rows/ })).toHaveAttribute('href', 'https://github.com/northwind/web/issues/87');
    expect(within(block).getByText(/created by @mai-tran/)).toBeInTheDocument();
    expect(within(block).getByRole('link', { name: 'Open issue' })).toHaveAttribute('target', '_blank');
    expect(within(block).getByText(/reviewed the text and sent it/)).toBeInTheDocument();
    fireEvent.click(within(block).getByRole('button', { name: 'Cancel' }));
    expect(onAction).toHaveBeenCalledWith('cancel');
  });

  it('explains a fix stuck waiting for GitHub and offers to send it again', () => {
    const { onAction } = show(detailOf({ state: 'sending', issue_number: null, issue_url: null, vendor_url: null, state_changed_at: '2026-10-27T11:55:00Z' }));
    expect(screen.getByText(/If the GitHub page was closed/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry (attempt 2)' }));
    expect(onAction).toHaveBeenCalledWith('retry');
  });

  it('turns a silent fix to attention with the checklist of what to look at', () => {
    show(detailOf({ state: 'sent', stale: true, state_changed_at: '2026-10-27T11:26:00Z' }));
    expect(screen.getByText('No word 34 min')).toBeInTheDocument();
    expect(screen.getByText(/ANTHROPIC_API_KEY/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Link a PR' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open issue #87' })).toBeInTheDocument();
  });

  it('asks a person to assign Copilot on GitHub in the fallback mode', () => {
    show(detailOf({ connector: 'copilot_issue', copilot_assignment: 'on_github' }));
    expect(screen.getByRole('link', { name: 'Assign to Copilot on GitHub' })).toHaveAttribute('href', 'https://github.com/northwind/web/issues/87');
  });

  it('marks a merged fix released, then drafts the reply or takes the release back', () => {
    const merged = show(detailOf({ state: 'merged', ...withPR, merge_sha: 'a1b2c3d4e5', merged_by_login: 'lena-dev', merged_at: '2026-10-27T11:12:00Z' }));
    expect(screen.getByText('a1b2c3d')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Mark as released' }));
    expect(merged.onAction).toHaveBeenCalledWith('mark_released');
  });

  it('after a release offers Draft reply and Unmark released', () => {
    const { onAction } = show(detailOf({ state: 'deployed', ...withPR, deployed_at: '2026-10-27T11:05:00Z', deployed_by_name: 'Mai Tran' }));
    expect(screen.getByText('Released', { selector: '.status' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Draft reply' }));
    fireEvent.click(screen.getByRole('button', { name: 'Unmark released' }));
    expect(onAction.mock.calls).toEqual([['draft_reply'], ['unmark_released']]);
  });

  it('says why a fix failed in words, with the agent’s own line, and offers the next attempt', () => {
    const fix = detailOf({ state: 'failed', outcome_reason: 'agent_error' }, [eventOf({ id: 4, to_state: 'failed', from_state: 'working', actor_name: 'claude[bot]', data: { summary: 'Could not run the tests' } })]);
    const { onAction } = show(fix);
    expect(screen.getByText(/Claude stopped with an error/)).toBeInTheDocument();
    expect(screen.getAllByText(/Could not run the tests/).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: 'Retry (attempt 2)' }));
    expect(onAction).toHaveBeenCalledWith('retry');
  });

  it('says a cancelled issue may still run, and that an open pull request stays open', () => {
    show(detailOf({ state: 'cancelled', outcome_reason: 'cancelled_by_person', ...withPR }, [eventOf({ kind: 'cancel', actor_kind: 'person', actor_name: 'Mai Tran' })]));
    expect(screen.getByText(/Cancelled by Mai Tran/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'stop it on GitHub' })).toBeInTheDocument();
    expect(screen.getByText(/stays open: Colliedesk can’t close pull requests/)).toBeInTheDocument();
  });

  it('says when Cursor did not stop a cancelled run, with where to stop it', () => {
    show(detailOf({ state: 'cancelled', outcome_reason: 'cancelled_by_person', connector: 'cursor_api', issue_number: null, issue_url: null, vendor_url: 'https://cursor.com/agents?id=bc-1' },
      [eventOf({ kind: 'cancel', actor_kind: 'person', actor_name: 'Mai Tran' }), eventOf({ id: 2, kind: 'cursor_cancel_failed', actor_kind: 'system', to_state: null, data: { summary: 'Cursor did not stop the run.', vendor_url: 'https://cursor.com/agents?id=bc-1' } })]));
    expect(screen.getByRole('link', { name: 'stop it on Cursor' })).toHaveAttribute('href', 'https://cursor.com/agents?id=bc-1');
    expect(screen.queryByText('The Cursor run was stopped.')).not.toBeInTheDocument();
  });

  it('offers to send again when nothing left Colliedesk', () => {
    const { onAction } = show(detailOf({ state: 'cancelled', outcome_reason: 'authorization_abandoned', issue_number: null, issue_url: null, vendor_url: null, github_login: null }));
    expect(screen.getByText(/not finished within 15 minutes/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Send again' }));
    expect(onAction).toHaveBeenCalledWith('send_again');
  });

  it('names the AI agent’s request when a token sent it', () => {
    show(detailOf({ created_by_kind: 'token', approved_request_id: '77777777-7777-4777-8777-777777777777' }));
    expect(screen.getByText(/approved an AI agent’s request/)).toBeInTheDocument();
  });

  it('never links an address that is not https', () => {
    show(detailOf({ state: 'pr_open', pr_number: 9, pr_url: 'javascript:alert(1)', pr_linked_by: 'auto' }));
    expect(screen.queryByRole('link', { name: /#9/ })).not.toBeInTheDocument();
  });

  it('keeps the newest five events and shows the rest on request', () => {
    const events = Array.from({ length: 7 }, (_, i) => eventOf({ id: i + 1, kind: 'signal_noted', to_state: null, data: { summary: `line ${i + 1}` }, created_at: `2026-10-27T11:0${i}:00Z` }));
    show(detailOf({ state: 'working' }, events));
    const list = screen.getByRole('list', { name: 'Activity, newest first' });
    expect(within(list).getAllByRole('listitem')).toHaveLength(5);
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent('line 7');
    fireEvent.click(screen.getByRole('button', { name: 'All 7' }));
    expect(within(list).getAllByRole('listitem')).toHaveLength(7);
  });

  it('says when Autofix did not load and lets people try again', () => {
    const onRetryLoad = vi.fn();
    render(<AutofixBlockView load="error" fix={null} target={{}} timezone="UTC" now={now} onSend={vi.fn()} onAction={vi.fn()} onRetryLoad={onRetryLoad} settingsLink={settingsLink} />);
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetryLoad).toHaveBeenCalled();
  });
});
