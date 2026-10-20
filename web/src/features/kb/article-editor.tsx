import { useCallback, useDeferredValue, useEffect, useId, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import type { KeyboardEvent } from 'react';
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useBlocker, useNavigate } from '@tanstack/react-router';
import { api, ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import { messages } from '../../messages';
import { RouterLink } from '../../ui/router-link';
import { ChevronLeftIcon } from '../../ui/icons';
import { Part, PartUnavailable } from '../../ui/lazy-part';
import { SavedReplyHistory } from '../settings/saved-reply-panels';
import type { EditorHandle, UploadHooks, ViewMode } from './editor/codemirror-editor';
import { editorPart } from './editor-part';
import { kbApi } from './kb-api';
import type { KBArticle, KBArticleStatus, KBArticleVersion } from './kb-api';
import { kbMessages } from './kb-messages';
import { buildTree, flatten, indent, reason } from './kb-tree';
import { useRefreshKB } from './knowledge-page';
import { MarkdownPreview, videoIds } from './markdown-preview';
import type { PreviewVideo } from './markdown-preview';
import { mediaApi } from './media-api';
import type { KBMediaDetails } from './media-api';
import { embedFor } from './media-grid';
import { kindOf, uploadVideo } from './media-uploads';

// The limits the server checks (server/internal/domain/kb.go), counted in characters like the server.
export const maxTitle = 300;
export const maxBody = 100000;
const maxImageBytes = 5 * 1024 * 1024;

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
  // The editor's chunk loads alongside the article, not after it.
  useEffect(editorPart.preload, []);
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

// Side by side needs a wide window: the text and the preview each get half of the column left of the settings.
const wideQuery = '(min-width: 1180px)';
const subscribeWide = (change: () => void) => {
  const query = window.matchMedia?.(wideQuery);
  query?.addEventListener('change', change);
  return () => query?.removeEventListener('change', change);
};
const isWide = () => window.matchMedia?.(wideQuery).matches ?? false;
// A longer article opens in Write even in a wide window: drawing its preview takes a long task (around 200 ms
// for 100,000 characters on a slow computer) after every pause in typing. Side by side is a click away.
export const splitLimit = 20000;

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
  const wide = useSyncExternalStore(subscribeWide, isWide);
  const [chosen, setMode] = useState<ViewMode>(() => (isWide() && (!article || characters(article.body_md) <= splitLimit) ? 'split' : 'edit'));
  const mode: ViewMode = chosen === 'split' && !wide ? 'edit' : chosen;
  const modes: ViewMode[] = wide ? ['edit', 'split', 'preview'] : ['edit', 'preview'];
  const [handle, setHandle] = useState<EditorHandle>();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [conflict, setConflict] = useState(false);
  const [savedOnce, setSavedOnce] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [notice, setNotice] = useState('');
  const [imageProblems, setImageProblems] = useState<string[]>([]);
  const historyButton = useRef<HTMLButtonElement>(null);
  // A refused save says why where the person can see it: on a phone the side column is below the text.
  const conflictNote = useRef<HTMLDivElement>(null);
  useEffect(() => { if (conflict) conflictNote.current?.scrollIntoView?.({ block: 'nearest' }); }, [conflict]);
  const set = (change: Partial<Draft>) => { setSavedOnce(false); setDraft((current) => ({ ...current, ...change })); };
  // sameBody: the editor writes every line ending as \n, so a text that differs from the stored one only there (an
  // import or an API client wrote \r\n) is the article's own text, not an edit.
  const sameBody = (text: string, of: KBArticle) => text === of.body_md || text === of.body_md.replace(/\r\n?/g, '\n');
  const isDirty = (form: Draft) => (base
    // Compared as save() sends them: title and address name trimmed, and an emptied address name is not
    // a change (the server keeps the old one), so a form is never dirty with nothing to send.
    ? form.title.trim() !== base.title || (form.slug.trim() !== '' && form.slug.trim() !== base.slug) || !sameBody(form.body, base) || form.collection_id !== base.collection_id
    : form.title.trim() !== '' || form.body.trim() !== '');
  const dirty = isDirty(draft);
  // bodyNow reads the editor at once: it tells the page of typing a moment after it stops.
  const bodyNow = () => handle?.markdown() ?? draft.body;
  // A person leaving with unsaved text is asked first; a save that moves on (a new article, a delete) is not.
  const leaving = useRef(false);
  const unsaved = useRef(() => dirty);
  unsaved.current = () => isDirty({ ...draft, body: bodyNow() });
  useBlocker({
    shouldBlockFn: () => !leaving.current && unsaved.current() && !window.confirm(kbMessages.leaveUnsaved),
    enableBeforeUnload: () => !leaving.current && unsaved.current(),
  });
  const titleLength = characters(draft.title);
  const bodyLength = characters(draft.body);
  const tooLong = bodyLength > maxBody;
  // Images still uploading hold a placeholder in the text; saving waits for them so it is never stored.
  const [uploading, setUploading] = useState(0);
  const invalid = !draft.title.trim() || titleLength > maxTitle || tooLong || !draft.collection_id || uploading > 0;

  const save = async (status?: KBArticleStatus, over?: KBArticle) => {
    const body = bodyNow();
    const current = over ?? base;
    const sent = body === draft.body ? draft : { ...draft, body };
    // Typing not yet on the counter is now; a text over the limit shows there and is not sent.
    if (sent !== draft) setDraft(sent);
    if (busy || invalid || characters(body) > maxBody) return;
    setBusy(true);
    setProblem(undefined);
    try {
      if (!current) {
        const created = await kbApi.createArticle({ collection_id: sent.collection_id, title: sent.title.trim(), body_md: sent.body, status: status ?? 'draft' });
        client.setQueryData(['kb-article', created.id], created);
        void refresh();
        leaving.current = true;
        await navigate({ to: '/knowledge/articles/$articleId', params: { articleId: created.id }, replace: true });
        return;
      }
      const change = {
        ...(sent.title.trim() !== current.title ? { title: sent.title.trim() } : {}),
        ...(sent.slug.trim() && sent.slug.trim() !== current.slug ? { slug: sent.slug.trim() } : {}),
        ...(!sameBody(sent.body, current) ? { body_md: sent.body } : {}),
        ...(sent.collection_id !== current.collection_id ? { collection_id: sent.collection_id } : {}),
        ...(status && status !== current.status ? { status } : {}),
      };
      // Nothing changed (Ctrl+S on a saved article): there is nothing to ask the server.
      if (Object.keys(change).length === 0) return;
      const updated = await kbApi.updateArticle(current.id, { updated_at: current.updated_at, ...change });
      setBase(updated);
      // The server may write the title or address name its own way; text typed during the save stays.
      setDraft((now) => (now === sent ? { ...draftOf(updated), body: sent.body } : now));
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
  // show puts an article's text on screen, in the form and in the editor.
  const show = (shown: KBArticle) => {
    setDraft(draftOf(shown));
    handle?.load(shown.body_md);
  };
  const loadTheirs = async () => {
    const fresh = await latest();
    if (!fresh) return;
    show(fresh);
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

  // Images are checked before any upload: a wrong type or a file over 5 MB is refused with a reason. The editor
  // holds an uploading image's place on screen only, never in the text, and puts the image in as a paragraph of
  // its own: right after a table row or inside a line, Markdown would read it as part of the table or the text.
  // A video goes up in parts (media-uploads.ts); leaving the page cancels the ones still going, so they
  // hold none of the workspace's three uploads in progress.
  const videoUploads = useRef(new Map<File, { id?: string }>());
  useEffect(() => {
    const uploads = videoUploads.current;
    const leave = () => {
      for (const session of uploads.values())
        if (session.id) void fetch(`/api/v1/kb/media-uploads/${session.id}`, { method: 'DELETE', credentials: 'same-origin', keepalive: true }).catch(() => undefined);
      uploads.clear();
    };
    window.addEventListener('pagehide', leave);
    return () => { window.removeEventListener('pagehide', leave); leave(); };
  }, []);
  const imageHooks = useMemo<UploadHooks>(() => ({
    accept: (files) => {
      const problems: string[] = [];
      const accepted = files.filter((file) => {
        const kind = kindOf(file);
        if (!kind) { problems.push(kbMessages.imageType(file.name)); return false; }
        if (kind === 'image' && file.size > maxImageBytes) { problems.push(kbMessages.imageTooLarge(file.name)); return false; }
        return true;
      });
      setImageProblems(problems);
      return accepted;
    },
    upload: async (file, progress) => {
      if (kindOf(file) === 'image') return { snippet: `![${altText(file.name)}](${(await kbApi.uploadImage(file)).url})` };
      const session: { id?: string } = {};
      videoUploads.current.set(file, session);
      try {
        const item = await uploadVideo(file, { keep: false, signal: new AbortController().signal, session, onProgress: (sent) => progress(sent / file.size) });
        if (!item) throw new Error(kbMessages.imageFailed(file.name));
        return { snippet: embedFor(item) };
      } finally {
        videoUploads.current.delete(file);
      }
    },
    onStart: () => setUploading((count) => count + 1),
    onDone: (file) => { setUploading((count) => count - 1); setNotice(kbMessages.imageUploaded(file.name)); },
    onError: (file, error) => { setUploading((count) => count - 1); setImageProblems((current) => [...current, reason(error, kbMessages.imageFailed(file.name))]); },
  }), []);
  // onReady: the editor shows the text it was made with; one loaded meanwhile (a restore, theirs) goes in now.
  const draftNow = useRef(draft);
  draftNow.current = draft;
  const madeWith = useRef<string>(undefined);
  // The preview first draws after the editor is on screen, so opening never waits for it.
  const [previewReady, setPreviewReady] = useState(false);
  const onReady = (ready: EditorHandle) => {
    setHandle(ready);
    requestAnimationFrame(() => { setTimeout(() => setPreviewReady(true), 0); });
    if (madeWith.current !== undefined && madeWith.current !== draftNow.current.body) ready.load(draftNow.current.body);
  };
  // The preview follows the text at low priority: rendering a long article takes tens of milliseconds, and typing
  // goes first (the editor tells the page of typing 120 ms after it stops, then this renders). It starts empty
  // (null) so the first frame shows the editor without waiting for it.
  const previewText = useDeferredValue<string | null>(draft.body, null);
  // The library videos the text names, for their poster and size in the preview; a 404 is a video the
  // library does not have, which the preview leaves out and names, as the help center does.
  const ids = useMemo(() => (previewText === null ? [] : videoIds(previewText)), [previewText]);
  // combine runs again only when an answer changes (it is stable for the same ids), so the preview gets the
  // same map until then and does not render again on every keystroke.
  const combineVideos = useCallback((answers: { data?: KBMediaDetails; isError: boolean }[]) => {
    const known = new Map<string, PreviewVideo>();
    answers.forEach((answer, index) => {
      const videoId = ids[index]!;
      if (answer.data) known.set(videoId, answer.data.kind === 'video' && !answer.data.deleted_at
        ? { poster: answer.data.poster_url, width: answer.data.width, height: answer.data.height } : null);
      else if (answer.isError) known.set(videoId, null);
    });
    return known;
  }, [ids]);
  const library = useQueries({
    queries: ids.map((videoId) => ({ queryKey: ['kb-media-details', videoId], queryFn: () => mediaApi.details(videoId), retry: false, staleTime: 60_000 })),
    combine: combineVideos,
  });
  const onKeyDown = (event: KeyboardEvent) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
      event.preventDefault();
      void save();
    }
  };
  const restoreVersion = async (version: number) => {
    if (!base) return;
    // Restoring replaces the text on screen, so unsaved edits are saved or dropped first, never lost.
    if (unsaved.current()) throw new InvalidRequestError(kbMessages.restoreNeedsSave);
    const restored = await kbApi.restoreVersion(base.id, version, base.updated_at);
    setBase(restored);
    show(restored);
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
              {modes.map((name, at) => (
                <button key={name} type="button" role="tab" id={`${id}-${name}-tab`} aria-controls={`${id}-panel`} aria-selected={mode === name}
                  tabIndex={mode === name ? 0 : -1} onClick={() => setMode(name)}
                  onKeyDown={(event) => {
                    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
                    const next = modes[(at + (event.key === 'ArrowRight' ? 1 : modes.length - 1)) % modes.length];
                    setMode(next);
                    document.getElementById(`${id}-${next}-tab`)?.focus();
                  }}>
                  {name === 'edit' ? kbMessages.write : name === 'split' ? kbMessages.split : kbMessages.preview}</button>
              ))}
            </div>
            <div role="tabpanel" id={`${id}-panel`} aria-labelledby={`${id}-${mode}-tab`}>
              <Part part={editorPart} failed={<div className="kb-editor-loading"><PartUnavailable /></div>} render={({ default: Editor }) => {
                madeWith.current ??= draft.body;
                return <Editor initial={madeWith.current} uploads={imageHooks} label={kbMessages.body} describedBy={`${id}-help ${id}-count`} invalid={tooLong} mode={mode}
                  preview={mode === 'edit' || !previewReady || previewText === null ? undefined : <MarkdownPreview text={previewText} library={library} />} onChange={(text) => set({ body: text })} onReady={onReady} />;
              }} />
              {!handle && <p role="status" className="kb-editor-loading kb-hint">{kbMessages.loadingEditor}</p>}
            </div>
            <div className="kb-editor-foot">
              <small id={`${id}-help`} className="kb-hint">{kbMessages.bodyHelp}</small>
              <small id={`${id}-count`} className={tooLong ? 'error-copy kb-count' : 'kb-count'}>{kbMessages.characters(bodyLength, maxBody)}</small>
            </div>
          </div>
          {tooLong && <p role="alert" className="error-copy">{kbMessages.tooLong(bodyLength - maxBody)}</p>}
          {uploading > 0 && <p role="status" className="kb-hint">{kbMessages.waitingForImages}</p>}
          {imageProblems.map((text) => <p key={text} role="alert" className="error-copy">{text}</p>)}
        </div>
        <aside className="kb-editor-side" aria-label={kbMessages.articleSettings}>
          <div className="kb-status">
            <span className="badge" data-status={status}>{status === 'published' ? kbMessages.published : kbMessages.draft}</span>
            <span className="muted-copy">{status === 'published' ? kbMessages.statusPublished : kbMessages.statusDraft}</span>
          </div>
          {conflict && <div role="alert" ref={conflictNote} className="warning-note kb-conflict">
            <p>{kbMessages.changedElsewhere}</p>
            <div className="button-row">
              <button type="button" disabled={busy} onClick={() => void keepMine()}>{kbMessages.keepMine}</button>
              <button type="button" disabled={busy} onClick={() => void loadTheirs()}>{kbMessages.loadTheirs}</button>
            </div>
            <small>{kbMessages.keepMineHelp}</small>
          </div>}
          {problem && <p role="alert" className="error-copy">{problem}</p>}
          <div className="kb-actions">
            <button className="primary-action" type="submit" disabled={busy || invalid || (base !== undefined && !dirty)} aria-describedby={!draft.collection_id ? `${id}-needs-collection` : undefined}>
              {busy ? kbMessages.saving : base ? kbMessages.saveChanges : kbMessages.saveDraft}</button>
            {status === 'published'
              ? <button type="button" disabled={busy || invalid} onClick={() => void save('draft')}>{kbMessages.unpublish}</button>
              : <button type="button" disabled={busy || invalid} aria-describedby={!draft.collection_id ? `${id}-needs-collection` : undefined} onClick={() => void save('published')}>{dirty || !base ? kbMessages.saveAndPublish : kbMessages.publish}</button>}
          </div>
          {!draft.collection_id && <p id={`${id}-needs-collection`} className="kb-hint">{kbMessages.needsCollection}</p>}
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
      {/* key: a save or a restore adds a version, so an open history loads again. */}
      {base && historyOpen && <SavedReplyHistory<KBArticleVersion> key={base.updated_at} title={base.title} timezone={timezone} load={(before) => kbApi.versions(base.id, before)}
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
