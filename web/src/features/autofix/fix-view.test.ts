import { describe, expect, it } from 'vitest';
import { eventOf, fixOf, now, withPR } from './autofix.test.fixture';
import { fixView, redirectHref, safeHref, stripText } from './fix-view';

const marks = (view: ReturnType<typeof fixView>) => view.track.map((stop) => `${stop.label}:${stop.mark}`).join(' ');
const time = (value: string) => value.slice(11, 16);

describe('fixView', () => {
  it('walks the five stops from sending to released', () => {
    expect(marks(fixView(fixOf({ state: 'sending', issue_number: null, vendor_url: null }), now))).toBe('sending:now agent:todo PR:todo merged:todo released:todo');
    expect(marks(fixView(fixOf({ state: 'sent' }), now))).toBe('sent:now agent:todo PR:todo merged:todo released:todo');
    expect(marks(fixView(fixOf({ state: 'working' }), now))).toBe('sent:done agent:now PR:todo merged:todo released:todo');
    expect(marks(fixView(fixOf({ state: 'merged', ...withPR }), now))).toBe('sent:done agent:done PR:done merged:now released:todo');
    const released = fixView(fixOf({ state: 'deployed', ...withPR }), now);
    expect(marks(released)).toBe('sent:done agent:done PR:done merged:done released:live');
    expect(released.badge).toBe('Released');
    expect(released.tone).toBe('done');
  });

  it('draws the agent stop striped when a pull request came with no sign the agent ran', () => {
    const fix = fixOf({ state: 'pr_open', ...withPR, pr_linked_by: 'person' });
    const silent = fixView(fix, now, [eventOf({ to_state: 'sent' }), eventOf({ id: 2, from_state: 'sent', to_state: 'pr_open' })]);
    expect(marks(silent)).toBe('sent:done agent:skip PR:now merged:todo released:todo');
    expect(silent.skipped).toBe(true);
    const ran = fixView(fix, now, [eventOf({ to_state: 'working' })]);
    expect(ran.track[1].mark).toBe('done');
    // A list row has no timeline: it never claims a skip.
    expect(fixView(fix, now).skipped).toBe(false);
  });

  it('turns a silent fix to attention without changing its state, and offers to link a PR', () => {
    const view = fixView(fixOf({ state: 'sent', stale: true, state_changed_at: '2026-10-27T11:26:00Z' }), now);
    expect(view.badge).toBe('No word 34 min');
    expect(view.tone).toBe('attention');
    expect(marks(view)).toBe('sent:done no word:hold PR:todo merged:todo released:todo');
    expect(view.actions).toEqual(['link_pr', 'cancel']);
  });

  it('asks a person to assign Copilot on GitHub in the fallback mode', () => {
    const view = fixView(fixOf({ connector: 'copilot_issue', copilot_assignment: 'on_github' }), now);
    expect(view.badge).toBe('Assign on GitHub');
    expect(view.primary).toBe('assign_on_github');
    expect(view.actions).toEqual(['assign_on_github', 'cancel']);
    expect(view.track[1]).toEqual({ label: 'assign', mark: 'hold' });
  });

  it('has a way out of every state', () => {
    expect(fixView(fixOf({ state: 'sending', state_changed_at: '2026-10-27T11:59:30Z' }), now).actions).toEqual(['cancel']);
    // Waiting on GitHub for minutes: the person who confirms may have closed it, so there is a way to send again.
    expect(fixView(fixOf({ state: 'sending', state_changed_at: '2026-10-27T11:57:00Z' }), now).actions).toEqual(['retry', 'cancel']);
    expect(fixView(fixOf({ state: 'sending', connector: 'cursor_api', state_changed_at: '2026-10-27T11:57:00Z' }), now).actions).toEqual(['cancel']);
    expect(fixView(fixOf({ state: 'working' }), now).actions).toEqual(['link_pr', 'cancel']);
    expect(fixView(fixOf({ state: 'pr_open', ...withPR }), now).actions).toEqual(['cancel']);
    expect(fixView(fixOf({ state: 'pr_open', ...withPR, pr_linked_by: 'person' }), now).actions).toEqual(['unlink_pr', 'cancel']);
    expect(fixView(fixOf({ state: 'merged', ...withPR }), now).actions).toEqual(['mark_released']);
    expect(fixView(fixOf({ state: 'deployed', ...withPR }), now).actions).toEqual(['draft_reply', 'unmark_released']);
    expect(fixView(fixOf({ state: 'failed', outcome_reason: 'agent_error' }), now).actions).toEqual(['retry']);
    expect(fixView(fixOf({ state: 'failed', outcome_reason: 'pr_closed', ...withPR }), now).actions).toEqual(['retry', 'link_pr']);
    expect(fixView(fixOf({ state: 'failed', outcome_reason: 'repo_public', issue_number: null, vendor_url: null }), now).actions).toEqual(['settings']);
    expect(fixView(fixOf({ state: 'failed', outcome_reason: 'vendor_auth', connector: 'cursor_api', issue_number: null, vendor_url: null }), now).actions).toEqual(['retry', 'replace_key']);
    expect(fixView(fixOf({ state: 'cancelled', outcome_reason: 'cancelled_by_person' }), now).actions).toEqual(['retry']);
    expect(fixView(fixOf({ state: 'cancelled', outcome_reason: 'authorization_abandoned', issue_number: null, vendor_url: null }), now).actions).toEqual(['send_again']);
    expect(fixView(fixOf({ state: 'cancelled', outcome_reason: 'connector_disabled', issue_number: null, vendor_url: null }), now).actions).toEqual(['send_again']);
  });

  it('marks where a fix stopped', () => {
    expect(marks(fixView(fixOf({ state: 'failed', outcome_reason: 'agent_error' }), now))).toBe('sent:done failed:halt PR:todo merged:todo released:todo');
    expect(marks(fixView(fixOf({ state: 'failed', outcome_reason: 'pr_closed', ...withPR }), now))).toBe('sent:done agent:done PR closed:halt merged:todo released:todo');
    expect(marks(fixView(fixOf({ state: 'failed', outcome_reason: 'repo_public', issue_number: null, vendor_url: null }), now))).toBe('not sent:halt agent:todo PR:todo merged:todo released:todo');
    expect(marks(fixView(fixOf({ state: 'cancelled', outcome_reason: 'cancelled_by_person' }), now))).toBe('sent:done cancelled:cut PR:todo merged:todo released:todo');
    expect(marks(fixView(fixOf({ state: 'cancelled', outcome_reason: 'cancelled_by_person', ...withPR }), now))).toBe('sent:done agent:done cancelled:cut merged:todo released:todo');
    expect(marks(fixView(fixOf({ state: 'cancelled', outcome_reason: 'authorization_abandoned', issue_number: null, vendor_url: null }), now))).toBe('not sent:cut agent:todo PR:todo merged:todo released:todo');
    expect(fixView(fixOf({ state: 'failed', outcome_reason: 'agent_error' }), now).tone).toBe('stopped');
    expect(fixView(fixOf({ state: 'cancelled', outcome_reason: 'issue_closed' }), now).tone).toBe('off');
  });
});

