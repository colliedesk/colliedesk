import type { SLA, SLAClock } from '../api/schemas';
import { browserTimeZone, zoneSuffix } from './format-date';

// A conversation's response targets as the list and the header draw them (docs/internals/design.md):
// a mark and words only when a clock is due soon or overdue, never a fill. The server counts business
// hours and says which clock is due soon or late; the client only words it, so it never guesses a calendar.

export type ClockName = 'first_response' | 'resolution';
export interface SLAMark {
  tone: 'soon' | 'late';
  clock: ClockName;
  // short is the list's words ("Due 13:15", "Overdue"); header the header's ("Reply by 11:30", "Resolve overdue");
  // full is the accessible name and the tooltip, with the clock and that it counts business hours.
  short: string;
  header: string;
  full: string;
}

function format(value: Date, timeZone: string, options: Intl.DateTimeFormatOptions): string {
  try {
    return new Intl.DateTimeFormat('en', { ...options, timeZone }).format(value).replace(/\s/g, ' ');
  } catch {
    return new Intl.DateTimeFormat('en', { ...options, timeZone: 'UTC' }).format(value).replace(/\s/g, ' ');
  }
}

const day = (value: Date, timeZone: string) => format(value, timeZone, { year: 'numeric', month: 'numeric', day: 'numeric' });

// when is a moment near now in the agent's time zone: its time today, its weekday and time within a week,
// its date otherwise. Absolute, never a countdown: nothing on a row ticks. Read in another zone than the
// browser's clock, the time names its zone ("6:15 AM UTC"), as every other stamp of the inbox does.
export function when(value: string, timeZone: string, now = new Date(), browser = browserTimeZone()): string {
  const date = new Date(value);
  const time = format(date, timeZone, { hour: 'numeric', minute: '2-digit' }) + zoneSuffix(value, timeZone, browser);
  if (day(date, timeZone) === day(now, timeZone)) return time;
  const days = Math.abs(date.getTime() - now.getTime()) / 86_400_000;
  return days < 6 ? `${format(date, timeZone, { weekday: 'short' })} ${time}` : format(date, timeZone, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
}

const today = (value: string, timeZone: string, now: Date) => day(new Date(value), timeZone) === day(now, timeZone);

// duration words a number of minutes: "45 min", "1 h 20 min", "3 h".
export function duration(minutes: number): string {
  const total = Math.abs(Math.round(minutes));
  const hours = Math.floor(total / 60);
  const rest = total % 60;
  return hours === 0 ? `${rest} min` : rest === 0 ? `${hours} h` : `${hours} h ${rest} min`;
}

const clockWords: Record<ClockName, { name: string; verb: string }> = {
  first_response: { name: 'First reply', verb: 'Reply' },
  resolution: { name: 'Resolve', verb: 'Resolve' },
};

// slaOf reads a conversation's targets (none from an older server).
export const slaOf = (conversation: { sla?: SLA | null }): SLA | null | undefined => conversation.sla;

// slaMark is what a row and the header draw: the late clock first, then the one due soon; nothing otherwise.
export function slaMark(sla: SLA | null | undefined, timeZone: string, now = new Date(), browser = browserTimeZone()): SLAMark | undefined {
  if (!sla) return undefined;
  const clocks: [ClockName, SLAClock | null][] = [['first_response', sla.first_response], ['resolution', sla.resolution]];
  const late = clocks.find(([, clock]) => clock?.overdue);
  const soon = clocks.find(([, clock]) => clock?.due_soon && !clock.overdue && clock.due_at);
  const [name, clock] = late ?? soon ?? [];
  if (!name || !clock) return undefined;
  const words = clockWords[name];
  if (clock.overdue) {
    const since = clock.breached_at ?? clock.due_at;
    return { tone: 'late', clock: name, short: 'Overdue', header: `${words.verb} overdue`,
      full: `${words.name} overdue${since ? ` since ${when(since, timeZone, now, browser)}` : ''}, counted in business hours` };
  }
  const due = clock.due_at as string;
  const at = when(due, timeZone, now, browser);
  return { tone: 'soon', clock: name, short: `Due ${at}`, header: `${words.verb} by ${at}`,
    full: `${words.name} due ${at}${today(due, timeZone, now) ? ' today' : ''}, counted in business hours` };
}

// clockLine is a clock's line in the details: what it says and, while it runs, the business time left or late.
export function clockLine(clock: SLAClock | null, timeZone: string, now = new Date(), browser = browserTimeZone()): { text: string; detail?: string; tone?: 'soon' | 'late' | 'met' | 'quiet' } {
  if (!clock) return { text: 'No target', tone: 'quiet' };
  if (clock.state === 'met' && clock.met_at) return { text: `Met ${when(clock.met_at, timeZone, now, browser)}`, tone: 'met' };
  if (clock.state === 'paused') return { text: 'Paused while pending', tone: 'quiet' };
  if (clock.state === 'stopped') return { text: 'Stopped while resolved or closed', tone: 'quiet' };
  const left = clock.business_minutes_left;
  if (clock.overdue) {
    const since = clock.breached_at ?? clock.due_at;
    return { text: since ? `Overdue since ${when(since, timeZone, now, browser)}` : 'Overdue', detail: left === null ? undefined : `${duration(left)} of business time late`, tone: 'late' };
  }
  if (!clock.due_at) return { text: 'No target', tone: 'quiet' };
  return { text: `Due ${when(clock.due_at, timeZone, now, browser)}${today(clock.due_at, timeZone, now) ? ' today' : ''}`,
    detail: left === null ? undefined : `${duration(left)} of business time left`, tone: clock.due_soon ? 'soon' : undefined };
}

// nextChange is the soonest moment a listed conversation's mark may change (a running clock's warning or
// deadline still ahead): the inbox reads the list again then, and the server says what changed.
export function nextChange(items: { sla?: SLA | null }[], now = new Date()): Date | undefined {
  let soonest: number | undefined;
  for (const item of items) {
    for (const clock of [item.sla?.first_response, item.sla?.resolution]) {
      if (!clock || clock.state !== 'running') continue;
      for (const at of [clock.warn_at, clock.due_at]) {
        const time = at ? Date.parse(at) : Number.NaN;
        if (time > now.getTime() && (soonest === undefined || time < soonest)) soonest = time;
      }
    }
  }
  return soonest === undefined ? undefined : new Date(soonest);
}
