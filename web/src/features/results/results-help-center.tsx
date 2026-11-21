import { Fragment, useState } from 'react';
import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { articleStats } from '../../api/kb-article-stats';
import type { HelpCenterWindow } from '../../api/results';
import { HelpTip } from '../../ui/help-tip';
import { helpCenterMessages as m } from './results-help-center-messages';

// The help center next to the inbox: article views, the conversations customers opened, and the
// self-service score per week. Each number opens what it counts: the views their days, the requests
// the inbox, an article's uses the replies that used it.

export interface HelpCenterLinks {
  requests: (range: { from: string; to: string }, children: ReactNode) => ReactNode;
  article: (id: string, children: ReactNode) => ReactNode;
  conversation: (id: string, children: ReactNode) => ReactNode;
}

type Article = HelpCenterWindow['articles'][number];
type Range = { from: string; to: string };

// Days are calendar days in the workspace's timezone (YYYY-MM-DD): written as they are, never shifted.
const formatDay = (value: string, weekday = false) => new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', timeZone: 'UTC', ...(weekday ? { weekday: 'short' } : {}) })
  .format(new Date(`${value}T00:00:00Z`));
const dayRange = (first: string, last: string) => `${formatDay(first)} – ${formatDay(last)}`;
const nextDay = (value: string) => new Date(Date.parse(`${value}T00:00:00Z`) + 86_400_000).toISOString().slice(0, 10);
const score = (value: number | null) => (value === null ? m.noRequests : value.toFixed(1));

function ArticleReplies({ article, range, links }: { article: Article; range: Range; links: HelpCenterLinks }) {
  const stats = useQuery({ queryKey: ['kb-article-stats', article.id, range.from, range.to], queryFn: () => articleStats(article.id, range) });
  if (stats.isError) return <p role="alert" className="error-copy">{m.repliesFailed}</p>;
  if (!stats.data) return <p className="results-muted" role="status">{m.loadingReplies}</p>;
  const { replies, replies_total: total } = stats.data;
  return <>
    <ul className="results-helpcenter-replies" aria-label={m.repliesOf(article.title)}>
      {replies.map((reply) => <li key={reply.message_id}>{links.conversation(reply.conversation_id, <>{reply.display_ref} {reply.subject}</>)}
        <span className="results-muted">{formatDay(reply.at.slice(0, 10))}</span></li>)}
    </ul>
    {total > replies.length && <p className="results-muted">{m.moreReplies(replies.length, total)}</p>}
  </>;
}

export function ResultsHelpCenter({ help, links }: { help: HelpCenterWindow; links: HelpCenterLinks }) {
  const [daysOpen, setDaysOpen] = useState(false);
  const [usesOf, setUsesOf] = useState<string>();
  const first = help.days[0]?.day;
  const last = help.days[help.days.length - 1]?.day;
  const range: Range | undefined = first && last ? { from: first, to: nextDay(last) } : undefined;
  return <section className="results-card results-helpcenter" aria-labelledby="results-helpcenter">
    <header>
      <div className="heading-with-tip"><h2 id="results-helpcenter">{m.heading}</h2><HelpTip label={m.heading} text={m.how} /></div>
      {first && last && <p className="results-muted">{dayRange(first, last)}</p>}
    </header>
    <div className="results-helpcenter-grid">
      <div><p className="results-cell-title">{m.views}</p>
        <button type="button" className="results-number results-number-button" aria-expanded={daysOpen} onClick={() => setDaysOpen(!daysOpen)}>{help.views}</button></div>
      <div><p className="results-cell-title">{m.requests}</p>
        {links.requests({ from: help.from, to: help.to }, <span className="results-number">{help.requests}</span>)}
        <p className="results-muted">{m.requestsDetail}</p></div>
      <div><p className="results-cell-title">{m.score}</p><p className="results-number">{score(help.score)}</p>
        {help.score !== null && <p className="results-muted">{m.scoreDetail(help.views, help.requests)}</p>}</div>
    </div>
    {daysOpen && <table className="results-helpcenter-table" aria-label={m.viewsPerDay}>
      <thead><tr><th scope="col">{m.day}</th><th scope="col">{m.views}</th></tr></thead>
      <tbody>{help.days.map((day) => <tr key={day.day}><td>{formatDay(day.day, true)}</td><td>{day.views}</td></tr>)}</tbody>
    </table>}
    {help.weeks.length > 0 && <table className="results-helpcenter-table" aria-label={m.perWeek}>
      <thead><tr><th scope="col">{m.week}</th><th scope="col">{m.views}</th><th scope="col">{m.requests}</th><th scope="col">{m.score}</th></tr></thead>
      <tbody>{help.weeks.map((week) => <tr key={week.first_day}><td>{dayRange(week.first_day, week.last_day)}</td><td>{week.views}</td><td>{week.requests}</td>
        <td>{score(week.score)}</td></tr>)}</tbody>
    </table>}
    {help.articles.length === 0 ? <p className="results-muted">{m.noArticles}</p> : <table className="results-helpcenter-table" aria-label={m.articles}>
      <thead><tr><th scope="col">{m.article}</th><th scope="col">{m.views}</th><th scope="col">{m.helpful}</th><th scope="col">{m.usedInReplies}</th></tr></thead>
      <tbody>{help.articles.map((article) => <Fragment key={article.id}>
        <tr><td>{links.article(article.id, article.title)}</td><td>{article.views}</td>
          <td>{article.helpful_percent === null ? m.noVotes : m.helpfulOf(article.helpful_percent, article.helpful, article.votes)}</td>
          <td>{article.uses > 0 && range ? <button type="button" className="link-button" aria-expanded={usesOf === article.id}
            onClick={() => setUsesOf(usesOf === article.id ? undefined : article.id)}>{article.uses}</button> : article.uses}</td></tr>
        {usesOf === article.id && range && <tr><td colSpan={4}><ArticleReplies article={article} range={range} links={links} /></td></tr>}
      </Fragment>)}</tbody>
    </table>}
    {help.articles_total > help.articles.length && <p className="results-muted">{m.more(help.articles.length, help.articles_total)}</p>}
  </section>;
}
