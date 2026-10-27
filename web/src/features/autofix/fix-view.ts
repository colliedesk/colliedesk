import type { Fix, FixEvent } from './autofix-api';
import { autofixMessages as m } from './autofix-messages';

// The rules of what a fix shows, from the fix the API gives: where it is on the track, what the badge says, its
// tone, and the ways in and out it offers (plan §2.5). The screens only render this.

export type Mark = 'todo' | 'done' | 'now' | 'hold' | 'halt' | 'cut' | 'skip' | 'live';
export interface Stop { label: string; mark: Mark }
export type Tone = 'off' | 'running' | 'attention' | 'done' | 'stopped';
export type FixAction = 'cancel' | 'link_pr' | 'unlink_pr' | 'assign_on_github' | 'mark_released' | 'unmark_released' | 'draft_reply'
  | 'retry' | 'send_again' | 'replace_key' | 'settings';

export interface FixView {
  badge: string;
  tone: Tone;
  track: Stop[];
  // The track passed a stop with no sign of it (a pull request with no sign the agent ran).
  skipped: boolean;
  // Buttons in order; the first is the primary one where a state has one.
  actions: FixAction[];
  primary?: FixAction;
}

const minuteMs = 60_000;
export const minutesIn = (fix: Fix, now: number) => Math.max(1, Math.floor((now - Date.parse(fix.state_changed_at)) / minuteMs));

// reachedVendor: the vendor took the fix (an issue or a Cursor run exists), so a failure happened after sending.
export const reachedVendor = (fix: Fix) => fix.issue_number !== null || fix.vendor_url !== null;
// waitsForAssignment: Copilot in the workspace's fallback mode, waiting for a person to assign it on GitHub.
export const waitsForAssignment = (fix: Fix) => fix.state === 'sent' && fix.copilot_assignment === 'on_github';
// agentRan: a sign the agent ran came before the pull request. Without the timeline (a list row) it counts as ran.
const agentRan = (fix: Fix, events?: FixEvent[]) => events === undefined || fix.connector === 'cursor_api' && fix.vendor_url !== null
  || events.some((event) => event.to_state === 'working');

function track(fix: Fix, events: FixEvent[] | undefined): { stops: Stop[]; skipped: boolean } {
  const t = m.track;
  const labels = [t.sent, t.agent, t.pr, t.merged, t.released];
  const at = (index: number, mark: Mark, label?: string): Stop[] =>
    labels.map((name, i) => ({ label: i === index && label ? label : name, mark: i < index ? 'done' : i === index ? mark : 'todo' }));
  const skipAgent = (stops: Stop[]) => {
    if (agentRan(fix, events)) return { stops, skipped: false };
    stops[1] = { ...stops[1], mark: 'skip' };
    return { stops, skipped: true };
  };
  switch (fix.state) {
    case 'sending': return { stops: at(0, 'now', t.sending), skipped: false };
    case 'sent':
      if (waitsForAssignment(fix)) return { stops: at(1, 'hold', t.assign), skipped: false };
      return { stops: fix.stale ? at(1, 'hold', t.noWord) : at(0, 'now'), skipped: false };
    case 'working': return { stops: at(1, 'now'), skipped: false };
    case 'pr_open': return skipAgent(at(2, 'now'));
    case 'merged': return skipAgent(at(3, 'now'));
    case 'deployed': return skipAgent(at(4, 'live'));
    case 'failed':
      if (fix.outcome_reason === 'pr_closed') return { stops: at(2, 'halt', t.prClosed), skipped: false };
      return { stops: reachedVendor(fix) ? at(1, 'halt', t.failed) : at(0, 'halt', t.notSent), skipped: false };
    case 'cancelled':
      if (!reachedVendor(fix)) return { stops: at(0, 'cut', t.notSent), skipped: false };
      return { stops: fix.pr_number !== null ? at(2, 'cut', t.cancelled) : at(1, 'cut', t.cancelled), skipped: false };
  }
}

