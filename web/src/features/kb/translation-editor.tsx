import { useEffect, useId, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ChangedElsewhereError } from '../../api/client';
import { useCreateAction } from '../../api/use-create';
import { SavedReplyHistory } from '../settings/saved-reply-panels';
import { ArticleAttachments } from './article-attachments';
import { ArticleDraftBar, draftMessages, editableTranslation } from './article-draft';
import { attachmentMessages } from './attachment-messages';
import { ArticleReviewBox, useTextApproval } from './article-review';
import { kbApi } from './kb-api';
import type { KBArticle, KBArticleStatus, KBAttachment, KBTranslation, KBTranslationVersion } from './kb-api';
import { kbMessages } from './kb-messages';
import { ConflictCompare } from './conflict-compare';
import { KBDialog, KBMenu } from './kb-ui';
import { HelpTip } from '../../ui/help-tip';
import { Icon } from './editor/icons';
import { reason } from './kb-tree';
import { MarkdownPreview } from './markdown-preview';
import { useTranslationAutosave } from './translation-autosave';
import { languageName, translationMessages as t } from './translation-messages';

// TranslationRoute is one language of an article: its text beside the original's. The article says
// whether the translation exists (in the trash too); one that does not yet is started here from it.
export function TranslationRoute({ articleId, locale }: { articleId: string; locale: string }) {
  const article = useQuery({ queryKey: ['kb-article', articleId], queryFn: () => kbApi.article(articleId) });
  const exists = article.data?.translations.some((each) => each.locale === locale);
  // Read again on every return to the tab, however fresh: a save made elsewhere meanwhile is shown at once
  // (the editor keeps what is typed and says so), not only when Save is refused.
  const translation = useQuery({ queryKey: ['kb-translation', articleId, locale], queryFn: () => kbApi.translation(articleId, locale), enabled: exists === true,
    refetchOnWindowFocus: 'always' });
  const languages = useQuery({ queryKey: ['kb-languages'], queryFn: kbApi.languages });
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  if (translation.isError || article.isError) {
    return <main className="kb-frame kb-translation-page"><p role="alert" className="error-copy">{article.isError ? t.notFound : t.failed}</p>
      <a className="button-link" href={`/knowledge/articles/${articleId}`}>{t.back}</a></main>;
  }
  if (article.isPending || (exists && translation.isPending)) {
    return <main className="kb-frame kb-translation-page"><p role="status" className="muted-copy">{t.loading}</p></main>;
  }
  const visible = languages.data?.languages.find((language) => language.locale === locale)?.visible;
  const listed = languages.data ? languages.data.languages.some((language) => language.locale === locale) : true;
  // key: a translation started here becomes one that exists; a save elsewhere never remounts the editor
  // (it would drop what the person is typing): the editor compares it with what it edits against.
  return <TranslationEditor key={exists ? 'existing' : 'new'} articleId={articleId} locale={locale} latest={exists ? translation.data : undefined}
    article={article.data} visible={visible} listed={listed} timezone={me.data?.timezone ?? 'UTC'} />;
}

interface EditorProps {
  articleId: string;
  locale: string;
  // latest is the translation as the server has it now; the editor works against the one it opened (base).
  latest?: KBTranslation;
  article?: KBArticle;
  visible?: boolean;
  listed: boolean;
  timezone: string;
}

interface Draft {
  title: string;
  body_md: string;
  slug: string;
  seo_title: string;
  seo_description: string;
}

const modes = ['write', 'preview'] as const;

// draftOf is the text the form shows: a published translation's unpublished changes when it has them.
const draftOf = (raw?: KBTranslation): Draft => {
  const translation = raw && editableTranslation(raw);
  return {
  title: translation?.title ?? '', body_md: translation?.body_md ?? '', slug: translation?.slug ?? '',
  seo_title: translation?.seo_title ?? '', seo_description: translation?.seo_description ?? '',
  };
};

