import { useEffect, useId, useRef, useState } from 'react';
import { ConflictError } from '../../api/client';
import type { Contact, ContactDetail, ContactMerge, Conversation } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { contactAddress, contactMessages } from './contact-messages';
import { dialogKeys, keepDialogFocus, useReturnFocus } from '../../ui/dialog-keys';
import { ChevronLeftIcon, ChevronRightIcon } from '../../ui/icons';
import { StatusLabel } from '../inbox/conversation-labels';
import { ContactPicker } from './contact-picker';

interface ContactPageProps {
  contact?: ContactDetail;
  conversations?: Conversation[];
  page: number;
  pageSize: number;
  onPage: (page: number) => void;
  timezone?: string;
  loading?: boolean;
  failed?: boolean;
  historyLoading?: boolean;
  // Each may return a promise: the form stays open, with the reason shown, when it rejects.
  onRename?: (name: string) => Promise<unknown>;
  onMerge?: (duplicate: Contact) => Promise<unknown>;
  onUndoMerge?: (merge: ContactMerge) => Promise<unknown>;
}

export function ContactPage({ contact, conversations = [], page, pageSize, onPage, timezone = 'UTC', loading, failed, historyLoading, onRename, onMerge, onUndoMerge }: ContactPageProps) {
  const [merging, setMerging] = useState(false);
  // What the last change did, for screen readers; the region stays mounted so each change is announced.
  const [notice, setNotice] = useState('');
  const mergeButton = useRef<HTMLButtonElement>(null);
  const historyHeading = useRef<HTMLHeadingElement>(null);
  const pages = Math.max(1, Math.ceil((contact?.conversation_count ?? 0) / pageSize));
  // A new page starts at its first row: the heading above the list takes the focus and the view.
  const turnPage = (next: number) => {
    onPage(next);
    historyHeading.current?.focus();
    historyHeading.current?.scrollIntoView?.({ block: 'start' });
  };
  if (!contact) {
    return <main className="settings-page contact-page">
      {loading && <p role="status" className="empty-copy">{messages.loading}</p>}
      {failed && <p role="alert" className="error-copy">{contactMessages.contactNotLoaded}</p>}
    </main>;
  }
  const merged = contact.merged_into;
  const merge = onMerge && (async (duplicate: Contact) => {
    await onMerge(duplicate);
    setNotice(contactMessages.mergedNotice(contactAddress(duplicate)));
  });
  const undo = onUndoMerge && (async (item: ContactMerge) => {
    await onUndoMerge(item);
    setNotice(contactMessages.mergeUndoneNotice(contactAddress(item.merged_contact)));
    // The undone row is gone with its button; the merge button is where merging starts again.
    mergeButton.current?.focus();
  });
  return (
    <main className="settings-page contact-page" aria-label={contactMessages.contactOf(contact.name)}>
      <p className="sr-only" role="status" aria-live="polite">{notice}</p>
      <header className="contact-header">
        <ContactName contact={contact} onRename={merged ? undefined : onRename} onSaved={() => setNotice(contactMessages.nameSavedNotice)} />
        <dl className="contact-facts">
          <div><dt>{contactMessages.contactEmail}</dt><dd>{contact.email ?? contactMessages.noEmail}</dd></div>
          {contact.identities?.map((identity) => <div key={`${identity.channel_id}:${identity.kind}:${identity.value}`}>
            <dt>{contactMessages.contactOnChannel}</dt><dd>{identity.display_name ? `${identity.display_name} · ${identity.value}` : identity.value}</dd></div>)}
          {contact.organization && <div><dt>{contactMessages.contactOrganization}</dt><dd>{contact.organization}</dd></div>}
          <div><dt>{contactMessages.contactSince}</dt><dd><time dateTime={contact.created_at}>{formatDate(contact.created_at, timezone)}</time></dd></div>
        </dl>
      </header>
      {merged && <p role="status" className="contact-merged-notice">{contactMessages.mergedIntoNotice(merged.name, contactAddress(merged))}{' '}
        <a href={`/contacts/${merged.id}`}>{contactMessages.openMergedInto}</a></p>}
      {!merged && onMerge && <div className="button-row">
        <button type="button" ref={mergeButton} data-opens="merge-dialog" onClick={() => setMerging(true)}>{contactMessages.mergeDuplicate}</button>
      </div>}
      {contact.merges.length > 0 && <MergeList merges={contact.merges} timezone={timezone} onUndo={undo} />}
      {/* A merged contact's conversations are with the contact it joined; its own history would read "none". */}
      {!merged && <section className="contact-history" aria-labelledby="contact-history-title">
        <h2 id="contact-history-title" ref={historyHeading} tabIndex={-1}>{contactMessages.contactHistory}</h2>
        <p className="muted-copy">{contactMessages.contactConversationCount(contact.conversation_count)}</p>
        {historyLoading && conversations.length === 0 && <p role="status" className="empty-copy">{messages.loading}</p>}
        {!historyLoading && contact.conversation_count === 0 && <p className="empty-copy">{contactMessages.contactNoConversations}</p>}
        <ul className="contact-history-list">
          {conversations.map((item) => (
            <li key={item.id}>
              <a className="contact-history-row" href={`/conversations/${item.id}`}>
                <span className="contact-history-subject">{item.subject}</span>
                <span className="contact-history-meta">
                  <StatusLabel status={item.status} />
                  {item.origin && <span className="badge">{contactMessages.importedBadge}</span>}
                  <time dateTime={item.updated_at}>{formatDate(item.updated_at, timezone)}</time>
                </span>
              </a>
            </li>
          ))}
        </ul>
        {pages > 1 && <nav className="pagination" aria-label={contactMessages.contactHistory}>
          <button type="button" className="icon-button" aria-label={messages.previousPage} data-tip={messages.previousPage} disabled={page <= 1} onClick={() => turnPage(page - 1)}><ChevronLeftIcon /></button>
          <span>{contactMessages.contactHistoryPage(page, pages)}</span>
          <button type="button" className="icon-button" aria-label={messages.nextPage} data-tip={messages.nextPage} disabled={page >= pages} onClick={() => turnPage(page + 1)}><ChevronRightIcon /></button>
        </nav>}
      </section>}
      {merging && merge && <MergeDialog contact={contact} onMerge={merge} onClose={() => setMerging(false)} />}
    </main>
  );
}

