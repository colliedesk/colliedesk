import { useState } from 'react';
import { m } from './messages';

// CommandLine shows a command to run on the server, with a copy button.
export function CommandLine({ command, label = m.copy }: { command: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="copy-field">
      <code className="ops-command">{command}</code>
      <button type="button" onClick={() => { void navigator.clipboard?.writeText(command).then(() => setCopied(true), () => {}); }}>{copied ? m.copied : label}</button>
    </div>
  );
}

// when formats a UTC time in the browser's time zone (the operator has no workspace time zone).
export function when(value?: string | null): string {
  if (!value) return '';
  const date = new Date(value);
  return date.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}
