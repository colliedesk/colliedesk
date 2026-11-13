import type { ReactNode } from 'react';
import type { ResultsSnapshot, ResultsWindow, WindowKey } from '../../api/results';
import { formatMinutes } from '../../lib/format-minutes';
import { resultsMessages as m } from './results-messages';
import { day, range } from './results-dates';
import { formatDuration, minSample, perWeek, timeChange, volumeChange, weeks } from './results-format';
import type { Change } from './results-format';

// NumberLink opens the conversations a number counts: `result` is `<snapshot id>:<window>.<metric>`.
export type NumberLink = (props: { result: string; className?: string; children: ReactNode }) => ReactNode;

export interface Side {
  label: string;
  window?: ResultsWindow;
  snapshot?: ResultsSnapshot;
  key: WindowKey;
  // blocked replaces every number of the side (a failed import).
  blocked?: string;
  badge?: string;
}

export interface Row {
  name: string;
  // how is the tip of the name: how the number is counted. note ends it with the days the number counts.
  how: string;
  note: string;
  before: ReactNode;
  after: ReactNode;
  change?: Change;
}

export interface RowsInput {
  before: Side;
  after: Side;
  timezone: string;
  link: NumberLink;
  sourceName: string;
  helpScout: boolean;
  // oldImport: the baseline import ran before Colliedesk read resolve times.
  oldImport: boolean;
  // available is the day the current side's resolve numbers appear, while its status history is too short.
  available?: string;
  withBaseline: boolean;
  comparable: boolean;
}