function ContactName({ contact, onRename, onSaved }: { contact: ContactDetail; onRename?: (name: string) => Promise<unknown>; onSaved?: () => void }) {
  const [editing, setEditing] = useState(false);
  const renameButton = useRef<HTMLButtonElement>(null);
  // Closing the form, saved or not, gives the focus back to the button that opened it.
  const reopened = useRef(false);
  useEffect(() => {
    if (!editing && reopened.current) renameButton.current?.focus();
    reopened.current = editing;
  }, [editing]);
  const [name, setName] = useState(contact.name);
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState(false);
  const fieldId = useId();
  const open = () => { setName(contact.name); setProblem(false); setEditing(true); };
  const save = async () => {
    if (!onRename || !name.trim() || saving) return;
    setSaving(true);
    setProblem(false);
    try {
      await onRename(name.trim());
      setEditing(false);
      onSaved?.();
    } catch {
      setProblem(true);
    } finally {
      setSaving(false);
    }
  };
  if (!editing) {
    return <div className="contact-name-row">
      <h1>{contact.name}</h1>
      {onRename && <button type="button" ref={renameButton} onClick={open}>{contactMessages.renameContact}</button>}
    </div>;
  }
  return (
    <form className="contact-rename" onSubmit={(event) => { event.preventDefault(); void save(); }}>
      <label htmlFor={fieldId}>{contactMessages.contactName}</label>
      <input id={fieldId} value={name} maxLength={200} autoFocus disabled={saving} onChange={(event) => setName(event.target.value)}
        onKeyDown={(event) => { if (event.key === 'Escape') setEditing(false); }} />
      <button type="submit" className="primary-action" disabled={saving || !name.trim() || name.trim() === contact.name}>{messages.saveName}</button>
      <button type="button" disabled={saving} onClick={() => setEditing(false)}>{messages.cancel}</button>
      {problem && <p role="alert" className="error-copy">{contactMessages.contactRenameFailed}</p>}
    </form>
  );
}

