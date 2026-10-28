import { useCallback, useDeferredValue, useEffect, useId, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import type { KeyboardEvent } from 'react';
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useBlocker, useNavigate } from '@tanstack/react-router';
import { api, ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import { SubmitKeys } from '../../api/submit-key';
import { messages } from '../../messages';
import { RouterLink } from '../../ui/router-link';
import { Part, PartUnavailable } from '../../ui/lazy-part';
import { SavedReplyHistory } from '../settings/saved-reply-panels';
import { ArticleAIBox, ArticleAINote } from './article-ai';
import type { EditorHandle, UploadHooks, ViewMode } from './editor/codemirror-editor';
import { editorPart } from './editor-part';
import { kbApi } from './kb-api';
import type { KBArticle, KBArticleStatus, KBArticleVersion } from './kb-api';
import { kbMessages } from './kb-messages';
import { buildTree, flatten, indent, reason } from './kb-tree';
import { KBDialog, KBMenu, usePhone } from './kb-ui';
import type { MenuItem } from './kb-ui';
import { Icon } from './editor/icons';
import { useRefreshKB } from './knowledge-page';
import { MarkdownPreview, videoIds } from './markdown-preview';
import type { PreviewVideo } from './markdown-preview';
import { mediaApi, updateFields } from './media-api';
import type { KBMediaDetails } from './media-api';
import { embedFor } from './media-grid';
import { baseName, fileProblem, kindOf, uploadVideo } from './media-uploads';
import { addPoster } from './video-poster';

// The limits the server checks (server/internal/domain/kb.go), counted in characters like the server.
export const maxTitle = 300;
export const maxBody = 100000;

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
    return <main className="kb-frame kb-editor-page">{back}<p role="status" className="empty-copy">{messages.loading}</p></main>;
  }
  if (articleId && !article.data) {
    const missing = article.error instanceof Error && article.error.message === 'API 404';
    return <main className="kb-frame kb-editor-page">{back}<p role="alert" className="error-copy">{missing ? kbMessages.articleNotFound : kbMessages.articleLoadFailed}</p></main>;
  }
  if (article.data?.deleted_at) {
    return <main className="kb-frame kb-editor-page">{back}<p role="status" className="warning-note">{kbMessages.articleInTrash}</p></main>;
  }
  const items = collections.data?.items ?? [];
  const known = collectionId && items.some((item) => item.id === collectionId) ? collectionId : undefined;
  return <ArticleEditor article={article.data} collections={items} defaultCollection={known ?? (items.length === 1 ? items[0].id : '')}
    timezone={me.data?.timezone ?? 'UTC'} collectionsFailed={collections.isError} />;
}