describe('stripText', () => {
  it('says the state in one line', () => {
    expect(stripText(fixOf({ state: 'sent' }), now, time)).toBe('Issue #87 · waiting for Claude');
    expect(stripText(fixOf({ state: 'sending', connector: 'cursor_api' }), now, time)).toBe('Sending to Cursor…');
    expect(stripText(fixOf({ state: 'deployed', ...withPR, deployed_at: '2026-10-27T14:05:00Z' }), now, time, 'Tomás')).toBe('Released 14:05 · reply to Tomás');
    expect(stripText(fixOf({ state: 'failed', outcome_reason: 'pr_closed' }), now, time)).toBe('PR closed without merge');
  });
});

describe('safeHref', () => {
  it('keeps only https addresses', () => {
    expect(safeHref('https://github.com/a/b/pull/1')).toBe('https://github.com/a/b/pull/1');
    expect(safeHref('javascript:alert(1)')).toBeUndefined();
    expect(safeHref('http://github.com')).toBeUndefined();
    expect(safeHref(null)).toBeUndefined();
  });
});

describe('redirectHref', () => {
  it('follows the server’s GitHub address over https, and over http only to this machine (a local stand-in)', () => {
    expect(redirectHref('https://github.com/login/oauth/authorize?state=s')).toBe('https://github.com/login/oauth/authorize?state=s');
    expect(redirectHref('http://127.0.0.1:8097/apps/x/installations/new')).toBe('http://127.0.0.1:8097/apps/x/installations/new');
    expect(redirectHref('http://localhost:8097/login/oauth/authorize')).toBe('http://localhost:8097/login/oauth/authorize');
    expect(redirectHref('http://[::1]:8097/login/oauth/authorize')).toBe('http://[::1]:8097/login/oauth/authorize');
    expect(redirectHref('http://github.com/login/oauth/authorize')).toBeUndefined();
    expect(redirectHref('http://127.0.0.1.example.com/x')).toBeUndefined();
    expect(redirectHref('javascript:alert(1)')).toBeUndefined();
    expect(redirectHref(null)).toBeUndefined();
  });
});