function MergeList({ merges, timezone, onUndo }: { merges: ContactMerge[]; timezone: string; onUndo?: (merge: ContactMerge) => Promise<unknown> }) {
  const [busy, setBusy] = useState<string>();
  const [failed, setFailed] = useState<string>();
  const undo = async (merge: ContactMerge) => {
    if (!onUndo || busy) return;
    setBusy(merge.id);
    setFailed(undefined);
    try {
      await onUndo(merge);
    } catch {
      setFailed(merge.id);
    } finally {
      setBusy(undefined);
    }
  };
  return (
    <section className="contact-merges" aria-labelledby="contact-merges-title">
      <h2 id="contact-merges-title">{contactMessages.mergedContacts}</h2>
      <ul>
        {merges.map((merge) => (
          <li key={merge.id}>
            <span><a href={`/contacts/${merge.merged_contact.id}`}>{merge.merged_contact.name}</a>{' '}
              {contactMessages.mergedLine(contactAddress(merge.merged_contact), merge.conversation_ids.length)}{' '}
              <time className="muted-copy" dateTime={merge.merged_at}>{formatDate(merge.merged_at, timezone)}</time></span>
            {onUndo && <button type="button" aria-label={contactMessages.undoMergeLabel(contactAddress(merge.merged_contact))} disabled={busy !== undefined}
              onClick={() => void undo(merge)}>{contactMessages.undoMerge}</button>}
            {failed === merge.id && <p role="alert" className="error-copy">{contactMessages.undoMergeFailed}</p>}
          </li>
        ))}
      </ul>
    </section>
  );
}

function MergeDialog({ contact, onMerge, onClose }: { contact: ContactDetail; onMerge: (duplicate: Contact) => Promise<unknown>; onClose: () => void }) {
  useReturnFocus('[data-opens="merge-dialog"]');
  const [duplicate, setDuplicate] = useState<Contact>();
  const [sending, setSending] = useState(false);
  const [problem, setProblem] = useState<string>();
  const root = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const merge = async () => {
    if (!duplicate || sending) return;
    setSending(true);
    setProblem(undefined);
    try {
      await onMerge(duplicate);
      onClose();
    } catch (error) {
      setProblem(error instanceof ConflictError ? error.message : contactMessages.mergeFailed);
    } finally {
      setSending(false);
    }
  };
  return (
    <div className="dialog-backdrop" onMouseDown={keepDialogFocus}>
      <div className="dialog merge-dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} ref={root} onKeyDown={dialogKeys(root, onClose)}>
        <h2 id={titleId}>{contactMessages.mergeTitle(contact.name)}</h2>
        {!duplicate && <>
          <p className="muted-copy">{contactMessages.mergeIntro}</p>
          <ContactPicker label={contactMessages.mergeSearch} excludeId={contact.id} autoFocus onPick={setDuplicate} />
          <div className="button-row"><button type="button" onClick={onClose}>{messages.cancel}</button></div>
        </>}
        {duplicate && <>
          <p>{contactMessages.mergeConfirm(`${duplicate.name} (${contactAddress(duplicate)})`, contact.name, contactAddress(contact))}</p>
          {problem && <p role="alert" className="error-copy">{problem}</p>}
          <div className="button-row">
            <button type="button" className="primary-action" autoFocus disabled={sending} onClick={() => void merge()}>{sending ? contactMessages.merging : contactMessages.mergeAction}</button>
            <button type="button" disabled={sending} onClick={() => setDuplicate(undefined)}>{contactMessages.newConversationChange}</button>
            <button type="button" disabled={sending} onClick={onClose}>{messages.cancel}</button>
          </div>
        </>}
      </div>
    </div>
  );
}
