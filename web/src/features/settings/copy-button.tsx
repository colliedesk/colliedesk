import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { agentTokenMessages as text } from './agent-tokens-messages';

// CopyButton puts value on the clipboard and says so next to itself for two seconds. A browser that
// gives the page no clipboard (a server on plain http) or refuses it gets the failure said, not silence.
// copiedText is what a copy says when it worked, where the page has its own words for it.
export function CopyButton({ value, label, className, children, copiedText = text.copied }: { value: string; label?: string; className?: string; children: ReactNode; copiedText?: string }) {
  const [state, setState] = useState<'copied' | 'failed' | null>(null);
  useEffect(() => {
    if (!state) return undefined;
    const timer = window.setTimeout(() => setState(null), 2000);
    return () => window.clearTimeout(timer);
  }, [state]);
  const copy = () => {
    const write = navigator.clipboard?.writeText(value) ?? Promise.reject(new Error('no clipboard'));
    write.then(() => setState('copied'), () => setState('failed'));
  };
  return <>
    <button type="button" className={className} aria-label={label} onClick={copy}>{children}</button>
    {state && <span role="status" className={state === 'failed' ? 'error-copy' : 'copied'}>{state === 'copied' ? copiedText : text.copyFailed}</span>}
  </>;
}