function BackLink() {
  const navigate = useNavigate();
  return <RouterLink className="kb-back" href="/knowledge" onFollow={() => void navigate({ to: '/knowledge' })}><Icon name="chevronLeft" />{kbMessages.backToList}</RouterLink>;
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
  const [historyOpen, setHistoryOpen] = useState(false);
  const [notice, setNotice] = useState('');
  const [imageProblems, setImageProblems] = useState<string[]>([]);
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

  // Set from the press until the answer: clicks that land before the button turns disabled save nothing.
  const saving = useRef(false);
  // A new article retried after a failure is sent under the same key, so the server stores it once.
  const createKeys = useRef(new SubmitKeys());
  const save = async (status?: KBArticleStatus, over?: KBArticle) => {
    const body = bodyNow();
    const current = over ?? base;
    const sent = body === draft.body ? draft : { ...draft, body };
    // Typing not yet on the counter is now; a text over the limit shows there and is not sent.
    if (sent !== draft) setDraft(sent);
    if (busy || saving.current || invalid || characters(body) > maxBody) return;
    saving.current = true;
    setBusy(true);
    setProblem(undefined);
    try {
      if (!current) {
        const value = { collection_id: sent.collection_id, title: sent.title.trim(), body_md: sent.body, status: status ?? 'draft' } as const;
        const created = await kbApi.createArticle(value, createKeys.current.keyFor(JSON.stringify(value)));
        createKeys.current.done();
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
      saving.current = false;
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
      setDeleting(false);
      setBusy(false);
    }
  };

  // Images are checked before any upload: a wrong type or a file over 5 MB is refused with a reason. The editor
  // holds an uploading image's place on screen only, never in the text, and puts the image in as a paragraph of
  // its own: right after a table row or inside a line, Markdown would read it as part of the table or the text.
  // A video goes up in parts (media-uploads.ts); leaving the page cancels the ones still going, so they
  // hold none of the workspace's three uploads in progress.
  // The videos waiting their turn stop too: one signal for the editor, made by the effect (so a second run of
  // it, as in development, gets a fresh one) and aborted when it goes.
  const videoUploads = useRef(new Set<{ id?: string }>());
  const videoTurn = useRef<Promise<void>>(Promise.resolve());
  const videoSignal = useRef(new AbortController().signal);
  useEffect(() => {
    const uploads = videoUploads.current;
    let controller = new AbortController();
    videoSignal.current = controller.signal;
    const leave = () => {
      controller.abort();
      for (const session of uploads) cancelVideoUpload(session);
      uploads.clear();
    };
    // A page the browser kept and shows again (Back) uploads again with a fresh signal.
    const back = (event: PageTransitionEvent) => {
      if (!event.persisted) return;
      controller = new AbortController();
      videoSignal.current = controller.signal;
    };
    window.addEventListener('pagehide', leave);
    window.addEventListener('pageshow', back);
    return () => { window.removeEventListener('pagehide', leave); window.removeEventListener('pageshow', back); leave(); };
  }, []);
  const imageHooks = useMemo<UploadHooks>(() => ({
    accept: (files) => {
      const problems: string[] = [];
      const accepted = files.filter((file) => {
        const problem = fileProblem(file);
        if (problem === 'type') problems.push(kbMessages.imageType(file.name));
        if (problem === 'size') problems.push(kindOf(file) === 'video' ? kbMessages.videoTooLarge(file.name) : kbMessages.imageTooLarge(file.name));
        return !problem;
      });
      setImageProblems(problems);
      return accepted;
    },
    upload: async (file, progress, cancelled) => {
      // The name the library shows is the file's; a pasted image without one is "image".
      if (kindOf(file) === 'image') return { snippet: `![${altText(file.name)}](${(await mediaApi.uploadImage(file, baseName(file.name) || 'image', false)).url})` };
      const session: { id?: string } = {};
      // Either the editor going away or the person's Cancel stops it.
      const both = new AbortController();
      for (const signal of [videoSignal.current, cancelled]) {
        if (signal.aborted) both.abort();
        else signal.addEventListener('abort', () => both.abort(), { once: true });
      }
      const signal = both.signal;
      videoUploads.current.add(session);
      // One video at a time, in the order they came: a workspace has three uploads in progress at most.
      const turn = videoTurn.current.then(() => (signal.aborted ? undefined
        : uploadVideo(file, { keep: false, signal, session, onProgress: (sent) => progress(sent / file.size) })));
      videoTurn.current = turn.then(() => undefined, () => undefined);
      try {
        const item = await turn;
        if (!item) throw new DOMException('Cancelled', 'AbortError');
        // The text gets the video at once; its poster, drawn from the file, follows on its own (video-poster.ts).
        void addPoster(file, item);
        return { snippet: embedFor(item) };
      } catch (error) {
        // The editor cannot resume an upload: a failed or cancelled one is thrown away, so it holds none of the
        // workspace's three.
        cancelVideoUpload(session);
        throw error;
      } finally {
        videoUploads.current.delete(session);
      }
    },
    onStart: () => setUploading((count) => count + 1),
    onDone: (file) => { setUploading((count) => count - 1); setNotice(kbMessages.imageUploaded(file.name)); },
    onError: (file, error) => {
      setUploading((count) => count - 1);
      // A cancel is no problem to report.
      if (error instanceof DOMException && error.name === 'AbortError') return;
      setImageProblems((current) => [...current, reason(error, kbMessages.imageFailed(file.name))]);
    },
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
  const combineVideos = useCallback((answers: { data?: KBMediaDetails; isError: boolean; error: unknown }[]) => {
    const known = new Map<string, PreviewVideo>();
    answers.forEach((answer, index) => {
      const videoId = ids[index]!;
      if (answer.data) known.set(videoId, answer.data.kind === 'video' && !answer.data.deleted_at
        ? { poster: answer.data.poster_url, width: answer.data.width, height: answer.data.height } : null);
      // Only a 404 says the library has no such video; a failure that may pass leaves it as not looked up.
      else if (answer.isError && answer.error instanceof Error && answer.error.message === 'API 404') known.set(videoId, null);
    });
    return known;
  }, [ids]);
  const library = useQueries({
    queries: ids.map((videoId) => ({ queryKey: ['kb-media-details', videoId], queryFn: () => mediaApi.details(videoId), retry: false, staleTime: 60_000 })),
    combine: combineVideos,
  });
  // submit saves, or first asks about new images without alt text (set below, once those are known).
  const submit = useRef(() => undefined as void);
  const onKeyDown = (event: KeyboardEvent) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
      event.preventDefault();
      submit.current();
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

  // Excluding from AI or including again changes neither the text nor updated_at, so an edit in progress stays.
  const aiChanged = (changed: KBArticle, text: string) => {
    setBase(changed);
    client.setQueryData(['kb-article', changed.id], changed);
    setNotice(text);
    void refresh();
  };
  // The note goes with the exclusion; the keyboard focus moves to the box's button, which stays.
  const aiChangedFromNote = (changed: KBArticle, text: string) => {
    aiChanged(changed, text);
    requestAnimationFrame(() => document.getElementById(`${id}-ai-button`)?.focus());
  };

  const status = base?.status ?? 'draft';
  const heading = base ? kbMessages.editArticleHeading(base.title) : kbMessages.newArticleHeading;
  const phone = usePhone();
  // The help center decides whether Published is public; the bar and the details say so, with the way to turn it on.
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter });
  const isPublic = center.data?.enabled;
  const collectionName = tree.find((node) => node.collection.id === draft.collection_id)?.collection.name;
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // askingAlt: what waits for the answer about images without alt text, publishing a draft or saving a published article.
  const [askingAlt, setAskingAlt] = useState<'publish' | 'save'>();
  const [altFor, setAltFor] = useState<MissingAlt>();
  // Images in the text without alt text: the side column lists them, and publishing asks about them again.
  const missing = useMemo(() => missingAlt(draft.body), [draft.body]);
  const missingNames = useQueries({
    queries: missing.map((image) => ({ queryKey: ['kb-media-details', image.id ?? image.url], queryFn: () => mediaApi.details(image.id!), enabled: image.id !== undefined, retry: false, staleTime: 60_000 })),
  });
  const nameOf = (image: MissingAlt, at: number) => missingNames[at]?.data?.name || image.fallback;
  const publish = () => { if (missing.length > 0) setAskingAlt('publish'); else void save('published'); };
  // Saving a published article asks too when it brings in an image without alt text the saved version did not
  // have: that image would go on the help center at once.
  const savedMissing = useMemo(() => (base ? missingAlt(base.body_md).map((image) => image.markdown) : []), [base]);
  const newMissing = missing.filter((image, at) => missing.slice(0, at + 1).filter((each) => each.markdown === image.markdown).length
    > savedMissing.filter((markdown) => markdown === image.markdown).length);
  submit.current = () => { if (status === 'published' && newMissing.length > 0) setAskingAlt('save'); else void save(); };
  const disabledSave = busy || invalid || (base !== undefined && !dirty);
  const saveLabel = busy ? kbMessages.saving : base ? kbMessages.saveChanges : kbMessages.saveDraft;
  const publishLabel = dirty || !base ? kbMessages.saveAndPublish : kbMessages.publish;
  const needsCollection = !draft.collection_id ? `${id}-needs-collection` : undefined;
  const actions: MenuItem[] = [
    ...(base ? [
      { label: kbMessages.viewOnHelpCenter, icon: 'external' as const, hint: status !== 'published' ? kbMessages.draft : isPublic === false ? kbMessages.helpCenterOffShort : undefined,
        disabled: status !== 'published' || isPublic !== true, onSelect: () => window.open(base.url, '_blank', 'noopener') },
      { label: kbMessages.copyLink, icon: 'copy' as const, onSelect: () => void navigator.clipboard.writeText(base.url).then(() => setNotice(kbMessages.linkCopied)) },
      { label: kbMessages.versionHistory, icon: 'history' as const, onSelect: () => setHistoryOpen(true) },
    ] : []),
    status === 'published'
      ? { label: kbMessages.unpublish, icon: 'globe' as const, disabled: busy || invalid, onSelect: () => void save('draft') }
      : { label: publishLabel, icon: 'globe' as const, disabled: busy || invalid, onSelect: publish },
    ...(base ? [{ label: kbMessages.deleteArticleMenu, icon: 'trash' as const, danger: true, separated: true, onSelect: () => setDeleting(true) }] : []),
  ];
  const stateWords = [status === 'published' ? (isPublic === false ? kbMessages.notPublicShort : '') : '', dirty ? kbMessages.unsaved : savedOnce ? kbMessages.saved : ''].filter(Boolean);
  const details = (
    <>
      <section className="kb-side-box" aria-labelledby={`${id}-visibility`}>
        <h3 id={`${id}-visibility`}>{kbMessages.visibility}</h3>
        <div className="kb-side-status">
          <span className="kb-dot-status" data-status={status}>{status === 'published' ? kbMessages.published : kbMessages.draft}</span>
          <span>{status === 'published' ? (isPublic === false ? kbMessages.notPublicHelpCenter : kbMessages.statusPublished) : kbMessages.statusDraft}</span>
        </div>
        {isPublic === false && <RouterLink className="button-link" href="/settings#help-center-heading" onFollow={() => void navigate({ to: '/settings', hash: 'help-center-heading' })}>
          <Icon name="globe" />{kbMessages.turnOnHelpCenter}</RouterLink>}
      </section>
      {base && !phone && <ArticleAIBox article={base} timezone={timezone} headingId={`${id}-ai`} onChanged={aiChanged} />}
      {missing.length > 0 && <section className="kb-alt-warn" aria-labelledby={`${id}-alt-warn`}>
        <strong id={`${id}-alt-warn`}>{kbMessages.imagesWithoutAlt(missing.length)}</strong>
        <span>{kbMessages.imagesWithoutAltHelp}</span>
        <ul>{missing.slice(0, 3).map((image, at) => (
          <li key={`${image.url}-${at}`}><span title={nameOf(image, at)}>{nameOf(image, at)}</span>
            <button type="button" aria-label={kbMessages.addAltFor(nameOf(image, at))} onClick={() => { setDetailsOpen(false); setAltFor({ ...image, fallback: nameOf(image, at) }); }}>{kbMessages.addAlt}</button></li>
        ))}</ul>
        {missing.length > 3 && <span className="kb-hint">{kbMessages.andMoreImages(missing.length - 3)}</span>}
      </section>}
      <section className="kb-side-box" aria-labelledby={`${id}-placement`}>
        <h3 id={`${id}-placement`}>{kbMessages.placement}</h3>
        <div className="field"><label htmlFor={`${id}-collection`}>{kbMessages.articleCollection}</label>
          <select id={`${id}-collection`} value={draft.collection_id} required onChange={(event) => set({ collection_id: event.target.value })}>
            {!draft.collection_id && <option value="">{kbMessages.chooseCollection}</option>}
            {tree.map((node) => <option key={node.collection.id} value={node.collection.id}>{indent(node)}</option>)}
          </select>
          {collectionsFailed && <small className="error-copy">{kbMessages.collectionsFailed}</small>}
          {!draft.collection_id && <small id={`${id}-needs-collection`} className="kb-hint">{kbMessages.needsCollection}</small>}</div>
        {base && <div className="field"><label htmlFor={`${id}-slug-input`}>{kbMessages.articleSlug}</label>
          <input id={`${id}-slug-input`} value={draft.slug} aria-describedby={`${id}-slug`} onChange={(event) => set({ slug: event.target.value })} />
          <small id={`${id}-slug`} className="kb-hint">{kbMessages.articleSlugHelp(base.number, draft.slug.trim())}</small></div>}
      </section>
    </>
  );
  const more = <KBMenu label={kbMessages.moreArticleActions} title={draft.title.trim() || heading} items={actions} buttonClassName="kb-more-button" trigger={<Icon name="more" />} />;
  return (
    <main className="kb-frame kb-editor-page" aria-label={heading}>
      <p className="sr-only" role="status" aria-live="polite">{notice}</p>
      <header className="kb-art-bar">
        <nav className="kb-crumbs" aria-label={kbMessages.breadcrumb}>
          <BackLink />
          {collectionName && <><span aria-hidden="true">/</span><RouterLink href={`/knowledge?${new URLSearchParams({ collection: draft.collection_id })}`}
            onFollow={() => void navigate({ to: '/knowledge', search: { collection: draft.collection_id } })}>{collectionName}</RouterLink></>}
          <span aria-hidden="true">/</span><span aria-current="page">{draft.title.trim() || heading}</span>
        </nav>
        {!phone && <>
          <span className="kb-art-state">
            <span className="kb-dot-status" data-status={status}>{status === 'published' ? kbMessages.published : kbMessages.draft}</span>
            {stateWords.map((word) => <span key={word}>· {word}</span>)}
          </span>
          <span className="kb-art-acts">
            {status !== 'published' && <button type="button" disabled={busy || invalid} aria-describedby={needsCollection} onClick={publish}>{publishLabel}</button>}
            <button className="primary-action" type="submit" form={`${id}-form`} disabled={disabledSave} aria-describedby={needsCollection}>{saveLabel}</button>
            {more}
          </span>
        </>}
      </header>
      {(conflict || problem || tooLong || uploading > 0 || imageProblems.length > 0 || base?.has_source_html || (base?.ai_exclusion && !phone)) && <div className="kb-editor-notes">
        {base?.ai_exclusion && !phone && <ArticleAINote article={base} onChanged={aiChangedFromNote} />}
        {conflict && <div role="alert" className="warning-note kb-conflict">
          <p>{kbMessages.changedElsewhere}</p>
          <div className="button-row">
            <button type="button" disabled={busy} onClick={() => void keepMine()}>{kbMessages.keepMine}</button>
            <button type="button" disabled={busy} onClick={() => void loadTheirs()}>{kbMessages.loadTheirs}</button>
          </div>
          <small>{kbMessages.keepMineHelp}</small>
        </div>}
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        {tooLong && <p role="alert" className="error-copy">{kbMessages.tooLong(bodyLength - maxBody)}</p>}
        {uploading > 0 && <p role="status" className="kb-hint">{kbMessages.waitingForImages}</p>}
        {imageProblems.map((text) => <p key={text} role="alert" className="error-copy">{text}</p>)}
        {base?.has_source_html && <p className="muted-copy">{kbMessages.importedNotice}</p>}
      </div>}
      {/* On a phone the box stands at the top of the page, as the note does on a wide screen: the way out and back is never behind Details. */}
      {base && phone && <ArticleAIBox article={base} timezone={timezone} headingId={`${id}-ai`} onChanged={aiChanged} />}
      <form id={`${id}-form`} className="kb-editor" aria-label={heading} onKeyDown={onKeyDown} onSubmit={(event) => { event.preventDefault(); submit.current(); }}>
        <div className="kb-editor-main">
          <div className="field kb-title-field"><label htmlFor={`${id}-title`} className="sr-only">{kbMessages.articleTitle}</label>
            <input id={`${id}-title`} value={draft.title} required placeholder={kbMessages.articleTitle} aria-invalid={titleLength > maxTitle || undefined} aria-describedby={titleLength > maxTitle ? `${id}-title-error` : undefined}
              onChange={(event) => set({ title: event.target.value })} />
            {titleLength > maxTitle && <small id={`${id}-title-error`} className="error-copy">{kbMessages.titleTooLong(titleLength - maxTitle)}</small>}</div>
          <div className="kb-editor-card">
            <div className="kb-editor-head">
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
              <small id={`${id}-count`} className={tooLong ? 'error-copy kb-count' : 'kb-count'}>{kbMessages.characters(bodyLength, maxBody)}</small>
              <small id={`${id}-help`} className="sr-only">{kbMessages.bodyHelp}</small>
            </div>
            <div role="tabpanel" id={`${id}-panel`} className="kb-editor-panel" aria-labelledby={`${id}-${mode}-tab`}>
              <Part part={editorPart} failed={<div className="kb-editor-loading"><PartUnavailable /></div>} render={({ default: Editor }) => {
                madeWith.current ??= draft.body;
                return <Editor initial={madeWith.current} uploads={imageHooks} label={kbMessages.body} describedBy={`${id}-help ${id}-count`} invalid={tooLong} mode={mode}
                  preview={mode === 'edit' || !previewReady || previewText === null ? undefined : <MarkdownPreview text={previewText} library={library} />} onChange={(text) => set({ body: text })} onReady={onReady} />;
              }} />
              {!handle && <p role="status" className="kb-editor-loading kb-hint">{kbMessages.loadingEditor}</p>}
            </div>
          </div>
        </div>
        {!phone && <aside className="kb-editor-side" aria-label={kbMessages.articleSettings}>{details}</aside>}
      </form>
      {phone && <div className="kb-phone-bar" role="region" aria-label={kbMessages.articleActions}>
        <span className="kb-phone-state">
          <span className="kb-dot-status" data-status={status}>{status === 'published' ? kbMessages.published : kbMessages.draft}</span>
          <span>{stateWords.join(' · ')}</span>
        </span>
        <button type="button" className="kb-details-button" aria-haspopup="dialog" aria-label={kbMessages.detailsLabel(missing.length)} onClick={() => setDetailsOpen(true)}>
          {kbMessages.details}{missing.length > 0 && <span className="kb-badge-count" aria-hidden="true">{missing.length}</span>}</button>
        {more}
        {/* A saved draft has nothing to save: its next step, Publish, takes the place of Save. */}
        {status !== 'published' && base && !dirty
          ? <button type="button" className="primary-action" disabled={busy || invalid} onClick={publish}>{kbMessages.publish}</button>
          : <button className="primary-action" type="submit" form={`${id}-form`} disabled={disabledSave} aria-describedby={needsCollection}>{saveLabel}</button>}
      </div>}
      {phone && detailsOpen && <KBDialog title={kbMessages.details} className="kb-details-sheet" fieldFirst={false} closeLabel={kbMessages.closeDetails} onClose={() => setDetailsOpen(false)}>{details}</KBDialog>}
      {deleting && base && <KBDialog title={kbMessages.deleteHeading(base.title)} role="alertdialog" onClose={() => setDeleting(false)} footer={<>
        <button type="button" disabled={busy} onClick={() => setDeleting(false)}>{kbMessages.cancel}</button>
        <button type="button" className="danger-solid" disabled={busy} onClick={() => void remove()}>{kbMessages.confirmDelete}</button>
      </>}><p className="muted-copy">{kbMessages.deleteBody}</p></KBDialog>}
      {askingAlt && <KBDialog title={kbMessages.imagesWithoutAlt(missing.length)} role="alertdialog" onClose={() => setAskingAlt(undefined)} footer={<>
        <button type="button" onClick={() => setAskingAlt(undefined)}>{kbMessages.cancel}</button>
        <button type="button" className="primary-action" disabled={busy || invalid} onClick={() => { const then = askingAlt; setAskingAlt(undefined); void save(then === 'publish' ? 'published' : undefined); }}>
          {askingAlt === 'publish' ? kbMessages.publishAnyway : kbMessages.saveAnyway}</button>
      </>}>
        <p className="muted-copy">{askingAlt === 'publish' ? kbMessages.publishAltHelp : kbMessages.saveAltHelp}</p>
        <ul className="kb-alt-list">{missing.map((image, at) => (
          <li key={`${image.url}-${at}`}><span title={nameOf(image, at)}>{nameOf(image, at)}</span>
            <button type="button" aria-label={kbMessages.addAltFor(nameOf(image, at))} onClick={() => { setAskingAlt(undefined); setAltFor({ ...image, fallback: nameOf(image, at) }); }}>{kbMessages.addAlt}</button></li>
        ))}</ul>
      </KBDialog>}
      {altFor && <AltDialog image={altFor} library={missingNames[missing.findIndex((each) => each.markdown === altFor.markdown)]?.data}
        onSave={(alt) => { handle?.replace(altFor.markdown, altFor.markdown.replace(/^!\[\s*\]/, `![${alt.replace(/[[\]\\\n]/g, ' ')}]`)); setAltFor(undefined); }}
        onClose={() => setAltFor(undefined)} />}
      {/* key: a save, a restore or an AI exclusion adds a version, so an open history loads again. */}
      {base && historyOpen && <KBDialog title={kbMessages.versionHistory} className="kb-history" phone="full" closeLabel={kbMessages.closeHistory}
        onClose={() => setHistoryOpen(false)}>
        <SavedReplyHistory<KBArticleVersion> key={`${base.updated_at}-${base.last_change?.version}`} title={base.title} timezone={timezone} load={(before) => kbApi.versions(base.id, before)}
          text={(version) => version.body_md} changes={versionChanges} failedMessage={kbMessages.articleActionFailed}
          onRestore={restoreVersion} onClose={() => setHistoryOpen(false)} />
      </KBDialog>}
    </main>
  );
}

interface MissingAlt {
  // markdown is the image as the text holds it, `![](url)`; id the library item when the address is one.
  markdown: string;
  url: string;
  id?: string;
  fallback: string;
}

// missingAlt lists the images of a text that have no alt text, in order.
export function missingAlt(text: string): MissingAlt[] {
  const found: MissingAlt[] = [];
  for (const match of text.matchAll(/!\[\s*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)/g)) {
    const url = match[1]!;
    const id = /\/api\/v1\/kb\/images\/([0-9a-f-]{36})/.exec(url)?.[1];
    found.push({ markdown: match[0], url, id, fallback: kbMessages.imageNumber(found.length + 1) });
  }
  return found;
}

// AltDialog asks for an image's alt text; it goes in the text, and in the library when the library has none.
function AltDialog({ image, library, onSave, onClose }: { image: MissingAlt; library?: KBMediaDetails; onSave: (alt: string) => void; onClose: () => void }) {
  const [alt, setAlt] = useState('');
  const [busy, setBusy] = useState(false);
  const save = async () => {
    const text = alt.trim();
    if (!text || busy) return;
    setBusy(true);
    // The library keeps the alt text for the next article; kept is sent as it is (a new alt text would keep it).
    if (library && !library.alt.trim()) await updateFields(library, { alt: text, kept: library.kept }).catch(() => undefined);
    onSave(text);
  };
  return (
    <KBDialog title={kbMessages.altFor(image.fallback)} onClose={onClose} footer={<>
      <button type="button" onClick={onClose}>{kbMessages.cancel}</button>
      <button type="button" className="primary-action" disabled={!alt.trim() || busy} onClick={() => void save()}>{kbMessages.addAlt}</button>
    </>}>
      <img className="kb-alt-preview" src={image.url} alt="" />
      <label className="field">{kbMessages.altLabel}
        <input value={alt} maxLength={500} placeholder={kbMessages.pickerAltPlaceholder} onChange={(event) => setAlt(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void save(); } }} /></label>
      <p className="kb-hint">{kbMessages.pickerSavedToLibrary}</p>
    </KBDialog>
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

// cancelVideoUpload throws an unfinished video upload away on the server; keepalive outlives a closing page.
function cancelVideoUpload(session: { id?: string }) {
  if (!session.id) return;
  void fetch(`/api/v1/kb/media-uploads/${session.id}`, { method: 'DELETE', credentials: 'same-origin', keepalive: true }).catch(() => undefined);
  session.id = undefined;
}

// altText describes an image by its file name, without the characters that would end the Markdown.
function altText(name: string): string {
  const text = name.replace(/\.[^.]+$/, '').replace(/[[\]\\\n]/g, ' ').trim();
  return text || kbMessages.imageAlt;
}
