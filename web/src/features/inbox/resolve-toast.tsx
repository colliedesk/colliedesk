import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import type { Conversation, SettableStatus } from '../../api/schemas';
import { messages } from '../../messages';
import { XIcon } from '../../ui/icons';
import { noticesOf } from './status-tell';

type SetStatus = (conversationId: string, status: SettableStatus, options?: { notify?: boolean }) => Promise<unknown>;
interface Resolved { id: string; from: SettableStatus; told?: string }

// Long enough to read the line and press Undo; the toast sits over the reply box's buttons, so it does not stay.
const toastMs = 8000;

/** Resolving (from the header or with Send and resolve) says so in a toast with Undo, since the conversation
 * otherwise only leaves the list. Undo puts it back where it was, quietly: a chat that already told the
 * customer cannot take that message back, so the toast names it. It leaves after a few seconds unless the pointer or
 * the keyboard is on it; Reopen above the thread stays the way back. Opening another conversation clears it. */
export function useResolveToast(selected: Conversation | undefined, onSetStatus: SetStatus | undefined): { setStatus?: (status: SettableStatus, options?: { notify?: boolean }) => Promise<unknown>; toast: ReactNode } {
  const [resolved, setResolved] = useState<Resolved>();
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [held, setHeld] = useState(false);
  const selectedId = selected?.id;
  useEffect(() => { setResolved(undefined); setFailed(false); setHeld(false); }, [selectedId]);
  // Once it reads resolved, a later status (a customer's reply, a teammate, the details pane) ends the toast:
  // Undo would otherwise override that change.
  const status = selected?.status;
  const [seen, setSeen] = useState(false);
  useEffect(() => {
    if (!resolved) { setSeen(false); return; }
    if (status === 'resolved') setSeen(true);
    else if (seen) { setResolved(undefined); setHeld(false); }
  }, [resolved, status, seen]);
  useEffect(() => {
    if (!resolved || held) return;
    const timer = setTimeout(() => setResolved(undefined), toastMs);
    return () => clearTimeout(timer);
  }, [resolved, held]);
  if (!selected || !onSetStatus) return { toast: null };
  const conversation = selected;
  const setStatus = async (status: SettableStatus, options?: { notify?: boolean }) => {
    const from: SettableStatus = conversation.status === 'pending' ? 'pending' : 'open';
    const result = await onSetStatus(conversation.id, status, options);
    setFailed(false);
    if (status !== 'resolved') { setResolved(undefined); setHeld(false); return result; }
    const tells = options?.notify !== false && (noticesOf(conversation)?.includes('resolved') ?? false);
    setHeld(false);
    setResolved({ id: conversation.id, from, told: tells ? conversation.channel_ref?.name ?? messages.theChannel : undefined });
    return result;
  };
  const undo = (shown: Resolved) => {
    if (busy) return;
    setBusy(true);
    setFailed(false);
    // A slow Undo never clears the toast of a conversation resolved after it.
    const clear = () => setResolved((now) => (now === shown ? undefined : now));
    onSetStatus(shown.id, shown.from, { notify: false }).then(() => { clear(); setHeld(false); }, () => setFailed(true)).finally(() => setBusy(false));
  };
  const shown = resolved?.id === conversation.id ? resolved : undefined;
  const toast = shown && <div role="status" aria-label={messages.statuses.resolved} className="status-toast"
    onMouseEnter={() => setHeld(true)} onMouseLeave={() => setHeld(false)} onFocus={() => setHeld(true)} onBlur={() => setHeld(false)}>
    <span>{failed ? messages.statusChangeFailed : messages.conversationResolved(shown.told)}</span>
    <button type="button" className="link-action" disabled={busy} onClick={() => undo(shown)}>{messages.undo}</button>
    <button type="button" className="icon-button" aria-label={messages.dismiss} onClick={() => { setResolved(undefined); setHeld(false); }}><XIcon /></button></div>;
  return { setStatus, toast };
}
