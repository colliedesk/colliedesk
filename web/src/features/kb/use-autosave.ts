import { useEffect, useRef } from 'react';

// Runs `run` `delay` ms after `changed` stops changing, and at once when the window loses focus,
// the tab is hidden or closed, or the browser comes back online. `leaving` is true when the page
// may be gone before an answer arrives, so the request should outlive it.
export function useAutosave(changed: string, run: (leaving: boolean) => void, delay = 3000) {
  const latest = useRef(run);
  latest.current = run;
  const first = useRef(true);

  useEffect(() => {
    if (first.current) {
      first.current = false;
      return undefined;
    }
    const timer = window.setTimeout(() => latest.current(false), delay);
    return () => window.clearTimeout(timer);
  }, [changed, delay]);

  useEffect(() => {
    const now = () => latest.current(false);
    const leave = () => latest.current(true);
    const hidden = () => { if (document.visibilityState === 'hidden') leave(); };
    window.addEventListener('blur', now);
    window.addEventListener('online', now);
    window.addEventListener('pagehide', leave);
    document.addEventListener('visibilitychange', hidden);
    return () => {
      window.removeEventListener('blur', now);
      window.removeEventListener('online', now);
      window.removeEventListener('pagehide', leave);
      document.removeEventListener('visibilitychange', hidden);
    };
  }, []);
}
