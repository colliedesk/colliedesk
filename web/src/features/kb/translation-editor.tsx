import { useEffect, useId, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ChangedElsewhereError } from '../../api/client';
import { useCreateAction } from '../../api/use-create';
import { SavedReplyHistory } from '../settings/saved-reply-panels';
import { kbApi } from './kb-api';
import type { KBArticle, KBArticleStatus, KBTranslation, KBTranslationVersion } from './kb-api';
import { kbMessages } from './kb-messages';
import { KBDialog, KBMenu } from './kb-ui';
import { Icon } from './editor/icons';
import { reason } from './kb-tree';
import { MarkdownPreview } from './markdown-preview';
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

const draftOf = (translation?: KBTranslation): Draft => ({
  title: translation?.title ?? '', body_md: translation?.body_md ?? '', slug: translation?.slug ?? '',
  seo_title: translation?.seo_title ?? '', seo_description: translation?.seo_description ?? '',
});

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
  const [notice, setNotice] = useState('');
  const [historyOpen, setHistoryOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const create = useCreateAction((value: Parameters<typeof kbApi.createTranslation>[1], key: string) => kbApi.createTranslation(articleId, value, key));
  const undelete = useCreateAction((updatedAt: string, key: string) => kbApi.restoreTranslation(articleId, locale, updatedAt, key));
  const backTo = useCreateAction(({ version, updatedAt }: { version: number; updatedAt: string }, key: string) =>
    kbApi.restoreTranslationVersion(articleId, locale, version, updatedAt, key));
  useEffect(() => setUpToDate(false), [translation?.updated_at]);
  const set = (change: Partial<Draft>) => setDraft((current) => ({ ...current, ...change }));
  const dirty = !translation || draft.title !== translation.title || draft.body_md !== translation.body_md || draft.slug !== translation.slug
    || draft.seo_title !== translation.seo_title || draft.seo_description !== translation.seo_description;
  // A newer version from the server (someone saved elsewhere, the tab read it again): taken as it is
  // while nothing is typed; otherwise the typed text stays and the page says so.
  // Only a newer version counts: a read that answers what was there before this page's own save never
  // takes its place.
  useEffect(() => {
    if (!latest || !translation || Date.parse(latest.updated_at) <= Date.parse(translation.updated_at)) return;
    if (dirty) {
      setStale(true);
      return;
    }
    setTranslation(latest);
    setDraft(draftOf(latest));
  }, [latest, translation, dirty]);
  // saved makes the server's answer the version being edited; show also puts its text in the form.
  const saved = (answer: KBTranslation) => setTranslation(answer);
  const show = (answer: KBTranslation) => {
    setTranslation(answer);
    setDraft(draftOf(answer));
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
      await action();
      if (done) setNotice(done);
      await reload();
    } catch (error) {
      if (error instanceof ChangedElsewhereError) setStale(true);
      else setProblem(reason(error, t.failed));
    } finally {
      setBusy(false);
    }
  };
  // against is the version the change is made over: the one being edited, or theirs to save mine over.
  const save = (status?: KBArticleStatus, against = translation) => run(async () => {
    if (!against) {
      await create.run({ locale, title: draft.title, body_md: draft.body_md, ...(draft.slug.trim() ? { slug: draft.slug.trim() } : {}),
        ...(draft.seo_title ? { seo_title: draft.seo_title } : {}), ...(draft.seo_description ? { seo_description: draft.seo_description } : {}), ...(status ? { status } : {}) });
      return;
    }
    saved(await kbApi.updateTranslation(articleId, locale, {
      updated_at: against.updated_at,
      ...(draft.title !== against.title ? { title: draft.title } : {}),
      ...(draft.body_md !== against.body_md ? { body_md: draft.body_md } : {}),
      ...(draft.slug.trim() && draft.slug !== against.slug ? { slug: draft.slug.trim() } : {}),
      ...(draft.seo_title !== against.seo_title ? { seo_title: draft.seo_title } : {}),
      ...(draft.seo_description !== against.seo_description ? { seo_description: draft.seo_description } : {}),
      ...(status && status !== against.status ? { status } : {}),
      ...(upToDate && against.outdated ? { outdated: false } : {}),
    }));
    setStale(false);
  }, t.saved);
  const loadTheirs = () => run(async () => {
    show(await kbApi.translation(articleId, locale));
    setStale(false);
  });
  const keepMine = () => run(async () => {
    const theirs = await kbApi.translation(articleId, locale);
    await save(undefined, theirs);
  });
  const markOutdated = (outdated: boolean) => translation && run(async () => saved(await kbApi.updateTranslation(articleId, locale, { updated_at: translation.updated_at, outdated })), t.saved);
  const remove = () => translation && run(async () => {
    await kbApi.deleteTranslation(articleId, locale, translation.updated_at);
    setDeleting(false);
  });
  const restore = () => translation && run(async () => show(await undelete.run(translation.updated_at)));
  const restoreVersion = (version: number) => translation
    ? backTo.run({ version, updatedAt: translation.updated_at }).then(async (answer) => { show(answer); await reload(); })
    : Promise.resolve();
  const status = translation?.status ?? 'draft';
  const inTrash = translation?.in_trash === true;
  const invalid = busy || !draft.title.trim() || inTrash;
  const canSave = !invalid && (dirty || (upToDate && translation?.outdated));
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
        </span>
        <span className="kb-art-acts">
          {/* As on an article: Publish and Save stay in the bar, the rest is in its menu, so the bar fits a phone. */}
          {listed && status !== 'published' && <button type="button" disabled={invalid} onClick={() => void save('published')}>{t.publish}</button>}
          <button type="button" className="primary-action" disabled={!canSave} onClick={() => void save()}>{busy ? t.saving : translation ? t.save : t.create}</button>
          {translation && !inTrash && <KBMenu label={t.moreActions} title={t.translationOf(name)} buttonClassName="kb-more-button" trigger={<Icon name="more" />} items={[
            { label: t.history, icon: 'history', onSelect: () => setHistoryOpen(true) },
            ...(status === 'published' ? [{ label: t.unpublish, icon: 'globe' as const, disabled: invalid, onSelect: () => void save('draft') }] : []),
            { label: t.delete, icon: 'trash', danger: true, separated: true, onSelect: () => setDeleting(true) },
          ]} />}
        </span>
      </header>
      <div className="kb-editor-notes">
        {stale && <div role="alert" className="warning-note kb-conflict">
          <p>{t.changedElsewhere}</p>
          <div className="button-row">
            <button type="button" disabled={busy} onClick={() => void keepMine()}>{kbMessages.keepMine}</button>
            <button type="button" disabled={busy} onClick={() => void loadTheirs()}>{kbMessages.loadTheirs}</button>
          </div>
          <small>{kbMessages.keepMineHelp}</small>
        </div>}
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        {inTrash && <div className="warning-note"><p>{t.inTrashNote}</p><button type="button" disabled={busy} onClick={() => void restore()}>{t.restoreFromTrash}</button></div>}
        {!listed && <p className="warning-note">{t.languageNotListed}</p>}
        {listed && visible === false && <p className="muted-copy">{t.languageHidden} <a href="/knowledge/languages">{t.languagesTab}</a></p>}
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
      </>}><p className="muted-copy">{t.deleteBody}</p></KBDialog>}
      {historyOpen && translation && <KBDialog title={t.history} className="kb-history" phone="full" closeLabel={t.closeHistory} onClose={() => setHistoryOpen(false)}>
        <SavedReplyHistory<KBTranslationVersion> key={`${translation.updated_at}`} title={translation.title} timezone={timezone}
          load={(before) => kbApi.translationVersions(articleId, locale, before)} text={(version) => version.body_md} failedMessage={t.failed}
          onRestore={restoreVersion} onClose={() => setHistoryOpen(false)} />
      </KBDialog>}
    </main>
  );
}