function badge(fix: Fix, now: number): string {
  const b = m.badge;
  if (fix.state === 'sent') return waitsForAssignment(fix) ? b.assign : fix.stale ? b.noWord(minutesIn(fix, now)) : b.sent;
  return b[fix.state];
}

function tone(fix: Fix): Tone {
  switch (fix.state) {
    case 'sent': return fix.stale || waitsForAssignment(fix) ? 'attention' : 'running';
    case 'deployed': return 'done';
    case 'failed': return 'stopped';
    case 'cancelled': return 'off';
    default: return 'running';
  }
}

// actions: every way in has a way out (plan §2.5), in the order the block shows them.
function actions(fix: Fix): { actions: FixAction[]; primary?: FixAction } {
  switch (fix.state) {
    case 'sending': return { actions: ['cancel'] };
    case 'sent':
      if (waitsForAssignment(fix)) return { actions: ['assign_on_github', 'cancel'], primary: 'assign_on_github' };
      return { actions: fix.stale ? ['link_pr', 'cancel'] : ['cancel'] };
    case 'working': return { actions: ['link_pr', 'cancel'] };
    case 'pr_open': return { actions: fix.pr_linked_by === 'person' ? ['unlink_pr', 'cancel'] : ['cancel'] };
    case 'merged': return { actions: ['mark_released'], primary: 'mark_released' };
    case 'deployed': return { actions: ['draft_reply', 'unmark_released'], primary: 'draft_reply' };
    case 'failed':
      switch (fix.outcome_reason) {
        case 'pr_closed': return { actions: ['retry', 'link_pr'], primary: 'retry' };
        case 'repo_public': return { actions: ['settings'] };
        case 'vendor_auth': return { actions: ['retry', 'replace_key'] };
        default: return { actions: ['retry'], primary: 'retry' };
      }
    case 'cancelled':
      // Nothing left Colliedesk: the same text goes again.
      if (!reachedVendor(fix)) return { actions: ['send_again'], primary: 'send_again' };
      return { actions: ['retry'] };
  }
}

export function fixView(fix: Fix, now: number, events?: FixEvent[]): FixView {
  const shown = track(fix, events);
  return { badge: badge(fix, now), tone: tone(fix), track: shown.stops, skipped: shown.skipped, ...actions(fix) };
}

// stripText is the one line under the thread header on narrow screens.
export function stripText(fix: Fix, now: number, formatTime: (value: string) => string, customer?: string): string {
  const s = m.strip;
  switch (fix.state) {
    case 'sending': return s.sending(fix.connector);
    case 'sent':
      if (waitsForAssignment(fix)) return s.assign(fix.issue_number);
      return fix.stale ? s.noWord(fix.connector, minutesIn(fix, now)) : s.sent(fix.issue_number, fix.connector);
    case 'working': return s.working(fix.connector, fix.issue_number);
    case 'pr_open': return s.prOpen(fix.pr_number);
    case 'merged': return s.merged(fix.pr_number);
    case 'deployed': return s.released(formatTime(fix.deployed_at ?? fix.state_changed_at), customer);
    case 'failed': return fix.outcome_reason === 'pr_closed' ? m.failedTitle.pr_closed : m.reasons[fix.outcome_reason ?? 'agent_error'].split('. ')[0];
    case 'cancelled': return fix.outcome_reason === 'cancelled_by_person' || !fix.outcome_reason ? m.badge.cancelled : m.reasons[fix.outcome_reason].split('. ')[0];
  }
}

// shortId is the fix's id as people say it: its first four characters.
export const shortId = (id: string) => id.replace(/[^0-9a-z]/gi, '').slice(0, 4).toLowerCase();

// safeHref keeps a link only when it is https: an address a vendor reported never opens anything else.
export function safeHref(url: string | null | undefined): string | undefined {
  if (!url) return undefined;
  try {
    return new URL(url).protocol === 'https:' ? url : undefined;
  } catch {
    return undefined;
  }
}
