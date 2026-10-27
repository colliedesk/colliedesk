import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import type { ReactNode } from 'react';
import { InvalidRequestError } from '../../api/client';
import { formatShortDate } from '../../lib/format-date';
import { Dialog, Field } from '../../ui/dialog';
import { autofixApi } from './autofix-api';
import type { ConnectorState, Fix, FixConnector, FixPreview, FixSent, Repo } from './autofix-api';
import { ExternalIcon, KeyIcon, ShieldIcon } from './autofix-icons';
import { autofixMessages as m } from './autofix-messages';
import { RedirectRefused } from './fix-view';

// The send dialog: pick the repository and agent, read exactly what leaves Colliedesk (masked on the server),
// edit it, and go. An issue goes through GitHub in this window; Cursor is sent by the server.

// The body marks each customer message and each picked note with a line before and after it, and writes what it
// took out as "[… removed]" (FixPreview.body in the contract). Only for showing them apart; the text sent is the
// body as edited.
const blockStart = /^<(customer-message|team-note)\b[^>]*>$/;
const blockEnd = /^<\/(customer-message|team-note)>$/;
const removedToken = /(\[[a-z -]+ removed\])/g;
// A mention or an issue reference the server made inert carries an invisible character after its sign.
const inertToken = /((?:@|#|GH-)\u200b[\w.-]+)/g;

type Segment = { kind: 'text' | 'customer-message' | 'team-note'; text: string; by?: string };
const unescape = (value: string) => value.replace(/&quot;/g, '"').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&#39;/g, "'").replace(/&amp;/g, '&');
export function segmentsOf(body: string): Segment[] {
  const segments: Segment[] = [];
  let current: Segment = { kind: 'text', text: '' };
  for (const line of body.split('\n')) {
    const start = current.kind === 'text' ? blockStart.exec(line.trim()) : null;
    if (start) {
      if (current.text.trim()) segments.push(current);
      const by = /from="([^"]*)"/.exec(line)?.[1];
      current = { kind: start[1] as Segment['kind'], text: '', by: by === undefined ? undefined : unescape(by) };
    } else if (current.kind !== 'text' && blockEnd.test(line.trim())) {
      segments.push(current);
      current = { kind: 'text', text: '' };
    } else current.text += `${current.text ? '\n' : ''}${line}`;
  }
  if (current.text.trim()) segments.push(current);
  return segments;
}

function Masked({ text }: { text: string }) {
  return <>{text.split(removedToken).map((part, index) => (index % 2 === 1 ? <span key={index} className="af-red">{part}</span>
    : part.split(inertToken).map((piece, at) => (at % 2 === 1 ? <span key={`${index}-${at}`} className="af-off-ref" title={m.inertHelp}>{piece}</span> : piece))))}</>;
}

// BodyView shows the text as the agent gets it: the customer's words in the untrusted block, removed parts as chips.
export function BodyView({ body }: { body: string }) {
  return <div className="af-pv-read">{segmentsOf(body).map((segment, index) => (segment.kind === 'customer-message'
    ? <div key={index} className="af-untrusted"><span className="af-tag-u"><ShieldIcon />{m.untrustedTag}</span>
      {segment.by && <span className="af-by">{segment.by}</span>}<p><Masked text={segment.text.trim()} /></p></div>
    : segment.kind === 'team-note'
      ? <div key={index} className="af-team-note"><span className="af-by">{m.teamNote(segment.by)}</span><p><Masked text={segment.text.trim()} /></p></div>
      : <p key={index}><Masked text={segment.text.trim()} /></p>))}</div>;
}

export function Ledger({ removed }: { removed: FixPreview['removed'] }) {
  const kinds = (Object.keys(m.removed) as (keyof typeof m.removed)[]).filter((kind) => removed[kind] > 0);
  return <div className="af-ledger"><b>{m.removedTitle}</b>
    {kinds.length === 0 ? <span>{m.removedNothing}</span>
      : <ul>{kinds.map((kind) => { const [n, ...rest] = m.removed[kind](removed[kind]).split(' '); return <li key={kind}><b>{n}</b> {rest.join(' ')}</li>; })}</ul>}</div>;
}

