import { useEffect, useState } from 'react';

const checkEveryMs = 60_000;

// useNow returns the current time, refreshed when the calendar day in `timeZone` changes. A page left
// open overnight, or a laptop that slept through midnight, would otherwise keep yesterday's "Today".
// A timer alone is not enough: browsers pause timers in hidden tabs, so coming back also re-checks.
export function useNow(timeZone: string): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const day = (value: Date) => {
      try { return new Intl.DateTimeFormat('en', { timeZone, year: 'numeric', month: 'numeric', day: 'numeric' }).format(value); } catch { return value.toDateString(); }
    };
    const check = () => setNow((current) => {
      const next = new Date();
      return day(next) === day(current) ? current : next;
    });
    const timer = setInterval(check, checkEveryMs);
    document.addEventListener('visibilitychange', check);
    window.addEventListener('focus', check);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', check);
      window.removeEventListener('focus', check);
    };
  }, [timeZone]);
  return now;
}

// One timer for the whole page ticks each minute while anything on it counts minutes (how late a reply is), and
// stops when nothing does. Coming back to a hidden tab ticks at once, since browsers pause timers there.
const minuteListeners = new Set<() => void>();
let minuteTimer: ReturnType<typeof setInterval> | undefined;
const tick = () => { for (const listener of minuteListeners) listener(); };

function onEveryMinute(listener: () => void): () => void {
  if (minuteListeners.size === 0) {
    minuteTimer = setInterval(tick, checkEveryMs);
    document.addEventListener('visibilitychange', tick);
    window.addEventListener('focus', tick);
  }
  minuteListeners.add(listener);
  return () => {
    minuteListeners.delete(listener);
    if (minuteListeners.size > 0) return;
    clearInterval(minuteTimer);
    document.removeEventListener('visibilitychange', tick);
    window.removeEventListener('focus', tick);
  };
}

// useMinuteNow returns the current time, refreshed every minute from the page's one shared timer: for text that
// counts minutes, such as "1 h 20 min late", and grows while the page stays open. A new day is a new minute too.
export function useMinuteNow(): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => onEveryMinute(() => setNow(new Date())), []);
  return now;
}
