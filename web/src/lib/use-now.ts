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