// A row is its name, a value and one short line under it. A number that cannot be shown yet says what it needs in
// that line; the way it is counted is in the tip beside the name.
export function resultsRows(input: RowsInput): Row[] {
  const { before, after, timezone, link, sourceName, helpScout, oldImport, available, withBaseline, comparable } = input;
  const line = (children: ReactNode) => <p className="results-muted">{children}</p>;
  const number = (side: Side, metric: string, value: ReactNode) => side.snapshot
    ? link({ result: `${side.snapshot.id}:${side.key}.${metric}`, className: 'results-number', children: value }) : <span className="results-number">{value}</span>;
  const count = (side: Side, metric: string, text: string) => side.snapshot
    ? link({ result: `${side.snapshot.id}:${side.key}.${metric}`, className: 'results-sublink', children: text }) : text;
  const blocked = (side: Side) => side.blocked ? line(side.blocked) : null;
  // noValue is the value of a number that cannot be worked out yet; a screen reader hears why the dash is there.
  const noValue = <p className="results-value"><span aria-hidden="true">–</span><span className="sr-only">{m.noValue}</span></p>;
  const needs = (side: Side, metric: string, what: string, have: number) => <>{noValue}{line(<>{m.needs(minSample, what)}{count(side, metric, m.soFar(have))}</>)}</>;
  const availableCell = () => available ? <>{noValue}{line(m.availableLine(day(available, timezone), day(after.window!.resolved_created.from, timezone)))}</> : null;

  const firstResponse = (side: Side) => {
    const value = side.window?.first_response;
    if (blocked(side)) return blocked(side);
    if (!value) return null;
    if (value.median_seconds === null) return needs(side, 'first_response', m.firstResponse.what, value.answered);
    return <>
      {number(side, 'first_response', formatDuration(value.median_seconds))}
      {line(<>{value.p90_seconds !== null && `${m.firstResponse.p90(formatDuration(value.p90_seconds))} · `}{count(side, 'first_response', m.firstResponse.answered(value.answered))} · {count(side, 'first_response_waiting', m.firstResponse.waiting(value.waiting))}</>)}
    </>;
  };
  const resolution = (side: Side) => {
    if (blocked(side)) return blocked(side);
    if (side === after && available) return availableCell();
    if (side === before && helpScout) return line(m.resolution.helpScout);
    if (side === before && oldImport) return line(m.resolution.oldImport);
    const value = side.window?.resolution;
    if (!value) return null;
    return <>
      {value.median_seconds === null ? needs(side, 'resolution', m.resolution.what, value.resolved) : <>
        {number(side, 'resolution', formatDuration(value.median_seconds))}
        {line(<>{count(side, 'resolution', m.resolution.resolved(value.resolved))} · {count(side, 'unresolved', m.resolution.unresolved(value.unresolved))}</>)}</>}
      {value.without_resolve_time > 0 && line(count(side, 'without_resolve_time', m.resolution.without(value.without_resolve_time, sourceName)))}
    </>;
  };
  const reopened = (side: Side) => {
    if (side === before) return blocked(side) ?? line(m.reopened.baseline(sourceName));
    if (available) return availableCell();
    const value = side.window?.reopened;
    if (!value) return null;
    if (value.rate === null) return needs(side, 'reopened', m.reopened.what, value.resolutions);
    return <>{number(side, 'reopened', `${Math.round(value.rate * 100)}%`)}{line(count(side, 'reopened', m.reopened.of(value.reopened, value.resolutions)))}</>;
  };
  const volume = (side: Side) => {
    if (blocked(side)) return blocked(side);
    const value = side.window?.volume;
    if (!value) return null;
    if (value.count === 0) return <><p className="results-value">0</p>{line(m.volume.none)}</>;
    if (value.per_week === null) return <>{number(side, 'volume', m.volume.count(value.count))}{line(m.volume.tooShort)}</>;
    // Under a week of conversations a rate only extrapolates them: the count comes first.
    if (value.days < 7) return <>{number(side, 'volume', m.volume.count(value.count))}{line(m.volume.young(Math.floor(value.days)))}</>;
    return <>{number(side, 'volume', m.volume.value(perWeek(value.per_week)))}{line(m.volume.detail(value.count, weeks(value.days)))}</>;
  };
  const handle = (side: Side) => {
    if (side === after) return line(m.handle.live);
    if (blocked(side)) return blocked(side);
    const value = side.window?.handle;
    if (!value) return null;
    if (value.median_minutes === null) return needs(side, 'handle', m.handle.what, value.conversations);
    return <>{number(side, 'handle', formatMinutes(value.median_minutes))}{line(count(side, 'handle', m.handle.logged(value.conversations)))}</>;
  };

  // note says which days a number counts: the resolve window opens 7 days before the one that heads the columns.
  const note = (resolved: boolean) => {
    const spans = [{ label: 'Colliedesk', window: after.window }, ...(withBaseline ? [{ label: sourceName, window: before.window }] : [])]
      .flatMap(({ label, window }) => window ? [{ label, span: resolved ? window.resolved_created : window.created }] : []);
    return spans.map(({ label, span }) => m.windowNote(spans.length > 1 ? label : null, range(span.from, span.to, timezone))).join(' ');
  };

  const b = before.window;
  const a = after.window;
  // A weekly rate is compared only when both sides saw a full week.
  const weekly = (w?: ResultsWindow) => (w?.volume.per_week != null && w.volume.days >= 7 ? w.volume.per_week : undefined);
  const bWeekly = weekly(b);
  const aWeekly = weekly(a);
  return [
    { name: m.firstResponse.name, how: m.firstResponse.how, note: '', before: firstResponse(before), after: firstResponse(after),
      change: comparable && b?.first_response.median_seconds != null && a?.first_response.median_seconds != null ? timeChange(b.first_response.median_seconds, a.first_response.median_seconds) : undefined },
    { name: m.resolution.name, how: m.resolution.how, note: note(true), before: resolution(before), after: resolution(after),
      change: comparable && !available && !oldImport && b?.resolution?.median_seconds != null && a?.resolution?.median_seconds != null ? timeChange(b.resolution.median_seconds, a.resolution.median_seconds) : undefined },
    { name: m.reopened.name, how: m.reopened.how, note: note(true), before: reopened(before), after: reopened(after) },
    { name: m.volume.name, how: m.volume.how, note: '', before: volume(before), after: volume(after),
      change: comparable && bWeekly !== undefined && aWeekly !== undefined ? volumeChange(bWeekly, aWeekly) : undefined },
    { name: m.handle.name, how: m.handle.how, note: note(true), before: handle(before), after: handle(after) },
  ];
}
