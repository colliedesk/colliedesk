import { useState } from 'react';
import { Dialog, Field } from '../../ui/dialog';
import type { Fix } from './autofix-api';
import { autofixMessages as m } from './autofix-messages';

// CancelDialog says what cancelling does at the vendor before it happens: an issue is closed but a run may go on,
// a pull request stays open.
export function CancelDialog({ fix, busy, problem, onCancel, onClose }: { fix: Fix; busy: boolean; problem?: string; onCancel: () => void; onClose: () => void }) {
  const help = fix.state === 'sending' ? m.cancelSendingHelp : fix.connector === 'cursor_api' ? m.cancelCursorHelp : m.cancelIssueHelp(fix.issue_number, fix.connector);
  return <Dialog alert className="af-dialog" title={m.cancelTitle} help={fix.pr_number !== null ? `${help} ${m.cancelPRHelp}` : help} closeLabel={m.close} onClose={onClose} onSubmit={onCancel}
    footer={<><button type="button" className="af-ghost" onClick={onClose}>{m.keepIt}</button>
      <button type="submit" className="af-danger-solid" disabled={busy}>{m.cancelFix}</button></>}>
    <p className="af-help">{m.cancelKeep}</p>
    {problem && <p role="alert" className="error-copy">{problem}</p>}
  </Dialog>;
}

const pullRequest = /^https:\/\/github\.com\/([^/\s]+\/[^/\s]+)\/pull\/(\d+)\/?(?:[?#].*)?$/i;

// prRepoOf reads owner/name from a pull request's address, or undefined when it is not one.
export function prRepoOf(url: string): string | undefined {
  return pullRequest.exec(url.trim())?.[1];
}

// LinkDialog links a pull request by hand; the address must be a pull request of the fix's repository (the server
// checks it too: 400 pull_request_not_in_repo).
export function LinkDialog({ fix, busy, problem, onLink, onClose }: { fix: Fix; busy: boolean; problem?: string; onLink: (url: string) => void; onClose: () => void }) {
  const [url, setUrl] = useState('');
  const [error, setError] = useState<string>();
  const submit = () => {
    const found = prRepoOf(url);
    if (!found) return setError(m.notAPR);
    if (found.toLowerCase() !== fix.repo_full_name.toLowerCase()) return setError(m.wrongRepo(found, fix.repo_full_name));
    setError(undefined);
    onLink(url.trim());
  };
  return <Dialog className="af-dialog" title={m.linkTitle} help={m.linkHelp(fix.repo_full_name)} closeLabel={m.close} onClose={onClose} onSubmit={submit}
    footer={<><button type="button" className="af-ghost" onClick={onClose}>{m.cancel}</button>
      <button type="submit" className="primary-action" disabled={busy}>{m.linkPRSubmit}</button></>}>
    <Field id="af-link-url" label={m.prURL} error={error ?? problem}>{(props) => <input {...props} type="url" value={url} autoFocus inputMode="url"
      placeholder={`https://github.com/${fix.repo_full_name}/pull/…`} onChange={(event) => setUrl(event.target.value)} />}</Field>
  </Dialog>;
}