export interface SendDialogProps {
  conversationId: string;
  repos: Repo[];
  connectors: ConnectorState[];
  initialRepo?: string;
  initialConnector?: FixConnector;
  // Retrying sends a new attempt of this fix.
  retryOf?: Fix;
  timezone: string;
  onClose: () => void;
  // The fix was sent: an issue connector's answer carries where the browser goes next.
  onSent: (sent: FixSent) => void;
}

const newKey = () => (typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`);

export function SendDialog({ conversationId, repos, connectors, initialRepo, initialConnector, retryOf, timezone, onClose, onSent }: SendDialogProps) {
  const enabled = connectors.filter((item) => item.enabled && (item.kind !== 'cursor_api' || item.credential !== null));
  const [repoId, setRepoId] = useState(initialRepo ?? repos[0]?.id ?? '');
  const repo = repos.find((item) => item.id === repoId) ?? repos[0];
  const [connector, setConnector] = useState<FixConnector | undefined>(initialConnector ?? enabled[0]?.kind);
  const [noteIds, setNoteIds] = useState<string[]>([]);
  // One key per thing being sent: pressing Send twice sends once; a different repository or agent is a new send.
  const [clientKey, setClientKey] = useState(newKey);
  const preview = useQuery({
    queryKey: ['autofix-preview', conversationId, repo?.id, connector, noteIds],
    queryFn: () => autofixApi.preview(conversationId, { repo_id: repo!.id, connector: connector!, ...(noteIds.length ? { note_ids: noteIds } : {}) }),
    // Built once while the dialog is open, and again the next time it opens: new messages go in.
    enabled: Boolean(repo && connector), staleTime: Infinity, gcTime: 0, refetchOnMount: 'always', retry: false,
  });
  // The person's edits, over the preview the server built; a new preview (another repository, agent or note) starts again.
  const [edited, setEdited] = useState<{ for: unknown; title: string; body: string }>();
  const mine = edited?.for === preview.data ? edited : undefined;
  const title = mine?.title ?? preview.data?.title ?? '';
  const body = mine?.body ?? preview.data?.body ?? '';
  const [editing, setEditing] = useState(false);
  const [sending, setSending] = useState(false);
  const [problem, setProblem] = useState<string>();
  const change = (next: () => void) => { next(); setClientKey(newKey()); setProblem(undefined); setEditing(false); };
  const edit = (value: Partial<{ title: string; body: string }>) => setEdited({ for: preview.data, title, body, ...value });
  const submit = async () => {
    if (!repo || !connector || !preview.data || preview.isFetching) return;
    if (!title.trim()) return setProblem(m.emptyTitle);
    if (!body.trim()) return setProblem(m.emptyBody);
    setSending(true);
    setProblem(undefined);
    const input = { repo_id: repo.id, connector, title: title.trim(), body, message_ids: preview.data.message_ids, ...(noteIds.length ? { note_ids: noteIds } : {}), client_key: clientKey };
    try {
      onSent(await (retryOf ? autofixApi.retry(retryOf.id, input) : autofixApi.send(conversationId, input)));
    } catch (error) {
      setProblem(error instanceof InvalidRequestError || error instanceof RedirectRefused ? error.message : m.actionFailed);
      setSending(false);
    }
  };
  const data = preview.data;
  const issue = connector !== 'cursor_api';
  const notesAllowed = data?.notes_allowed ?? repo?.include_notes ?? false;
  const footer = <>
    {data && <span className="af-dest">{issue ? <ExternalIcon /> : <KeyIcon />}<span>{issue ? m.destIssue(data.destination.repo_full_name) : m.destCursor(data.destination.vendor_retention)}</span></span>}
    <button type="button" className="af-ghost" onClick={onClose}>{m.cancel}</button>
    <button type="submit" className="primary-action" disabled={!data || sending || preview.isFetching}>{issue ? <><ExternalIcon />{m.continueOnGitHub}</> : m.sendToCursor}</button>
  </>;
  let content: ReactNode;
  if (!repo || !connector) content = <p>{repos.length === 0 ? m.noRepo : m.noAgent}</p>;
  else if (preview.isPending) content = <div role="status" className="af-skeletons"><span className="af-skeleton" /><span className="af-skeleton" /><span className="af-skeleton" /><span className="af-help">{m.removingDetails}</span></div>;
  else if (preview.isError || !data) content = <div className="af-oc af-danger" role="alert"><span><b>{m.previewFailed}</b></span><div className="af-acts"><button type="button" onClick={() => void preview.refetch()}>{m.tryAgain}</button></div></div>;
  else {
    content = <>
      <Ledger removed={data.removed} />
      <section className="af-pv" aria-label={m.whatAgentGets}>
        <div className="af-pv-head"><b>{issue ? <>{m.issueIn} <span className="af-mono">{data.destination.repo_full_name}</span></> : m.promptForCursor}</b>
          <button type="button" className="link-button af-push" aria-pressed={editing} onClick={() => setEditing(!editing)}>{m.editable}</button></div>
        <label><span className="sr-only">{m.title}</span><input className="af-pv-title" value={title} maxLength={256} onChange={(event) => edit({ title: event.target.value })} /></label>
        {editing
          ? <label><span className="sr-only">{m.body}</span><textarea className="af-pv-edit" value={body} rows={12} maxLength={65536} onChange={(event) => edit({ body: event.target.value })} /></label>
          : <BodyView body={body} />}
      </section>
      <p className="af-help">{m.untrustedHelp}</p>
      {data.notes.length > 0 && <section className="af-notes-sec" aria-label={m.notesTitle}>
        <div className="af-sec-head"><b>{m.notesTitle}</b><span className="af-help">{notesAllowed ? m.notesOff : m.notesLocked(repo.full_name)}</span></div>
        <ul className={`af-notes${notesAllowed ? '' : ' af-locked'}`}>{data.notes.map((note) => <li key={note.message_id}>
          <input type="checkbox" disabled={!notesAllowed || sending} checked={noteIds.includes(note.message_id)}
            aria-label={m.includeNote(note.author_name ?? m.someone)}
            onChange={(event) => change(() => setNoteIds(event.target.checked ? [...noteIds, note.message_id] : noteIds.filter((id) => id !== note.message_id)))} />
          <span><Masked text={`“${note.excerpt}”`} /><span className="af-help">{m.noteBy(note.author_name, formatShortDate(note.created_at, timezone))}</span>{!notesAllowed && <span className="af-help">{m.notesLockedHelp}</span>}</span></li>)}</ul>
      </section>}
      {data.attachments_left_out > 0 && <p className="af-help">{m.attachmentsLeft(data.attachments_left_out)}</p>}
    </>;
  }
  const next = retryOf ? retryOf.attempt_no + 1 : 1;
  return <Dialog title={retryOf ? m.retryTitle(next) : m.sendTitle} closeLabel={m.close} onClose={onClose} onSubmit={() => void submit()} className="af-dialog af-wide"
    help={retryOf ? m.retryDialogHelp(retryOf.attempt_no, retryOf.state === 'cancelled' ? 'was cancelled' : 'failed') : m.sendHelp} footer={footer}>
    <Field id="af-send-repo" label={m.repository}>{(props) => <select {...props} value={repo?.id ?? ''} disabled={sending}
      onChange={(event) => change(() => { setRepoId(event.target.value); setNoteIds([]); })}>
      {repos.map((item) => <option key={item.id} value={item.id}>{item.full_name}</option>)}</select>}</Field>
    <fieldset className="af-agentpick-set"><legend className="field-label">{m.agent}</legend>
      <div className="af-agentpick">{enabled.map((item) => <label key={item.kind} className="af-cardopt">
        <input type="radio" name="af-send-agent" value={item.kind} checked={connector === item.kind} disabled={sending} onChange={() => change(() => setConnector(item.kind))} />
        <b>{m.vendor(item.kind)}</b><span>{m.agentPick[item.kind]}</span></label>)}</div>
    </fieldset>
    {content}
    {problem && <p role="alert" className="error-copy">{problem}</p>}
  </Dialog>;
}
