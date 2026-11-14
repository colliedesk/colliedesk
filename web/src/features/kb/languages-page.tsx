import { useState } from 'react';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { messages } from '../../messages';
import { useHolds } from '../../lib/permissions-context';
import { kbApi } from './kb-api';
import type { HelpCenterLanguages, KBLanguageItem, KBLanguageState } from './kb-api';
import { kbMessages } from './kb-messages';
import { KBHead } from './kb-ui';
import { HelpTip } from '../../ui/help-tip';
import { reason } from './kb-tree';
import { languageName, translationMessages as t } from './translation-messages';

export interface LanguagesSearch {
  locale?: string;
  state?: KBLanguageState;
}

const localeShape = /^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$/;

// translationHref opens the translation of an article in one language: its editor, which also starts one.
export const translationHref = (articleId: string, locale: string) => `/knowledge/articles/${articleId}/translations/${encodeURIComponent(locale)}`;

// LanguagesRoute is the help center's languages: which are shown, how far each is translated, and the
// articles to translate next in one of them.
export function LanguagesRoute({ search }: { search: LanguagesSearch }) {
  const holds = useHolds();
  const client = useQueryClient();
  const navigate = useNavigate();
  const languages = useQuery({ queryKey: ['kb-languages'], queryFn: kbApi.languages });
  const [adding, setAdding] = useState('');
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  const canSet = holds('help_center:update');
  // save reports whether the server took the languages; a refusal is shown with its reason.
  const save = async (next: HelpCenterLanguages['languages']): Promise<boolean> => {
    setBusy(true);
    setProblem(undefined);
    try {
      const saved = await kbApi.saveLanguages({ languages: next.map(({ locale, visible }) => ({ locale, visible })) });
      client.setQueryData(['kb-languages'], saved);
      await client.invalidateQueries({ queryKey: ['kb-language'] });
      return true;
    } catch (error) {
      setProblem(reason(error, t.saveFailed));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const list = languages.data?.languages ?? [];
  const add = async () => {
    const locale = adding.trim();
    if (!localeShape.test(locale)) {
      setProblem(t.languageCodeHelp);
      return;
    }
    // A refused code stays in the box, to be corrected.
    if (await save([...list, { locale, visible: false, position: list.length + 1, published: 0, drafts: 0, outdated: 0, missing: 0 }])) setAdding('');
  };
  const move = (index: number, by: number) => {
    const next = [...list];
    const [item] = next.splice(index, 1);
    next.splice(index + by, 0, item);
    void save(next);
  };
  const choose = (locale: string, state: KBLanguageState) => void navigate({ to: '/knowledge/languages', search: { locale, state } });
  return (
    <main className="kb-frame kb-languages-page" aria-labelledby="kb-heading">
      <KBHead current="languages" heading={kbMessages.heading} />
      <div className="kb-languages-scroll">
        <section className="kb-languages" aria-labelledby="kb-languages-heading">
          <h2 id="kb-languages-heading">{t.languagesHeading}</h2>
          <p className="muted-copy">{t.languagesDescription} <HelpTip label={t.languagesHeading} text={t.languagesTip} /></p>
          {languages.isError && <p role="alert" className="error-copy">{t.loadFailed}{' '}
            <button type="button" className="link-action" onClick={() => void languages.refetch()}>{kbMessages.retry}</button></p>}
          {languages.isPending && <p role="status" className="muted-copy">{messages.loading}</p>}
          {languages.data && <>
            <div className="kb-language-primary">
              <strong>{t.primaryLanguage}</strong> <span>{languageName(languages.data.primary)}</span>
              <small className="kb-hint">{t.primaryHelp} <HelpTip label={t.primaryLanguage} text={t.primaryTip} /></small>
            </div>
            <h3>{t.otherLanguages}</h3>
            {problem && <p role="alert" className="error-copy">{problem}</p>}
            {list.length === 0 && <p className="empty-copy">{t.noLanguages}</p>}
            {list.length > 0 && <ul className="kb-language-list">
              {list.map((language, index) => {
                const name = languageName(language.locale);
                const inUse = language.published + language.drafts > 0;
                return (
                  <li key={language.locale} className="kb-language-row">
                    <span className="kb-language-name"><strong>{name}</strong>
                      <small>{[t.published(language.published), t.drafts(language.drafts)].join(' · ')}</small></span>
                    <span className="kb-language-counts">
                      <button type="button" className="link-action" aria-pressed={search.locale === language.locale && search.state === 'missing'}
                        onClick={() => choose(language.locale, 'missing')}>{t.missingCount(language.missing)}</button>
                      <button type="button" className="link-action" aria-pressed={search.locale === language.locale && search.state === 'outdated'}
                        data-warn={language.outdated > 0 ? 'true' : undefined} onClick={() => choose(language.locale, 'outdated')}>{t.outdatedCount(language.outdated)}</button>
                    </span>
                    <label className="kb-language-visible">
                      <input type="checkbox" checked={language.visible} disabled={!canSet || busy} aria-label={t.showLanguage(name)}
                        onChange={(event) => void save(list.map((each) => (each.locale === language.locale ? { ...each, visible: event.target.checked } : each)))} />
                      <span>{language.visible ? t.shown : t.hidden}</span>
                    </label>
                    {canSet && <span className="kb-language-actions">
                      <button type="button" className="icon-button" aria-label={t.moveUp(name)} disabled={busy || index === 0} onClick={() => move(index, -1)}>↑</button>
                      <button type="button" className="icon-button" aria-label={t.moveDown(name)} disabled={busy || index === list.length - 1} onClick={() => move(index, 1)}>↓</button>
                      <button type="button" className="danger-action" aria-label={t.removeLanguage(name)} disabled={busy || inUse} title={inUse ? t.removeInUse : undefined}
                        onClick={() => void save(list.filter((each) => each.locale !== language.locale))}>{t.remove}</button>
                    </span>}
                  </li>
                );
              })}
            </ul>}
            {canSet && <form className="kb-language-add" onSubmit={(event) => { event.preventDefault(); void add(); }}>
              <div className="field">
                <label htmlFor="kb-language-code">{t.languageCode}</label>
                <input id="kb-language-code" value={adding} maxLength={35} placeholder="fr" aria-describedby="kb-language-code-help" onChange={(event) => setAdding(event.target.value)} />
                <small id="kb-language-code-help" className="kb-hint">{t.languageCodeHelp}</small>
              </div>
              <button type="submit" disabled={busy || adding.trim() === ''}>{t.addLanguage}</button>
            </form>}
            {languages.data.unlisted.length > 0 && <div className="warning-note">
              <strong>{t.unlisted}</strong>: {languages.data.unlisted.map(languageName).join(', ')}
              <p className="kb-hint">{t.unlistedHelp} <HelpTip label={t.unlisted} text={t.unlistedTip} /></p>
            </div>}
          </>}
        </section>
        {search.locale && search.state && <LanguageArticles locale={search.locale} state={search.state} />}
      </div>
    </main>
  );
}

const stateLabel = (state: KBLanguageState) => (state === 'missing' ? t.missing : state === 'outdated' ? t.outdated : t.translated);
const emptyLabel = (state: KBLanguageState) => (state === 'missing' ? t.noneMissing : state === 'outdated' ? t.noneOutdated : t.noneTranslated);

// LanguageArticles lists the articles missing, outdated or translated in one language, each with the way to its translation.
function LanguageArticles({ locale, state }: { locale: string; state: KBLanguageState }) {
  const items = useInfiniteQuery({
    queryKey: ['kb-language', locale, state],
    queryFn: ({ pageParam }) => kbApi.languageArticles(locale, state, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  });
  const rows: KBLanguageItem[] = items.data?.pages.flatMap((page) => page.items) ?? [];
  const heading = t.articlesIn(stateLabel(state), languageName(locale));
  return (
    <section className="kb-language-articles" aria-labelledby="kb-language-articles-heading">
      <h2 id="kb-language-articles-heading">{heading}</h2>
      {items.isError && <p role="alert" className="error-copy">{kbMessages.listFailed}</p>}
      {items.isPending && <p role="status" className="muted-copy">{messages.loading}</p>}
      {items.isSuccess && rows.length === 0 && <p className="empty-copy">{emptyLabel(state)}</p>}
      {rows.length > 0 && <ul className="kb-article-list">
        {rows.map((row) => (
          <li key={row.article_id} className="kb-article-item">
            <a className="kb-article-row" href={translationHref(row.article_id, locale)}>
              <span className="kb-article-main">
                <span className="kb-article-title">{row.translation?.title ?? row.title}</span>
                <span className="kb-article-meta">{[row.translation ? row.title : '', row.collection_name, kbMessages.articleNumber(row.number)].filter(Boolean).join(' · ')}</span>
              </span>
              <span className="kb-article-status">
                {row.translation
                  ? <span className="kb-dot-status" data-status={row.translation.status}>{row.translation.status === 'published' ? kbMessages.published : kbMessages.draft}</span>
                  : <span className="kb-dot-status">{t.missing}</span>}
                {row.translation?.outdated && <small className="kb-outdated">{t.outdated}</small>}
              </span>
              <span className="button-link">{row.translation ? t.edit : t.translate}</span>
            </a>
          </li>
        ))}
        {items.hasNextPage && <li className="kb-more-row"><button type="button" className="kb-more" disabled={items.isFetchingNextPage} onClick={() => void items.fetchNextPage()}>
          {items.isFetchingNextPage ? messages.loading : kbMessages.loadMore}</button></li>}
      </ul>}
    </section>
  );
}
