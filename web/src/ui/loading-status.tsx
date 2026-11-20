import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { messages } from '../messages';

// After this long a wait is no longer normal: the server answers in milliseconds, so the path to the browser is slow.
export const slowLoadAfterMs = 5_000;

interface LoadingStatusProps {
  text?: string;
  // Starts the request again. A stuck first request is not replaced by a plain refetch; see lib/restart-query.ts.
  onRetry?: () => void;
  // The page that is coming, drawn in grey blocks (ui/skeleton.tsx): it holds the layout, so nothing jumps
  // when the content arrives. The words stay for screen readers until the wait is slow.
  shape?: ReactNode;
}

// A loading status that says so out loud when the wait goes on, instead of looking like the app is broken.
export function LoadingStatus({ text = messages.loading, onRetry, shape }: LoadingStatusProps) {
  const [attempt, setAttempt] = useState(0);
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    setSlow(false);
    const timer = setTimeout(() => setSlow(true), slowLoadAfterMs);
    return () => clearTimeout(timer);
  }, [attempt]);
  const retry = () => {
    setAttempt((value) => value + 1);
    onRetry?.();
  };
  return <div className={shape ? 'loading-shape' : 'empty-copy'} aria-busy={shape ? true : undefined}>
    <p role="status" className={shape && !slow ? 'sr-only' : undefined}>{text}{slow && <> <span className="muted-copy">{messages.slowConnection}</span></>}</p>
    {slow && onRetry && <button type="button" onClick={retry}>{messages.tryAgain}</button>}
    {shape && <div aria-hidden="true">{shape}</div>}
  </div>;
}