function TranslationEditor({ articleId, locale, latest, article, visible, listed, timezone }: EditorProps) {
  const id = useId();
  const client = useQueryClient();
  const name = languageName(locale);
  // translation is the version being edited: what the person opened, or last saved or loaded.
  const [translation, setTranslation] = useState<KBTranslation | undefined>(latest);
  const original = translation?.original ?? (article ? { title: article.title, body_md: article.body_md, locale: '', status: article.status } : undefined);
  const [draft, setDraft] = useState<Draft>(() => draftOf(latest));
  const [upToDate, setUpToDate] = useState(false);
  const [mode, setMode] = useState<'write' | 'preview'>('write');
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [stale, setStale] = useState(false);
  // theirs is the newer copy, shown beside the typed text until the person picks one.
  const [theirs, setTheirs] = useState<KBTranslation>();
  const [notice, setNotice] = useState('');
  const [historyOpen, setHistoryOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const create = useCreateAction((value: Parameters<typeof kbApi.createTranslation>[1], key: string) => kbApi.createTranslation(articleId, value, key));
  const undelete = useCreateAction((updatedAt: string, key: string) => kbApi.restoreTranslation(articleId, locale, updatedAt, key));
  const backTo = useCreateAction(({ version, updatedAt }: { version: number; updatedAt: string }, key: string) =>
    kbApi.restoreTranslationVersion(articleId, locale, version, updatedAt, key));
  // Reset when the flag changes, not on every save: an autosave leaves the box as the person ticked it.
  useEffect(() => setUpToDate(false), [translation?.outdated]);
  const set = (change: Partial<Draft>) => setDraft((current) => ({ ...current, ...change }));
  // shown is the saved text the form edits; the translation's own fields stay the published text.
  const shown = translation && editableTranslation(translation);
  const publishChanges = useCreateAction((updatedAt: string, key: string) => kbApi.publishTranslationDraft(articleId, locale, updatedAt, key));
  const dirty = !shown || !translation || draft.title !== shown.title || draft.body_md !== shown.body_md || draft.slug !== shown.slug
    || draft.seo_title !== shown.seo_title || draft.seo_description !== shown.seo_description;
  // A newer version from the server (someone saved elsewhere, the tab read it again): taken as it is
  // while nothing is typed; otherwise the typed text stays and the page says so.
  // Only a newer version counts: a read that answers what was there before this page's own save never
  // takes its place.
  useEffect(() => {
    if (!latest || !translation || Date.parse(latest.updated_at) <= Date.parse(translation.updated_at)) return;
    if (dirty) {
      setStale(true);
      setTheirs(latest);
      return;
    }
    setTranslation(latest);
    setDraft(draftOf(latest));
  }, [latest, translation, dirty]);
  // translationNow is the newest answer at once, before the page renders it: an autosave or the list of files
  // may have answered since, and the next request sends the updated_at it gave.
  const translationNow = useRef(translation);
  translationNow.current = translation;
  // saved makes the server's answer the version being edited; show also puts its text in the form.
  const saved = (answer: KBTranslation) => {
    translationNow.current = answer;
    setTranslation(answer);
  };
  const changedElsewhere = () => {
    setStale(true);
    kbApi.translation(articleId, locale).then(setTheirs, () => undefined);
  };
  const autosave = useTranslationAutosave({ articleId, locale, title: draft.title, body: draft.body_md, timezone,
    current: () => translationNow.current, blocked: () => busy || stale || translationNow.current?.in_trash === true,
    onSaved: (answer) => {
      saved(answer);
      client.setQueryData(['kb-translation', articleId, locale], answer);
    },
    onConflict: changedElsewhere, onProblem: setProblem });
  // The translation's own files are saved at once, as the article's are: typed text stays in the form, and the
  // next save sends the updated_at this answer gave.
  const setAttachments = async (mediaIds: string[]): Promise<KBAttachment[]> => {
    await autosave.settle();
    const answer = await kbApi.setTranslationAttachments(articleId, locale, mediaIds, translationNow.current!.updated_at);
    saved(answer);
    client.setQueryData(['kb-translation', articleId, locale], answer);
    return editableTranslation(answer).attachments;
  };
  const show = (answer: KBTranslation) => {
    saved(answer);
    setDraft(draftOf(answer));
    autosave.saved();
  };
  const reload = async () => {
    await client.invalidateQueries({ queryKey: ['kb-translation', articleId, locale] });
    await Promise.all([client.invalidateQueries({ queryKey: ['kb-article', articleId] }), client.invalidateQueries({ queryKey: ['kb-languages'] }),
      client.invalidateQueries({ queryKey: ['kb-language'] })]);
  };
  const run = async (action: () => Promise<unknown>, done?: string) => {
    setBusy(true);
    setProblem(undefined);
    try {
      // An autosave in flight answers first, so the action sends the updated_at it gave.
      await autosave.settle();
      await action();
      if (done) setNotice(done);
      await reload();
    } catch (error) {
      if (error instanceof ChangedElsewhereError) changedElsewhere();
      else setProblem(reason(error, t.failed));
    } finally {
      setBusy(false);
    }
  };
  // against is the version the change is made over: the one being edited, or theirs to save mine over.
  const save = (status?: KBArticleStatus, over?: KBTranslation) => run(async () => {
    const against = over ?? translationNow.current;
    if (!against) {
      await create.run({ locale, title: draft.title, body_md: draft.body_md, ...(draft.slug.trim() ? { slug: draft.slug.trim() } : {}),
        ...(draft.seo_title ? { seo_title: draft.seo_title } : {}), ...(draft.seo_description ? { seo_description: draft.seo_description } : {}), ...(status ? { status } : {}) });
      return;
    }
    const text = editableTranslation(against);
    const change = {
      ...(draft.title !== text.title ? { title: draft.title } : {}),
      ...(draft.body_md !== text.body_md ? { body_md: draft.body_md } : {}),
      ...(draft.slug.trim() && draft.slug !== text.slug ? { slug: draft.slug.trim() } : {}),
      ...(draft.seo_title !== text.seo_title ? { seo_title: draft.seo_title } : {}),
      ...(draft.seo_description !== text.seo_description ? { seo_description: draft.seo_description } : {}),
      ...(status && status !== against.status ? { status } : {}),
      ...(upToDate && against.outdated ? { outdated: false } : {}),
    };
    // Text an autosave stored has no version yet: Save sends it again, and the server adds the version.
    if (Object.keys(change).length === 0 && autosave.pending) Object.assign(change, { body_md: draft.body_md });
    const answer = await kbApi.updateTranslation(articleId, locale, { updated_at: against.updated_at, ...change });
    saved(answer);
    autosave.saved(answer.updated_at);
    setStale(false);
    setTheirs(undefined);
  }, t.saved);
  const loadTheirs = () => run(async () => {
    show(await kbApi.translation(articleId, locale));
    setStale(false);
    setTheirs(undefined);
  });
  const keepMine = () => run(async () => {
    const theirs = await kbApi.translation(articleId, locale);
    await save(undefined, theirs);
  });
  // now is the newest answer, read after an autosave in flight answered (run waits for it).
  const now = () => translationNow.current!;
  const markOutdated = (outdated: boolean) => translation && run(async () => saved(await kbApi.updateTranslation(articleId, locale, { updated_at: now().updated_at, outdated })), t.saved);
  const remove = () => translation && run(async () => {
    await kbApi.deleteTranslation(articleId, locale, now().updated_at);
    setDeleting(false);
  });
  const restore = () => translation && run(async () => show(await undelete.run(now().updated_at)));
  // A published translation's unpublished changes go live, or are dropped, as an article's do.
  const publishDraft = () => translation && run(async () => show(await publishChanges.run(now().updated_at)), draftMessages.changesPublished);
  const discardDraft = async () => { if (translation) await run(async () => show(await kbApi.discardTranslationDraft(articleId, locale, now().updated_at)), draftMessages.changesDiscarded); };
  const restoreVersion = async (version: number) => {
    if (!translation) return;
    await autosave.settle();
    const answer = await backTo.run({ version, updatedAt: now().updated_at });
    show(answer);
    await reload();
  };
  const status = translation?.status ?? 'draft';
  const inTrash = translation?.in_trash === true;
  const invalid = busy || !draft.title.trim() || inTrash;
  const canSave = !invalid && (dirty || autosave.pending || (upToDate && translation?.outdated));
  // As an article's text: where one of its collections requires approval, Publish waits for a teammate's.
  const collections = useQuery({ queryKey: ['kb-collections'], queryFn: kbApi.collections }).data?.items ?? [];
  const approval = useTextApproval({ articleId, locale, collections, collectionIds: article?.collection_ids ?? [], dirty, version: translation?.updated_at });
  const blocked = approval.blocked;
  const reasonId = `${id}-approval`;
  return (
    <main className="kb-frame kb-translation-page" aria-labelledby={`${id}-heading`}>
      <p className="sr-only" role="status" aria-live="polite">{notice}</p>
      <header className="kb-art-bar">
        <nav className="kb-crumbs" aria-label={kbMessages.breadcrumb}>
          {/* kb-back: on a phone the crumbs fold to this way back to the article. */}
          <a className="kb-back" href={`/knowledge/articles/${articleId}`}><Icon name="chevronLeft" />{original?.title ?? t.back}</a>
          <span aria-hidden="true">/</span><span id={`${id}-heading`} aria-current="page">{t.translationOf(name)}</span>
        </nav>
        <span className="kb-art-state">
          <span className="kb-dot-status" data-status={status}>{status === 'published' ? kbMessages.published : kbMessages.draft}</span>
          {translation?.outdated && <span className="kb-outdated">· {t.outdated}</span>}
          {autosave.word && <span>· {autosave.word}</span>}
        </span>
        <span className="kb-art-acts">
          {/* As on an article: Publish and Save stay in the bar, the rest is in its menu, so the bar fits a phone. */}
          {listed && status !== 'published' && <button type="button" disabled={invalid || blocked !== undefined}
            aria-describedby={blocked ? reasonId : undefined} title={blocked} onClick={() => void save('published')}>{t.publish}</button>}
          <button type="button" className="primary-action" disabled={!canSave} onClick={() => void save()}>{busy ? t.saving : translation ? t.save : t.create}</button>
          {translation && !inTrash && <KBMenu label={t.moreActions} title={t.translationOf(name)} buttonClassName="kb-more-button" trigger={<Icon name="more" />} items={[
            { label: t.history, icon: 'history', onSelect: () => setHistoryOpen(true) },
            ...(status === 'published' ? [{ label: t.unpublish, icon: 'globe' as const, disabled: invalid, onSelect: () => void save('draft') }] : []),
            { label: t.delete, icon: 'trash', danger: true, separated: true, onSelect: () => setDeleting(true) },
          ]} />}
        </span>
      </header>
      <div className="kb-editor-notes">
        {stale && <ConflictCompare message={t.changedElsewhere} theirs={theirs && { title: editableTranslation(theirs).title, body: editableTranslation(theirs).body_md }}
          mine={{ title: draft.title, body: draft.body_md }} busy={busy} onKeepMine={() => void keepMine()} onLoadTheirs={() => void loadTheirs()} />}
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        {inTrash && <div className="warning-note"><p>{t.inTrashNote}</p><button type="button" disabled={busy} onClick={() => void restore()}>{t.restoreFromTrash}</button></div>}
        {translation?.draft && !inTrash && <ArticleDraftBar article={translation} title={draft.title} body={() => draft.body_md} busy={busy} dirty={dirty} blocked={blocked}
          onPublish={() => void publishDraft()} onDiscard={discardDraft} />}
        {listed && !inTrash && (status !== 'published' || dirty || translation?.has_draft) && <ArticleReviewBox articleId={articleId} locale={locale} approval={approval} dirty={dirty}
          saved={translation !== undefined} headingId={`${id}-review`} reasonId={reasonId} />}
        {!listed && <p className="warning-note">{t.languageNotListed}</p>}
        {listed && visible === false && <p className="muted-copy" data-quiet="state">{t.languageHidden} <a href="/knowledge/languages">{t.languagesTab}</a></p>}
        {original?.status === 'draft' && <p className="muted-copy">{t.articleDraft}</p>}
        {translation?.outdated && <div className="warning-note kb-outdated-note">
          <p>{t.originalChanged}</p>
          {/* Unchanged, one click marks it; while editing, it is marked with the save. */}
          {dirty
            ? <label><input type="checkbox" checked={upToDate} onChange={(event) => setUpToDate(event.target.checked)} /> {t.markUpToDate}</label>
            : <button type="button" disabled={busy} onClick={() => void markOutdated(false)}>{t.markUpToDate}</button>}
        </div>}
        {translation && !translation.outdated && !inTrash && <p className="kb-hint">{t.upToDate}{' '}
          <button type="button" className="link-action" disabled={busy} onClick={() => void markOutdated(true)}>{t.markOutdated}</button></p>}
      </div>
      <div className="kb-translation-split">
        <section className="kb-translation-original" aria-labelledby={`${id}-original`}>
          <h2 id={`${id}-original`}>{t.original(original?.locale ? languageName(original.locale) : kbMessages.heading)}</h2>
          <h3 className="kb-translation-original-title">{original?.title}</h3>
          <div className="kb-translation-scroll"><MarkdownPreview text={original?.body_md ?? ''} /></div>
        </section>
        <section className="kb-translation-own" aria-labelledby={`${id}-own`}>
          <h2 id={`${id}-own`}>{name}</h2>
          {!translation && original && <button type="button" className="link-action" onClick={() => set({ title: draft.title || original.title, body_md: original.body_md })}>{t.copyOriginal}</button>}
          <div className="field"><label htmlFor={`${id}-title`}>{t.title}</label>
            <input id={`${id}-title`} value={draft.title} required maxLength={300} disabled={inTrash} onChange={(event) => set({ title: event.target.value })} /></div>
          {/* Tabs as the article editor's: one panel, the selected tab in the tab order, arrows move between them. */}
          <div className="kb-tabs" role="tablist" aria-label={t.body}>
            {modes.map((each, at) => (
              <button key={each} type="button" role="tab" id={`${id}-${each}-tab`} aria-controls={`${id}-panel`} aria-selected={mode === each}
                tabIndex={mode === each ? 0 : -1} onClick={() => setMode(each)}
                onKeyDown={(event) => {
                  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
                  const next = modes[(at + (event.key === 'ArrowRight' ? 1 : modes.length - 1)) % modes.length];
                  setMode(next);
                  document.getElementById(`${id}-${next}-tab`)?.focus();
                }}>{each === 'write' ? t.write : t.preview}</button>
            ))}
          </div>
          <div role="tabpanel" id={`${id}-panel`} aria-labelledby={`${id}-${mode}-tab`}>
            {mode === 'write'
              ? <div className="field"><label htmlFor={`${id}-body`} className="sr-only">{t.body}</label>
                <textarea id={`${id}-body`} className="kb-translation-body" value={draft.body_md} rows={18} maxLength={100000} disabled={inTrash}
                  onChange={(event) => set({ body_md: event.target.value })} /></div>
              : <div className="kb-translation-scroll"><MarkdownPreview text={draft.body_md} /></div>}
          </div>
          <ArticleAttachments files={shown?.attachments ?? []} onSet={translation && !inTrash ? setAttachments : undefined}
            saveFirst={attachmentMessages.saveTranslationFirst} note={attachmentMessages.articleFiles} />
          <details className="kb-translation-more">
            <summary>{kbMessages.details}</summary>
            <div className="field"><label htmlFor={`${id}-slug`}>{t.slug}</label>
              <input id={`${id}-slug`} value={draft.slug} maxLength={120} aria-describedby={`${id}-slug-help`} disabled={inTrash} onChange={(event) => set({ slug: event.target.value })} />
              <small id={`${id}-slug-help`} className="kb-hint">{t.slugHelp}</small></div>
            <div className="field"><label htmlFor={`${id}-seo-title`}>{t.seoTitle}</label>
              <input id={`${id}-seo-title`} value={draft.seo_title} maxLength={120} disabled={inTrash} onChange={(event) => set({ seo_title: event.target.value })} /></div>
            <div className="field"><label htmlFor={`${id}-seo-description`}>{t.seoDescription}</label>
              <textarea id={`${id}-seo-description`} rows={3} value={draft.seo_description} maxLength={320} disabled={inTrash} onChange={(event) => set({ seo_description: event.target.value })} /></div>
          </details>
        </section>
      </div>
      {deleting && translation && <KBDialog title={t.deleteHeading(name)} role="alertdialog" onClose={() => setDeleting(false)} footer={<>
        <button type="button" disabled={busy} onClick={() => setDeleting(false)}>{kbMessages.cancel}</button>
        <button type="button" className="danger-solid" disabled={busy} onClick={() => void remove()}>{kbMessages.confirmDelete}</button>
      </>}><p className="muted-copy">{t.deleteBody} <HelpTip label={t.trashLabel} text={t.deleteTip} /></p></KBDialog>}
      {historyOpen && translation && <KBDialog title={t.history} className="kb-history" phone="full" closeLabel={t.closeHistory} onClose={() => setHistoryOpen(false)}>
        <SavedReplyHistory<KBTranslationVersion> key={`${translation.updated_at}`} title={translation.title} timezone={timezone}
          load={(before) => kbApi.translationVersions(articleId, locale, before)} text={(version) => version.body_md} failedMessage={t.failed}
          onRestore={restoreVersion} onClose={() => setHistoryOpen(false)} />
      </KBDialog>}
    </main>
  );
}
