import { useQuery } from '@tanstack/react-query';
import { useHolds } from '../../lib/permissions-context';
import { kbApi } from './kb-api';
import type { KBArticle } from './kb-api';
import { kbMessages } from './kb-messages';
import { translationHref } from './languages-page';
import { languageName, translationMessages as t } from './translation-messages';

// ArticleTranslationsBox lists the help center's languages with this article's translation in each:
// its status, whether it is behind the article, and the way to it (or to start it).
export function ArticleTranslationsBox({ article, headingId }: { article: KBArticle; headingId: string }) {
  const holds = useHolds();
  const languages = useQuery({ queryKey: ['kb-languages'], queryFn: kbApi.languages, enabled: holds('help_center:read') });
  const listed = languages.data?.languages ?? [];
  const byLocale = new Map(article.translations.map((translation) => [translation.locale, translation]));
  // A translation in a language the help center no longer lists still shows, so it can be edited or deleted.
  const locales = [...listed.map((language) => language.locale), ...article.translations.map((translation) => translation.locale).filter((locale) => !listed.some((language) => language.locale === locale))];
  return (
    <section className="kb-side-box kb-translations-box" aria-labelledby={headingId}>
      <h3 id={headingId}>{t.translations}</h3>
      {locales.length === 0
        ? <p className="kb-hint">{t.noOtherLanguages} {holds('help_center:read') && <a href="/knowledge/languages">{t.manageLanguages}</a>}</p>
        : <ul className="kb-translation-list">
          {locales.map((locale) => {
            const translation = byLocale.get(locale);
            return (
              <li key={locale}>
                <a href={translationHref(article.id, locale)}>{languageName(locale)}</a>
                <span className="kb-translation-state">
                  {!translation ? <span className="kb-hint">{t.missing}</span>
                    : translation.in_trash ? <span className="kb-hint">{t.inTrash}</span>
                      : <span className="kb-dot-status" data-status={translation.status}>{translation.status === 'published' ? kbMessages.published : kbMessages.draft}</span>}
                  {translation?.outdated && !translation.in_trash && <span className="kb-outdated">{t.outdated}</span>}
                </span>
              </li>
            );
          })}
        </ul>}
    </section>
  );
}
