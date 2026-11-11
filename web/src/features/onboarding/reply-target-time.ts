// Reply target times as the setup's reply targets step shows them. A business day is Monday to Friday,
// 09:00 to 18:00: the hours the step saves, so a day of a preset is 9 hours (540 minutes).

const opens = 9 * 60;
const closes = 18 * 60;
export const businessDay = closes - opens;
const weekdays = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

// targetText writes a target the way the presets table does: whole business days, else hours, else minutes.
export function targetText(minutes: number): string {
  if (minutes % businessDay === 0) {
    const days = minutes / businessDay;
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
const isOpen = (weekday: number) => weekday < 5;

export interface Due { weekday: number; minute: number; daysLater: number; parts: { weekday: number; minutes: number }[] }

// dueAfter counts minutes of business time from a moment (weekday 0 is Monday, minute of the day, inside the
// open hours) and says when the target falls and how much of it each open day takes.
export function dueAfter(weekday: number, minute: number, minutes: number): Due {
  let day = weekday;
  let at = minute;
  let left = minutes;
  let daysLater = 0;
  const parts: Due['parts'] = [];
  for (;;) {
    if (isOpen(day % 7) && at < closes) {
      const start = Math.max(at, opens);
      const used = Math.min(left, closes - start);
      if (used > 0) parts.push({ weekday: day % 7, minutes: used });
      left -= used;
      if (left === 0) return { weekday: day % 7, minute: start + used, daysLater, parts };
    }
    day += 1;
    daysLater += 1;
    at = opens;
  }
}

// workedExample is the sentence that shows how business hours count: a Normal message on Friday at 16:30.
export function workedExample(normalMinutes: number, sentence: (arrives: string, due: string, parts: string) => string): string {
  const due = dueAfter(4, 16 * 60 + 30, normalMinutes);
  const parts = due.parts.length > 1 ? due.parts.map((part) => `${partText(part.minutes)} on ${weekdays[part.weekday]}`).join(', ') : '';
  return sentence('Friday at 16:30', `${weekdays[due.weekday]} at ${clock(due.minute)}`, parts);
}

// exampleDue is when a conversation that came in today (a Tuesday) at minute is due: a time today, tomorrow, or
// a weekday.
export function exampleDue(minute: number, minutes: number, tomorrow: (time: string) => string): string {
  const due = dueAfter(1, minute, minutes);
  const time = clock(due.minute);
  if (due.daysLater === 0) return time;
  return due.daysLater === 1 ? tomorrow(time) : `${weekdays[due.weekday]} ${time}`;
}
