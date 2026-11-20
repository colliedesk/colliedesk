import { useEffect, useState } from 'react';
import { messages } from '../messages';

// After this long a wait is no longer normal: the server answers in milliseconds, so the path to the browser is slow.
export const slowLoadAfterMs = 5_000;

interface LoadingStatusProps {
  text?: string;
  // Starts the request again. A stuck first request is not replaced by a plain refetch; see lib/restart-query.ts.
  onRetry?: () => void;
}

// A loading status that says so out loud when the wait goes on, instead of looking like the app is broken.
export function LoadingStatus({ text = messages.loading, onRetry }: LoadingStatusProps) {
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
  return <div className="empty-copy">
    <p role="status">{text}{slow && <> <span className="muted-copy">{messages.slowConnection}</span></>}</p>
    {slow && onRetry && <button type="button" onClick={retry}>{messages.tryAgain}</button>}
  </div>;
}
