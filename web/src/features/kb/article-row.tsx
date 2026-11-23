import type { ReactNode } from 'react';
import type { KBListColumn } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { conflictsMessages } from './conflicts-messages';
import { savedReplyChangeText } from '../../lib/saved-reply-change';
import { Icon } from './editor/icons';
import type { KBArticleSummary } from './kb-api';
import { kbBulkMessages } from './kb-bulk-messages';
import { kbMessages } from './kb-messages';
import { listMessages } from './list-messages';
import { languageName } from './translation-messages';
import { VisibilityBadge } from './visibility-badge';

interface ArticleRowProps {
  article: KBArticleSummary;
  // columns are the cells after the title, in the order the person chose.
  columns: KBListColumn[];
  collection?: string;
  timezone: string;
  isPublic?: boolean;
  // order holds the buttons that move the article in its collection, while the list is the whole order;
  // they share the last track with the open button.
  order?: ReactNode;
  // select ticks the row for a bulk action; missing for a person who may not change articles.
  select?: { checked: boolean; onChange: () => void };
}

export function ArticleRow({ article, columns, collection, timezone, isPublic, order, select }: ArticleRowProps) {
  const published = article.status === 'published';
  // Readers can open it only while it is published, for everyone, and the help center is on.
  const everyone = article.effective_visibility === 'everyone';
  const open = published && everyone && isPublic === true;
  const openButton = open
    ? <a className="kb-article-open icon-button" href={article.url} target="_blank" rel="noreferrer" aria-label={kbMessages.openPublic(article.title)} data-tip={kbMessages.openOnHelpCenter}><Icon name="external" /></a>
    : <span className="kb-article-open" aria-hidden="true" title={published && everyone ? kbMessages.turnOnToOpen : undefined}><Icon name="external" /></span>;
  const meta = [columns.includes('collection') ? undefined : collection, kbMessages.articleNumber(article.number)];
  return (
    <li className="kb-article-item" data-selected={select?.checked ? 'true' : undefined}>
      {select && <input type="checkbox" className="kb-article-select" aria-label={kbBulkMessages.select(article.title)} checked={select.checked} onChange={select.onChange} />}
      <a className="kb-article-row" href={`/knowledge/articles/${article.id}`}>
        <span className="kb-article-main">
          <span className="kb-article-title">{article.title}</span>
          {article.open_conflicts > 0 && <small className="kb-outdated-mark">{conflictsMessages.open}</small>}
          <span className="kb-article-meta">{meta.filter(Boolean).join(' · ')}</span>
        </span>
        {columns.map((column, index) => <Cell key={column} column={column} article={article} collection={collection} timezone={timezone}
          isPublic={isPublic} extra={index > 0} />)}
      </a>
      {order ? <span className="kb-article-tail">{openButton}{order}</span> : openButton}
    </li>
  );
}

interface CellProps {
  column: KBListColumn;
  article: KBArticleSummary;
  collection?: string;
  timezone: string;
  isPublic?: boolean;
  // extra: a cell after the first, which a narrow list leaves out.
  extra: boolean;
}

function Cell({ column, article, collection, timezone, isPublic, extra }: CellProps) {
  const marker = extra ? { 'data-extra': 'true' } : {};
  if (column === 'status') {
    const published = article.status === 'published';
    return (
      <span className="kb-article-status" {...marker}>
        <span className="kb-dot-status" data-status={article.status}>{published ? kbMessages.published : kbMessages.draft}</span>
        {article.effective_visibility === 'everyone'
          ? <small>{published ? (isPublic === false ? kbMessages.notPublic : isPublic ? kbMessages.public : '') : kbMessages.agentsOnly}</small>
          : <VisibilityBadge visibility={article.effective_visibility} />}
        {article.has_draft && <small className="kb-changes-mark">{kbMessages.unpublishedChanges}</small>}
      </span>
    );
  }
  if (column === 'updated') {
    return (
      <span className="kb-article-when" {...marker}><time dateTime={article.updated_at}>{formatDate(article.updated_at, timezone)}</time>
        {article.last_change && <small>{savedReplyChangeText(article.last_change)}</small>}</span>
    );
  }
  return <span className="kb-article-cell" data-column={column} {...marker}>{cellText(column, article, collection, timezone)}</span>;
}

function cellText(column: Exclude<KBListColumn, 'status' | 'updated'>, article: KBArticleSummary, collection: string | undefined, timezone: string): ReactNode {
  switch (column) {
    case 'collection': return collection;
    case 'author': return article.author ? (article.author.left ? listMessages.left(article.author.name) : article.author.name) : null;
    case 'tags': return article.tags.join(', ');
    case 'languages': return article.languages.map((item) => (item.outdated ? listMessages.outdatedLanguage(languageName(item.locale)) : languageName(item.locale))).join(', ');
    case 'created': return <time dateTime={article.created_at}>{formatDate(article.created_at, timezone)}</time>;
    case 'votes': return <span title={listMessages.votesLabel(article.votes.helpful, article.votes.not_helpful)}>{listMessages.votes(article.votes.helpful, article.votes.not_helpful)}</span>;
  }
}
