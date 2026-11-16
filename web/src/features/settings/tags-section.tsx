import { useEffect, useRef, useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import type { Tag } from '../../api/schemas';
import { messages } from '../../messages';
import { Refused, useConfirmDrop, useSaveChanges, useSettingsEdit } from './settings-edits';
import { settingsMessages } from './settings-messages';
import { tagMessages as m } from './tag-messages';
import './tags.css';

interface TagsSectionProps {
  // Each tag carries conversation_count (GET /api/v1/tags?counts=true).
  tags: Tag[];
  // Rename needs tags:update and Delete tags:delete; without them the row shows no button for it.
  canRename: boolean;
  canDelete: boolean;
  // Each resolves when the server took the change and rejects with the reason when it did not.
  onRename: (id: string, name: string) => Promise<unknown>;
  onDelete: (id: string) => Promise<unknown>;
}

const reason = (error: unknown) => (error instanceof InvalidRequestError ? error.message : m.actionFailed);

// TagsSection lists the workspace's tags with how many conversations carry each. A tag is renamed in its row, saved by
// the page's Save bar like any typed text, and deleted after the row asks once, naming what the delete takes away.
export function TagsSection({ tags, canRename, canDelete, onRename, onDelete }: TagsSectionProps) {
  const [editing, setEditing] = useState<{ tag: Tag; name: string } | null>(null);
  const [confirming, setConfirming] = useState<string | null>(null);
  const [problem, setProblem] = useState<string>();
  const [status, setStatus] = useState<string>();
  const [busy, setBusy] = useState(false);
  // The row's button that opened a rename or a confirm gets the focus back when it closes without a change; the row
  // is drawn again by then, so the button is found by its name.
  const section = useRef<HTMLElement>(null);
  const opener = useRef<string | undefined>(undefined);
  const [refocus, setRefocus] = useState(false);
  useEffect(() => {
    if (!refocus) return;
    setRefocus(false);
    [...section.current?.querySelectorAll('button') ?? []].find((button) => button.getAttribute('aria-label') === opener.current)?.focus();
  }, [refocus]);
  const saveChanges = useSaveChanges();
  const confirmDrop = useConfirmDrop();

  const closeEdit = (focus: boolean) => { setEditing(null); setProblem(undefined); if (focus) setRefocus(true); };
  const changed = editing !== null && editing.name !== editing.tag.name;
  useSettingsEdit('tag-rename', changed ? { label: m.tagLabel(editing.tag.name),
    discard: () => closeEdit(true),
    save: async () => {
      const name = editing.name.trim();
      if (name === '') { setProblem(m.nameRequired); throw new Refused(m.nameRequired); }
      setProblem(undefined);
      try {
        await onRename(editing.tag.id, name);
      } catch (error) {
        setProblem(reason(error));
        throw error;
      }
      setStatus(m.renamed(editing.tag.name, name));
      setEditing(null);
    } } : undefined);

  const startRename = (tag: Tag) => {
    if (!confirmDrop('tag-rename')) return;
    opener.current = m.renameTag(tag.name);
    setConfirming(null);
    setProblem(undefined);
    setEditing({ tag, name: tag.name });
  };
  const startDelete = (tag: Tag) => {
    opener.current = m.deleteTag(tag.name);
    setProblem(undefined);
    setConfirming(tag.id);
  };
  const remove = async (tag: Tag) => {
    if (busy) return;
    setBusy(true);
    setProblem(undefined);
    try {
      await onDelete(tag.id);
      setConfirming(null);
      setStatus(m.deleted(tag.name));
    } catch (error) {
      setProblem(reason(error));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section ref={section} className="settings-card tags-settings" aria-labelledby="tags-heading">
      <h2 id="tags-heading" className="sr-only">{m.tags}</h2>
      <div className="settings-body">
        {status && <p role="status" className="notice notice-plain">{status}</p>}
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        {tags.length === 0 ? <p className="empty-copy">{m.noTags}</p> : <ul className="tag-list">{tags.map((tag) => {
          const count = tag.conversation_count ?? 0;
          if (editing?.tag.id === tag.id) {
            return <li key={tag.id}>
              <form className="tag-rename" onSubmit={(event) => { event.preventDefault(); void saveChanges(); }}>
                <input autoFocus aria-label={m.newNameFor(tag.name)} value={editing.name} maxLength={100}
                  onChange={(event) => setEditing({ tag, name: event.target.value })}
                  onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeEdit(true); } }} />
                <span className="tag-count">{m.count(count)}</span>
                <span className="tag-rename-actions"><button type="button" onClick={() => closeEdit(true)}>{messages.cancel}</button></span>
              </form>
            </li>;
          }
          if (confirming === tag.id) {
            return <li key={tag.id}>
              <div role="group" aria-label={m.deleteTag(tag.name)} className="tag-confirm">
                <span>{m.confirmDelete(tag.name, count)}</span>
                <span className="tag-confirm-actions">
                  <button type="button" className="danger-action" disabled={busy} onClick={() => void remove(tag)}>{m.deleteButton}</button>
                  <button type="button" disabled={busy} onClick={() => { setConfirming(null); setRefocus(true); }}>{messages.cancel}</button>
                </span>
              </div>
            </li>;
          }
          return <li key={tag.id}>
            <strong>{tag.name}</strong>
            <span className="tag-count">{m.count(count)}</span>
            {(canRename || canDelete) && <span className="tag-actions">
              {canRename && <button type="button" aria-label={m.renameTag(tag.name)}
                onClick={() => startRename(tag)}>{m.rename}</button>}
              {canDelete && <button type="button" className="danger-action" aria-label={m.deleteTag(tag.name)}
                onClick={() => startDelete(tag)}>{settingsMessages.confirmDelete}</button>}
            </span>}
          </li>;
        })}</ul>}
      </div>
    </section>
  );
}
