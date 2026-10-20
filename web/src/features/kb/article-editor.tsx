import { useId, useMemo, useRef, useState } from 'react';
import type { ClipboardEvent, DragEvent, KeyboardEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useBlocker, useNavigate } from '@tanstack/react-router';
import { api, ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import { messages } from '../../messages';
import { RouterLink } from '../../ui/router-link';
import { ChevronLeftIcon } from '../../ui/icons';
import { SavedReplyHistory } from '../settings/saved-reply-panels';
import { kbApi } from './kb-api';
import type { KBArticle, KBArticleStatus, KBArticleVersion } from './kb-api';
import { kbMessages } from './kb-messages';
import { buildTree, flatten, indent, reason } from './kb-tree';
import { useRefreshKB } from './knowledge-page';
import { MarkdownPreview } from './markdown-preview';

// The limits the server checks (server/internal/domain/kb.go), counted in characters like the server.
export const maxTitle = 300;
export const maxBody = 100000;
const maxImageBytes = 5 * 1024 * 1024;
const imageTypes = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'];

// characters counts what a person sees as characters (Unicode code points), as the server does; a
// string's length counts UTF-16 units, so an emoji would count twice.
export const characters = (text: string) => [...text].length;

interface Draft {
  title: string;
  slug: string;
  body: string;
  collection_id: string;
}

const draftOf = (article: KBArticle): Draft => ({ title: article.title, slug: article.slug, body: article.body_md, collection_id: article.collection_id });

// ArticleRoute opens an article in the editor, or a new one (in collectionId when given).
export function ArticleRoute({ articleId, collectionId }: { articleId?: string; collectionId?: string }) {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const collections = useQuery({ queryKey: ['kb-collections'], queryFn: kbApi.collections });
  const article = useQuery({ queryKey: ['kb-article', articleId], queryFn: () => kbApi.article(articleId ?? ''), enabled: articleId !== undefined, retry: false });
  const back = <BackLink />;
  if ((articleId && article.isPending) || collections.isPending) {
    return <main className="settings-page kb-editor-page">{back}<p role="status" className="empty-copy">{messages.loading}</p></main>;
  }
  if (articleId && !article.data) {
    const missing = article.error instanceof Error && article.error.message === 'API 404';
    return <main className="settings-page kb-editor-page">{back}<p role="alert" className="error-copy">{missing ? kbMessages.articleNotFound : kbMessages.articleLoadFailed}</p></main>;
  }
  if (article.data?.deleted_at) {
    return <main className="settings-page kb-editor-page">{back}<p role="status" className="warning-note">{kbMessages.articleInTrash}</p></main>;
  }
  const items = collections.data?.items ?? [];
  const known = collectionId && items.some((item) => item.id === collectionId) ? collectionId : undefined;
  return <ArticleEditor article={article.data} collections={items} defaultCollection={known ?? (items.length === 1 ? items[0].id : '')}
    timezone={me.data?.timezone ?? 'UTC'} collectionsFailed={collections.isError} />;
}

function BackLink() {
  const navigate = useNavigate();
  return <RouterLink className="kb-back" href="/knowledge" onFollow={() => void navigate({ to: '/knowledge' })}><ChevronLeftIcon />{kbMessages.backToList}</RouterLink>;
}

interface ArticleEditorProps {
  article?: KBArticle;
  collections: Parameters<typeof buildTree>[0];
  defaultCollection: string;
  timezone: string;
  collectionsFailed?: boolean;
}

export function ArticleEditor({ article, collections, defaultCollection, timezone, collectionsFailed }: ArticleEditorProps) {
  const id = useId();
  const client = useQueryClient();
  const navigate = useNavigate();
  const refresh = useRefreshKB();
  const tree = useMemo(() => flatten(buildTree(collections)), [collections]);
  // base is the article as the server last answered; every save and restore sends its updated_at.
  const [base, setBase] = useState(article);
  const [draft, setDraft] = useState<Draft>(() => (article ? draftOf(article) : { title: '', slug: '', body: '', collection_id: defaultCollection }));
  const [tab, setTab] = useState<'write' | 'preview'>('write');
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [conflict, setConflict] = useState(false);
  const [savedOnce, setSavedOnce] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [notice, setNotice] = useState('');
  const [imageProblems, setImageProblems] = useState<string[]>([]);
  const body = useRef<HTMLTextAreaElement>(null);
  const historyButton = useRef<HTMLButtonElement>(null);
  const uploads = useRef(0);
  const set = (change: Partial<Draft>) => { setSavedOnce(false); setDraft((current) => ({ ...current, ...change })); };
  const dirty = base
    ? draft.title !== base.title || draft.slug !== base.slug || draft.body !== base.body_md || draft.collection_id !== base.collection_id
    : draft.title.trim() !== '' || draft.body.trim() !== '';
  // A person leaving with unsaved text is asked first; a save that moves on (a new article, a delete) is not.
  const leaving = useRef(false);
  const unsaved = useRef(dirty);
  unsaved.current = dirty;
  useBlocker({
    shouldBlockFn: () => unsaved.current && !leaving.current && !window.confirm(kbMessages.leaveUnsaved),
    enableBeforeUnload: () => unsaved.current && !leaving.current,
  });
  const titleLength = characters(draft.title);
  const bodyLength = characters(draft.body);
  const tooLong = bodyLength > maxBody;
  const invalid = !draft.title.trim() || titleLength > maxTitle || tooLong || !draft.collection_id;

  const save = async (status?: KBArticleStatus, over?: KBArticle) => {
    if (busy || invalid) return;
    const current = over ?? base;
    setBusy(true);
    setProblem(undefined);
    const sent = draft;
    try {
      if (!current) {
        const created = await kbApi.createArticle({ collection_id: sent.collection_id, title: sent.title.trim(), body_md: sent.body, status: status ?? 'draft' });
        client.setQueryData(['kb-article', created.id], created);
        void refresh();
        leaving.current = true;
        await navigate({ to: '/knowledge/articles/$articleId', params: { articleId: created.id }, replace: true });
        return;
      }
      const updated = await kbApi.updateArticle(current.id, {
        updated_at: current.updated_at,
        ...(sent.title.trim() !== current.title ? { title: sent.title.trim() } : {}),
        ...(sent.slug.trim() && sent.slug.trim() !== current.slug ? { slug: sent.slug.trim() } : {}),
        ...(sent.body !== current.body_md ? { body_md: sent.body } : {}),
        ...(sent.collection_id !== current.collection_id ? { collection_id: sent.collection_id } : {}),
        ...(status && status !== current.status ? { status } : {}),
      });
      setBase(updated);
      // The server may write the title or address name its own way; text typed during the save stays.
      setDraft((now) => (now === sent ? draftOf(updated) : now));
      client.setQueryData(['kb-article', updated.id], updated);
      setConflict(false);
      setSavedOnce(true);
      setNotice(status === 'published' ? kbMessages.statusPublished : status === 'draft' ? kbMessages.statusDraft : kbMessages.saved);
      void refresh();
    } catch (error) {
      if (error instanceof ChangedElsewhereError) setConflict(true);
      else setProblem(reason(error, kbMessages.articleActionFailed));
    } finally {
      setBusy(false);
    }
  };
  // Their version replaces this form only when the person asks; keeping mine saves it over theirs, which
  // stays in the history.
  const latest = async () => {
    if (!base) return undefined;
    try {
      const fresh = await kbApi.article(base.id);
      setBase(fresh);
      return fresh;
    } catch (error) {
      setProblem(reason(error, kbMessages.articleLoadFailed));
      return undefined;
    }
  };
  const loadTheirs = async () => {
    const fresh = await latest();
    if (!fresh) return;
    setDraft(draftOf(fresh));
    setConflict(false);
  };
  const keepMine = async () => {
    const fresh = await latest();
    if (fresh) await save(undefined, fresh);
  };
  const remove = async () => {
    if (!base) return;
    setBusy(true);
    setProblem(undefined);
    try {
      await kbApi.deleteArticle(base.id, base.updated_at);
      void refresh();
      leaving.current = true;
      await navigate({ to: '/knowledge' });
    } catch (error) {
      if (error instanceof ChangedElsewhereError) setConflict(true);
      else setProblem(reason(error, kbMessages.articleActionFailed));
      setConfirmingDelete(false);
      setBusy(false);
    }
  };

  // Images pasted or dropped into the text are uploaded; a placeholder holds their place meanwhile.
  const insertAtCursor = (text: string) => {
    const field = body.current;
    setDraft((current) => {
      const start = field?.selectionStart ?? current.body.length;
      const end = field?.selectionEnd ?? start;
      return { ...current, body: current.body.slice(0, start) + text + current.body.slice(end) };
    });
    setSavedOnce(false);
  };
  const addImages = (files: File[]) => {
    const problems: string[] = [];
    const placeholders: string[] = [];
    const accepted = files.filter((file) => {
      if (!imageTypes.includes(file.type)) { problems.push(kbMessages.imageType(file.name)); return false; }
      if (file.size > maxImageBytes) { problems.push(kbMessages.imageTooLarge(file.name)); return false; }
      return true;
    });
    setImageProblems(problems);
    for (const file of accepted) {
      uploads.current += 1;
      const placeholder = `![${kbMessages.uploadingImage(file.name)}](#upload-${uploads.current})`;
      placeholders.push(placeholder);
      void kbApi.uploadImage(file).then((image) => {
        replacePlaceholder(placeholder, `![${altText(file.name)}](${image.url})`);
        setNotice(kbMessages.imageUploaded(file.name));
      }, (error: unknown) => {
        replacePlaceholder(placeholder, '');
        setImageProblems((current) => [...current, reason(error, kbMessages.imageFailed(file.name))]);
      });
    }
    if (placeholders.length > 0) insertAtCursor(placeholders.join('\n'));
  };
  const replacePlaceholder = (placeholder: string, text: string) =>
    setDraft((current) => (current.body.includes(placeholder) ? { ...current, body: current.body.replace(placeholder, text) } : current));
  const imagesIn = (list: FileList | null) => [...(list ?? [])].filter((file) => file.type.startsWith('image/') || file.type === '');
  const onPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = imagesIn(event.clipboardData.files);
    if (files.length === 0) return;
    event.preventDefault();
    addImages(files);
  };
  const onDrop = (event: DragEvent<HTMLTextAreaElement>) => {
    const files = [...(event.dataTransfer.files ?? [])];
    if (files.length === 0) return;
    event.preventDefault();
    addImages(files);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
      event.preventDefault();
      void save();
    }
  };
  const restoreVersion = async (version: number) => {
    if (!base) return;
    // Restoring replaces the text on screen, so unsaved edits are saved or dropped first, never lost.
    if (dirty) throw new InvalidRequestError(kbMessages.restoreNeedsSave);
    const restored = await kbApi.restoreVersion(base.id, version, base.updated_at);
    setBase(restored);
    setDraft(draftOf(restored));
    client.setQueryData(['kb-article', restored.id], restored);
    void refresh();
  };

  const status = base?.status ?? 'draft';
  const heading = base ? kbMessages.editArticleHeading(base.title) : kbMessages.newArticleHeading;
  return (
    <main className="settings-page kb-editor-page" aria-label={heading}>
      <BackLink />
      <p className="sr-only" role="status" aria-live="polite">{notice}</p>
      <form className="kb-editor" aria-label={heading} onKeyDown={onKeyDown} onSubmit={(event) => { event.preventDefault(); void save(); }}>
        <div className="kb-editor-main">
          <div className="field kb-title-field"><label htmlFor={`${id}-title`}>{kbMessages.articleTitle}</label>
            <input id={`${id}-title`} value={draft.title} required aria-invalid={titleLength > maxTitle || undefined} aria-describedby={titleLength > maxTitle ? `${id}-title-error` : undefined}
              onChange={(event) => set({ title: event.target.value })} />
            {titleLength > maxTitle && <small id={`${id}-title-error`} className="error-copy">{kbMessages.titleTooLong(titleLength - maxTitle)}</small>}</div>
          {base?.has_source_html && <p className="muted-copy">{kbMessages.importedNotice}</p>}
          <div className="kb-editor-card">
            <div className="kb-tabs" role="tablist" aria-label={kbMessages.editorTabs}>
              {(['write', 'preview'] as const).map((name) => (
                <button key={name} type="button" role="tab" id={`${id}-${name}-tab`} aria-controls={`${id}-${name}`} aria-selected={tab === name}
                  tabIndex={tab === name ? 0 : -1} onClick={() => setTab(name)}
                  onKeyDown={(event) => { if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { const next = name === 'write' ? 'preview' : 'write'; setTab(next); document.getElementById(`${id}-${next}-tab`)?.focus(); } }}>
                  {name === 'write' ? kbMessages.write : kbMessages.preview}</button>
              ))}
            </div>
            <div role="tabpanel" id={`${id}-write`} aria-labelledby={`${id}-write-tab`} hidden={tab !== 'write'}>
              <label className="sr-only" htmlFor={`${id}-body`}>{kbMessages.body}</label>
              {/* No maxLength: a browser would cut a long paste silently. The counter refuses it instead. */}
              <textarea id={`${id}-body`} ref={body} className="kb-body" value={draft.body} aria-describedby={`${id}-help ${id}-count`} aria-invalid={tooLong || undefined}
                onChange={(event) => set({ body: event.target.value })} onPaste={onPaste} onDrop={onDrop}
                onDragOver={(event) => { if (event.dataTransfer.types.includes('Files')) event.preventDefault(); }} />
            </div>
            <div role="tabpanel" id={`${id}-preview`} aria-labelledby={`${id}-preview-tab`} hidden={tab !== 'preview'} className="kb-preview-panel" tabIndex={0}>
              {tab === 'preview' && <MarkdownPreview text={draft.body} />}
            </div>
            <div className="kb-editor-foot">
              <small id={`${id}-help`} className="kb-hint">{kbMessages.bodyHelp}</small>
              <small id={`${id}-count`} className={tooLong ? 'error-copy kb-count' : 'kb-count'}>{kbMessages.characters(bodyLength, maxBody)}</small>
            </div>
          </div>
          {tooLong && <p role="alert" className="error-copy">{kbMessages.tooLong(bodyLength - maxBody)}</p>}
          {imageProblems.map((text) => <p key={text} role="alert" className="error-copy">{text}</p>)}
        </div>
        <aside className="kb-editor-side" aria-label={kbMessages.articleSettings}>
          <div className="kb-status">
            <span className="badge" data-status={status}>{status === 'published' ? kbMessages.published : kbMessages.draft}</span>
            <span className="muted-copy">{status === 'published' ? kbMessages.statusPublished : kbMessages.statusDraft}</span>
          </div>
          {conflict && <div role="alert" className="warning-note kb-conflict">
            <p>{kbMessages.changedElsewhere}</p>
            <div className="button-row">
              <button type="button" disabled={busy} onClick={() => void keepMine()}>{kbMessages.keepMine}</button>
              <button type="button" disabled={busy} onClick={() => void loadTheirs()}>{kbMessages.loadTheirs}</button>
            </div>
            <small>{kbMessages.keepMineHelp}</small>
          </div>}
          {problem && <p role="alert" className="error-copy">{problem}</p>}
          <div className="kb-actions">
            <button className="primary-action" type="submit" disabled={busy || invalid || (base !== undefined && !dirty)}>
              {busy ? kbMessages.saving : base ? kbMessages.saveChanges : kbMessages.saveDraft}</button>
            {status === 'published'
              ? <button type="button" disabled={busy || invalid} onClick={() => void save('draft')}>{kbMessages.unpublish}</button>
              : <button type="button" disabled={busy || invalid} onClick={() => void save('published')}>{dirty || !base ? kbMessages.saveAndPublish : kbMessages.publish}</button>}
          </div>
          <p className="kb-hint kb-save-state" aria-hidden="true">{dirty ? kbMessages.unsaved : savedOnce ? kbMessages.saved : ' '}</p>
          <div className="field"><label htmlFor={`${id}-collection`}>{kbMessages.articleCollection}</label>
            <select id={`${id}-collection`} value={draft.collection_id} required onChange={(event) => set({ collection_id: event.target.value })}>
              {!draft.collection_id && <option value="">{kbMessages.chooseCollection}</option>}
              {tree.map((node) => <option key={node.collection.id} value={node.collection.id}>{indent(node)}</option>)}
            </select>
            {collectionsFailed && <small className="error-copy">{kbMessages.collectionsFailed}</small>}</div>
          {base && <div className="field"><label htmlFor={`${id}-slug-input`}>{kbMessages.articleSlug}</label>
            <input id={`${id}-slug-input`} value={draft.slug} aria-describedby={`${id}-slug`} onChange={(event) => set({ slug: event.target.value })} />
            <small id={`${id}-slug`} className="kb-hint">{kbMessages.articleSlugHelp(base.number, draft.slug.trim())}</small></div>}
          {base && <div className="kb-side-actions">
            <button type="button" ref={historyButton} aria-expanded={historyOpen} onClick={() => setHistoryOpen(!historyOpen)}>{kbMessages.history}</button>
            {confirmingDelete
              ? <div role="group" aria-label={kbMessages.deleteArticle} className="kb-confirm">
                  <span>{kbMessages.confirmDeleteArticle(base.title)}</span>
                  <div className="button-row">
                    <button type="button" className="danger-action" disabled={busy} onClick={() => void remove()}>{kbMessages.confirmDelete}</button>
                    <button type="button" disabled={busy} onClick={() => setConfirmingDelete(false)}>{kbMessages.cancel}</button>
                  </div>
                </div>
              : <button type="button" className="danger-action" onClick={() => setConfirmingDelete(true)}>{kbMessages.deleteArticle}</button>}
          </div>}
        </aside>
      </form>
      {base && historyOpen && <SavedReplyHistory<KBArticleVersion> title={base.title} timezone={timezone} load={(before) => kbApi.versions(base.id, before)}
        text={(version) => version.body_md} changes={versionChanges} failedMessage={kbMessages.articleActionFailed}
        onRestore={restoreVersion} onClose={() => { setHistoryOpen(false); historyButton.current?.focus(); }} />}
    </main>
  );
}

// versionChanges names what a version changed from the one before it.
function versionChanges(version: KBArticleVersion, previous: KBArticleVersion): string[] {
  return [
    ...(version.title !== previous.title ? [messages.savedReplyTitleChanged] : []),
    ...(version.slug !== previous.slug ? [kbMessages.slugChanged] : []),
    ...(version.body_md !== previous.body_md ? [messages.savedReplyTextChanged] : []),
    ...(version.status !== previous.status ? [version.status === 'published' ? kbMessages.published : kbMessages.draft] : []),
    ...(version.collection_id !== previous.collection_id ? [kbMessages.movedCollection] : []),
  ];
}

// altText describes an image by its file name, without the characters that would end the Markdown.
function altText(name: string): string {
  const text = name.replace(/\.[^.]+$/, '').replace(/[[\]\\\n]/g, ' ').trim();
  return text || kbMessages.imageAlt;
}
