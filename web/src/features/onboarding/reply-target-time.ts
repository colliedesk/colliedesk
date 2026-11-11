// Reply target times as the setup's reply targets step shows them, counted in the business hours a preset
// applies with (the server's reply_target_hours): a day of a preset is one business day of those hours.

const weekdayKeys = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;
const weekdays = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

export interface BusinessHours { day: string; open: boolean; from?: string; to?: string }

// Open minutes of each weekday, Monday first; a closed day is null.
type Week = ({ opens: number; closes: number } | null)[];
const minutesOf = (clock: string) => Number(clock.slice(0, 2)) * 60 + Number(clock.slice(3, 5));
function weekOf(hours: BusinessHours[]): Week {
  return weekdayKeys.map((key) => {
    const day = hours.find((item) => item.day === key);
    if (!day?.open || !day.from || !day.to) return null;
    const opens = minutesOf(day.from);
    const closes = minutesOf(day.to);
    return closes > opens ? { opens, closes } : null;
  });
}

// targetText writes a target the way the presets table does: whole business days, else hours, else minutes.
export function targetText(minutes: number, day: number): string {
  if (minutes % day === 0) {
    const days = minutes / day;
    return `${days} ${days === 1 ? 'day' : 'days'}`;
  }
  return minutes % 60 === 0 ? `${minutes / 60} h` : `${minutes} min`;
}

// spokenMinutes writes a wall-clock length: 45 min, 3 h, 3 h 10 min.
export function spokenMinutes(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (hours === 0) return `${rest} min`;
  return rest === 0 ? `${hours} h` : `${hours} h ${rest} min`;
}

// partText writes the business time spent on one day: 30 min, 7 h, 1 h 30.
function partText(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (hours === 0) return `${rest} min`;
  return rest === 0 ? `${hours} h` : `${hours} h ${String(rest).padStart(2, '0')}`;
}

const clock = (minute: number) => `${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}`;

export interface Due { weekday: number; minute: number; daysLater: number; parts: { weekday: number; minutes: number }[] }

// dueAfter counts minutes of business time from a moment (weekday 0 is Monday, minute of the day) and says when
// the target falls and how much of it each open day takes. Undefined when the hours have no open day.
export function dueAfter(hours: BusinessHours[], weekday: number, minute: number, minutes: number): Due | undefined {
  const week = weekOf(hours);
  if (!week.some(Boolean)) return undefined;
  let day = weekday;
  let at = minute;
  let left = minutes;
  let daysLater = 0;
  const parts: Due['parts'] = [];
  for (;;) {
    const open = week[day % 7];
    if (open && at < open.closes) {
      const start = Math.max(at, open.opens);
      const used = Math.min(left, open.closes - start);
      if (used > 0) parts.push({ weekday: day % 7, minutes: used });
      left -= used;
      if (left === 0) return { weekday: day % 7, minute: start + used, daysLater, parts };
    }
    day += 1;
    daysLater += 1;
    at = 0;
  }
}

// workedExample is the sentence that shows how business hours count: a Normal message on Friday at 16:30. It
// is left out when Friday is not open at 16:30 in these hours, where the example would not hold.
export function workedExample(hours: BusinessHours[], normalMinutes: number, sentence: (arrives: string, due: string, parts: string) => string): string | undefined {
  const friday = weekOf(hours)[4];
  const arrives = 16 * 60 + 30;
  if (!friday || arrives < friday.opens || arrives >= friday.closes) return undefined;
  const due = dueAfter(hours, 4, arrives, normalMinutes);
  if (!due) return undefined;
  const parts = due.parts.length > 1 ? due.parts.map((part) => `${partText(part.minutes)} on ${weekdays[part.weekday]}`).join(', ') : '';
  return sentence('Friday at 16:30', `${weekdays[due.weekday]} at ${clock(due.minute)}`, parts);
}

// exampleDue is when a conversation that came in today (a Tuesday) at minute is due: a time today, tomorrow, or
// a weekday.
export function exampleDue(hours: BusinessHours[], minute: number, minutes: number, tomorrow: (time: string) => string): string {
  const due = dueAfter(hours, 1, minute, minutes);
  if (!due) return '—';
  const time = clock(due.minute);
  if (due.daysLater === 0) return time;
  return due.daysLater === 1 ? tomorrow(time) : `${weekdays[due.weekday]} ${time}`;
}
