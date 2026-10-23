import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { Tag, TeamMember } from '../../api/schemas';
import { InvalidRequestError, PlanLimitError } from '../../api/client';
import { formatDate, formatShortDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { CheckIcon } from '../../ui/icons';
import { aiTriageApi } from './ai-triage-api';
import type { AITriage, TriagePart, TriagePick } from './ai-triage-api';
import { isSettingsFailure, refusalCode, SettingsWay } from './ai-failure';
import { AISources } from './ai-sources';
import './ai-triage.css';

// Strings of the suggestion card; it is a lazy part, so they live in its chunk.
const text = {
  heading: 'Suggested by AI',
  headingShort: 'Suggested',
  headingApplied: 'Applied by AI',
  headingUndone: 'Undone',
  addedLower: 'added',
  setLower: 'set',
  undo: 'Undo',
  undoPart: (part: string, value: string) => `Undo ${part} ${value}`,
  undoAll: 'Undo all',
  applyAgain: 'Apply again',
  onlyFilled: 'Only filled what nobody set.',
  fillsEmpty: 'Fills only what is still empty.',
  undoneMark: 'Undone',
  removed: (names: string) => `${names} removed`,
  takenBack: (value: string) => `${value} taken back`,
  unassigned: (name: string) => `${name} unassigned`,
  kept: (value: string) => `Kept ${value}: a person changed it after AI.`,
  undoneBy: (who: string, at: string) => `by ${who}, ${at}`,
  appliedLine: (summary: string) => summary,
  skippedWhy: {
    set_by_person: (value: string) => `Not set: a person had set it to ${value}.`,
    agent_conversation: () => 'Not set: a teammate started this ticket.',
    already_assigned: (value: string) => `Not set: ${value} was already assigned.`,
  } as Record<'set_by_person' | 'agent_conversation' | 'already_assigned', (value: string) => string>,
  appliedPart: 'Applied. The rest of the suggestion is still open.',
  writing: 'Suggesting tags, priority and assignee…',
  keepWorking: 'Keep working; it appears here when ready.',
  cancel: 'Cancel',
  tags: 'Tags',
  priority: 'Priority',
  assignee: 'Assignee',
  accept: 'Accept',
  acceptTags: (names: string) => `Accept tags ${names}`,
  acceptPriority: (value: string) => `Accept priority ${value}`,
  acceptAssignee: (name: string) => `Accept assignee ${name}`,
  added: 'Added',
  set: 'Set',
  assigned: 'Assigned',
  acceptAll: 'Accept all',
  acceptRest: 'Accept the rest',
  dismiss: 'Dismiss',
  suggest: 'Suggest tags and priority',
  suggestNow: 'Suggest now',
  again: 'Suggest again',
  tryAgain: 'Try again',
  nothing: 'Nothing to change: the tags, priority and assignee already fit.',
  stalePriority: (value: string) => `Not suggested: a person set ${value} before this arrived.`,
  staleAssignee: (name: string) => `Not suggested: a person assigned ${name} before this arrived.`,
  undoneNote: 'Undone.',
  appliedAgainBy: (who: string) => `applied again by ${who}`,
  acceptedBy: (who: string, at: string) => `Accepted by ${who}, ${at}. Change a field below to undo.`,
  you: 'you',
  someone: 'a teammate',
  partly: 'Part of it was applied; the rest was dismissed.',
  dismissed: 'Dismissed.',
  expired: 'Nobody used it for 7 days, so it expired.',
  superseded: 'Replaced by a newer suggestion.',
  stopped: 'Stopped.',
  loadFailed: 'The suggestion could not be loaded. Try again in a moment.',
  refusedFallback: 'Nothing was changed: the request did not reach Colliedesk. Check your connection, then try again.',
  generated: 'Written by AI: check it. Nothing changes until you accept.',
  paused: (until: string) => `Automatic suggestions are paused until ${until}: the daily limit is reached. You can still ask for one.`,
  failed: (code: string | null) => failures[code ?? ''] ?? 'No suggestion this time: the AI provider did not answer. Try again in a moment.',
};

const failures: Record<string, string> = {
  ai_allowance_reached: 'No suggestion: this month’s included AI is used up. An admin can add your own provider in Settings → AI.',
  feature_off: 'No suggestion: triage suggestions were turned off. An admin turns them on in Settings → AI.',
  auth: 'No suggestion: the AI provider refused its key. An admin can change it in Settings → AI.',
  quota_exhausted: 'No suggestion: the AI provider account is out of credit. An admin can check it in Settings → AI.',
  refused: 'No suggestion: the model declined this ticket. Sort it by hand, or try again.',
  too_long: 'No suggestion: the model’s answer ran too long. Try again; fewer tags in the workspace make it shorter.',
  empty_answer: 'No suggestion: the model returned nothing. Try again.',
  internal: 'No suggestion: Colliedesk could not save it. Try again.',
  invalid_answer: 'No suggestion: the model’s answer could not be read. Try again.',
  restored: 'This suggestion was lost when the workspace was restored. Ask for a new one.',
  workspace_locked: 'No suggestion: the workspace is locked.',
};

interface AITriageCardProps {
  conversationId: string;
  tags: Tag[];
  agents: TeamMember[];
  // The signed-in agent, so an accepted suggestion says "you"; and the timezone its time is shown in.
  currentAgentId?: string;
  timezone?: string;
  // An admin is offered Settings → AI when a suggestion fails for a setting; others are told to ask one.
  canManage?: boolean;
  // Called after a part was applied, so the conversation is read again.
  onApplied: () => void;
  // Told whether a part waits for someone to accept it; false when the card goes.
  onWaiting?: (waiting: boolean) => void;
  // details: the card in the details panel. timeline: on the conversation's timeline, a line for what AI
  // applied on wide screens, and the whole suggestion on narrow ones, where the details hide behind a toggle.
  placement?: 'details' | 'timeline';
}

const writing = (suggestion: AITriage | null | undefined) => suggestion?.status === 'queued' || suggestion?.status === 'running';

// The parts of a suggestion nobody applied yet.
function openParts(suggestion: AITriage) {
  const { applied } = suggestion;
  return {
    tags: suggestion.tag_ids.filter((id) => !applied.tag_ids.includes(id)),
    priority: Boolean(suggestion.priority && !applied.priority),
    assignee: Boolean(suggestion.assignee_agent_id && !applied.assignee_agent_id),
  };
}

function nextUTCMidnight(at: string) {
  const day = new Date(at);
  return new Date(Date.UTC(day.getUTCFullYear(), day.getUTCMonth(), day.getUTCDate() + 1)).toISOString();
}

const emptySet = (set: AITriage['applied']) => set.tag_ids.length === 0 && !set.priority && !set.assignee_agent_id;
// What AI applied by itself is still on it; undone: what it applied was taken back.
const appliedByAI = (suggestion: AITriage) => suggestion.auto_applied_at !== null && !emptySet(suggestion.auto_applied);
const undoneByAI = (suggestion: AITriage) => suggestion.outcome === 'undone';

function hasOpenPart(suggestion: AITriage) {
  const parts = openParts(suggestion);
  return parts.tags.length > 0 || parts.priority || parts.assignee;
}

// AITriageCard shows the conversation's newest AI triage suggestion: tags, priority and assignee, each
// with Accept, then Accept all or Dismiss, its reason and its sources. A new ticket gets one by itself;
// a person can ask again. Nothing changes on the conversation until someone accepts.
export function AITriageCard({ conversationId, tags, agents, currentAgentId, timezone = 'UTC', canManage = false, onApplied, onWaiting, placement = 'details' }: AITriageCardProps) {
  const client = useQueryClient();
  const key = ['ai-triage', conversationId];
  // While it is written the card reads it again every 3 s; a failed read stops that (Try again is the query's own).
  const latest = useQuery({ queryKey: key, queryFn: () => aiTriageApi.latest(conversationId),
    refetchInterval: (query) => (query.state.status !== 'error' && writing(query.state.data?.suggestion) ? 3000 : false) });
  const suggestion = latest.data?.suggestion ?? null;
  const waiting = placement === 'details' && suggestion?.status === 'ready' && suggestion.outcome === null && hasOpenPart(suggestion);
  useEffect(() => {
    onWaiting?.(waiting);
  }, [onWaiting, waiting]);
  useEffect(() => () => onWaiting?.(false), [onWaiting]);
  // When AI applies a suggestion by itself while the conversation is open, the conversation is read again.
  const wasWriting = useRef(false);
  const justApplied = placement === 'details' && suggestion?.status === 'ready' && appliedByAI(suggestion);
  useEffect(() => {
    if (justApplied && wasWriting.current) onApplied();
    wasWriting.current = writing(suggestion);
  }, [justApplied, suggestion, onApplied]);
  // The answer replaces the suggestion it is, which may be the newest one, the one AI applied, or both.
  const set = (next: AITriage) => client.setQueryData<{ suggestion: AITriage | null; auto_applied: AITriage | null }>(key, (current) => ({
    suggestion: !current?.suggestion || current.suggestion.id === next.id ? next : current.suggestion,
    auto_applied: current?.auto_applied?.id === next.id ? next : current?.auto_applied ?? null,
  }));
  // After an action its button goes: focus moves to the card's heading, and a status says what happened (the
  // card's own closing line, or this one when a part was applied and the rest is still open).
  const heading = useRef<HTMLHeadingElement>(null);
  const [announcement, setAnnouncement] = useState('');
  const settled = (next: AITriage, said: string) => {
    set(next);
    setAnnouncement(said);
    heading.current?.focus();
  };
  const ask = useMutation({ mutationFn: () => aiTriageApi.create(conversationId),
    onSuccess: (next) => client.setQueryData<{ suggestion: AITriage | null; auto_applied: AITriage | null }>(key, (current) => ({ suggestion: next, auto_applied: current?.auto_applied ?? null })) });
  const accept = useMutation({ mutationFn: ({ target, pick }: { target: AITriage; pick?: TriagePick }) => aiTriageApi.accept(conversationId, target.id, pick),
    onSuccess: (next) => { settled(next, next.outcome === null ? text.appliedPart : ''); onApplied(); } });
  const dismiss = useMutation({ mutationFn: (target: AITriage) => aiTriageApi.dismiss(conversationId, target.id),
    onSuccess: (next) => settled(next, '') });
  const undo = useMutation({ mutationFn: ({ target, parts }: { target: AITriage; parts?: TriagePart[] }) => aiTriageApi.undo(conversationId, target.id, parts),
    onSuccess: (next) => { settled(next, undoneByAI(next) ? '' : text.undoneNote); onApplied(); } });
  const refused = [ask, accept, dismiss, undo].find((mutation) => mutation.isError)?.error;
  const busy = ask.isPending || accept.isPending || dismiss.isPending || undo.isPending;
  const name = (id: string | null) => {
    const agent = agents.find((item) => item.id === id);
    return id === currentAgentId ? text.you : agent ? agent.display_name || agent.email : text.someone;
  };
  const askButton = (label: string) => <button type="button" disabled={busy} onClick={() => ask.mutate()}>{label}</button>;

  let body: ReactNode = null;
  let actions: ReactNode = null;
  if (latest.isError) {
    body = <p role="alert" className="error-copy">{text.loadFailed}</p>;
    actions = <button type="button" onClick={() => void latest.refetch()}>{text.tryAgain}</button>;
  } else if (latest.isPending) {
    body = null;
  } else if (!suggestion) {
    actions = askButton(text.suggest);
  } else if (writing(suggestion)) {
    body = (
      <div role="status" className="ai-triage-writing">
        <span><span className="ai-triage-pulse" aria-hidden="true" />{text.writing}</span>
        <span className="muted-copy">{text.keepWorking}</span>
      </div>
    );
    actions = <button type="button" className="ghost" disabled={busy} onClick={() => dismiss.mutate(suggestion)}>{text.cancel}</button>;
  } else if (suggestion.status === 'failed') {
    // The daily limit counts UTC days: paused until the next midnight UTC, shown in the agent's timezone.
    const message = suggestion.error_code === 'automatic_limit' ? text.paused(formatDate(nextUTCMidnight(suggestion.created_at), timezone)) : text.failed(suggestion.error_code);
    const paused = suggestion.error_code === 'automatic_limit';
    body = <p className={paused ? 'ai-triage-message' : 'ai-triage-message error-copy'}>{message}</p>;
    // A setting causes it: asking again fails the same way, so the way to the setting is the action.
    actions = isSettingsFailure(suggestion.error_code) ? <SettingsWay code={suggestion.error_code} canManage={canManage} />
      : askButton(paused ? text.suggestNow : text.tryAgain);
  } else if (suggestion.status === 'cancelled') {
    body = <p className="ai-triage-message" role="status">{text.stopped}</p>;
    actions = askButton(text.again);
  } else {
    const open = suggestion.outcome === null;
    const anything = suggestion.tag_ids.length > 0 || suggestion.priority || suggestion.assignee_agent_id;
    const stale = suggestion.stale.priority || suggestion.stale.assignee_agent_id;
    if (!anything && !stale) {
      body = <p className="ai-triage-message">{text.nothing}</p>;
      actions = askButton(text.again);
    } else if (undoneByAI(suggestion)) {
      body = (
        <>
          <UndoneRows suggestion={suggestion} tags={tags} agents={agents} />
          <p className="muted-copy ai-triage-note">{text.undoneBy(name(suggestion.undone_by_agent_id), formatShortDate(suggestion.undone_at ?? suggestion.created_at, timezone))}</p>
          {suggestion.reason && <p className="ai-triage-reason">{suggestion.reason}</p>}
          <AISources sources={suggestion.sources} />
        </>
      );
      actions = <>{hasOpenPart(suggestion) && <button type="button" disabled={busy} onClick={() => accept.mutate({ target: suggestion })}>{text.applyAgain}</button>}
        <span className="ai-triage-help">{text.fillsEmpty}</span></>;
    } else {
      const byAI = appliedByAI(suggestion);
      body = (
        <>
          <TriageRows suggestion={suggestion} tags={tags} agents={agents} open={open || byAI} busy={busy} onAccept={(pick) => accept.mutate({ target: suggestion, pick })}
            onUndo={(part) => undo.mutate({ target: suggestion, parts: [part] })} />
          {suggestion.reason && <p className="ai-triage-reason">{suggestion.reason}</p>}
          <AISources sources={suggestion.sources} />
          {open && !byAI && <p className="muted-copy ai-triage-note">{text.generated}</p>}
        </>
      );
      if (byAI) {
        actions = <>
          {hasOpenPart(suggestion) && <button type="button" className="primary-action" disabled={busy} onClick={() => accept.mutate({ target: suggestion })}>{text.acceptRest}</button>}
          <button type="button" disabled={busy} onClick={() => undo.mutate({ target: suggestion })}>{text.undoAll}</button>
          <span className="ai-triage-help">{text.onlyFilled}</span>
        </>;
      } else if (open) {
        const some = suggestion.applied.tag_ids.length > 0 || suggestion.applied.priority || suggestion.applied.assignee_agent_id;
        actions = (
          <>
            {hasOpenPart(suggestion) && <button type="button" className="primary-action" disabled={busy} onClick={() => accept.mutate({ target: suggestion })}>
              {some || stale ? text.acceptRest : text.acceptAll}</button>}
            <button type="button" className="ghost" disabled={busy} onClick={() => dismiss.mutate(suggestion)}>{text.dismiss}</button>
          </>
        );
      } else {
        const who = suggestion.decided_by_agent_id === currentAgentId ? text.you
          : agents.find((agent) => agent.id === suggestion.decided_by_agent_id)?.display_name || text.someone;
        const after = suggestion.outcome === 'accepted' && suggestion.decided_at ? text.acceptedBy(who, formatDate(suggestion.decided_at, timezone))
          : suggestion.outcome === 'partly_accepted' ? text.partly
          : suggestion.outcome === 'expired' ? text.expired
          : suggestion.outcome === 'superseded' ? text.superseded : text.dismissed;
        body = <>{body}<p className="muted-copy" role="status">{after}</p></>;
        actions = suggestion.outcome === 'accepted' ? null : askButton(text.again);
      }
    }
  }
  const headingText = suggestion?.status === 'ready' && undoneByAI(suggestion) ? text.headingUndone
    : suggestion?.status === 'ready' && appliedByAI(suggestion) ? text.headingApplied : text.headingShort;
  const card = (
    <section className={placement === 'details' ? 'details-section ai-triage' : 'ai-triage ai-triage-timeline'} aria-label={text.heading}
      aria-busy={latest.isPending || writing(suggestion)}>
      <h2 ref={heading} tabIndex={-1}><span className="ai-chip" aria-hidden="true">AI</span>{headingText}
        {suggestion && <time className="ai-triage-time" dateTime={suggestion.ready_at ?? suggestion.created_at}>{formatShortDate(suggestion.ready_at ?? suggestion.created_at, timezone)}</time>}</h2>
      {body}
      <p className="sr-only" role="status">{announcement}</p>
      {/* A refusal Colliedesk explains is shown as it is; anything else (no connection) changed nothing. */}
      {refused && <p role="alert" className="error-copy">{(refused instanceof InvalidRequestError || refused instanceof PlanLimitError) && refused.message ? refused.message : text.refusedFallback}
        {' '}<SettingsWay code={refusalCode(refused)} canManage={canManage} /></p>}
      {actions && <div className="ai-triage-actions">{actions}</div>}
    </section>
  );
  if (placement === 'details') return card;
  // On the timeline: the whole suggestion only while there is one to act on, and a line for what AI
  // applied by itself (which a newer suggestion asked for by hand does not hide).
  const auto = latest.data?.auto_applied ?? null;
  const named = suggestion !== null && (suggestion.tag_ids.length > 0 || suggestion.priority !== null || suggestion.assignee_agent_id !== null);
  const showCard = suggestion !== null && (writing(suggestion) || (suggestion.status === 'ready' && (named || undoneByAI(suggestion))
    && !['dismissed', 'superseded', 'expired'].includes(suggestion.outcome ?? '')));
  return <>
    {auto && (appliedByAI(auto) || undoneByAI(auto)) && <AppliedLine suggestion={auto} tags={tags} agents={agents} timezone={timezone} busy={busy} name={name}
      onUndo={() => undo.mutate({ target: auto })} onApplyAgain={() => accept.mutate({ target: auto })} />}
    {showCard && card}
  </>;
}

function summary(set: AITriage['applied'], tags: Tag[], agents: TeamMember[]) {
  const parts: string[] = [];
  const tagNames = set.tag_ids.map((id) => tags.find((tag) => tag.id === id)?.name).filter(Boolean);
  if (tagNames.length > 0) parts.push(`${tagNames.length === 1 ? 'tag' : 'tags'} ${tagNames.join(', ')}`);
  if (set.priority) parts.push(`priority ${messages.priorities[set.priority as keyof typeof messages.priorities] ?? set.priority}`);
  if (set.assignee_agent_id) {
    const agent = agents.find((item) => item.id === set.assignee_agent_id);
    parts.push(`assigned to ${agent ? agent.display_name || agent.email : 'a teammate'}`);
  }
  return parts.join(' · ');
}

// AppliedLine is what AI applied by itself, on the timeline at the time it did: one line with Undo, or,
// after an undo, who undid it with Apply again. The details card has each part.
function AppliedLine({ suggestion, tags, agents, timezone, busy, name, onUndo, onApplyAgain }: { suggestion: AITriage; tags: Tag[]; agents: TeamMember[]; timezone: string;
  busy: boolean; name: (id: string | null) => string; onUndo: () => void; onApplyAgain: () => void }) {
  const undone = undoneByAI(suggestion);
  return (
    <div role="group" aria-label={undone ? text.headingUndone : text.headingApplied} className="ai-triage-line">
      <span className="ai-chip" aria-hidden="true">AI</span><b>{undone ? text.headingUndone : text.headingApplied}</b>
      {/* Undone says who undid it and when; applied says what, when, and who applied it again after an undo. */}
      <span className="ai-triage-line-sum">{undone ? text.undoneBy(name(suggestion.undone_by_agent_id), formatShortDate(suggestion.undone_at ?? '', timezone))
        : suggestion.undone_at ? `${summary(suggestion.auto_applied, tags, agents)} · ${text.appliedAgainBy(name(suggestion.decided_by_agent_id))}`
          : summary(suggestion.auto_applied, tags, agents)}</span>
      {!undone && <time dateTime={suggestion.auto_applied_at ?? undefined}>{formatShortDate(suggestion.auto_applied_at ?? suggestion.created_at, timezone)}</time>}
      {undone ? <button type="button" disabled={busy} onClick={onApplyAgain}>{text.applyAgain}</button>
        : <button type="button" disabled={busy} aria-label={`${text.undo} ${summary(suggestion.auto_applied, tags, agents)}`} onClick={onUndo}>{text.undo}</button>}
    </div>
  );
}

// UndoneRows says what an undo took back, and what it kept because a person changed it after AI.
function UndoneRows({ suggestion, tags, agents }: { suggestion: AITriage; tags: Tag[]; agents: TeamMember[] }) {
  const { undone_parts: undone, kept } = suggestion;
  const tagNames = undone.tag_ids.map((id) => tags.find((tag) => tag.id === id)?.name).filter((name): name is string => Boolean(name));
  const agentName = (id: string | null) => agents.find((item) => item.id === id)?.display_name || '';
  const mark = <span className="ai-triage-done"><CheckIcon />{text.undoneMark}</span>;
  const row = (label: string, value: string, done: boolean) => (
    <div role="group" aria-label={label} className={done ? 'ai-triage-row' : 'ai-triage-row stale'}>
      <div><div className="ai-triage-key">{label}</div><div className="ai-triage-value">{value}</div></div>{done && mark}
    </div>
  );
  return (
    <div className="ai-triage-rows">
      {tagNames.length > 0 && row(text.tags, text.removed(tagNames.join(', ')), true)}
      {undone.priority && row(text.priority, text.takenBack(messages.priorities[undone.priority as keyof typeof messages.priorities] ?? undone.priority), true)}
      {kept.priority && row(text.priority, text.kept(messages.priorities[kept.priority]), false)}
      {undone.assignee_agent_id && row(text.assignee, text.unassigned(agentName(undone.assignee_agent_id)), true)}
      {kept.assignee_agent_id && row(text.assignee, text.kept(agentName(kept.assignee_agent_id)), false)}
    </div>
  );
}

interface TriageRowsProps {
  suggestion: AITriage;
  tags: Tag[];
  agents: TeamMember[];
  open: boolean;
  busy: boolean;
  onAccept: (pick: TriagePick) => void;
  onUndo: (part: TriagePart) => void;
}

// TriageRows is a row for each part: what it suggests, and Accept, or what was done with it.
function TriageRows({ suggestion, tags, agents, open, busy, onAccept, onUndo }: TriageRowsProps) {
  const parts = openParts(suggestion);
  const auto = suggestion.auto_applied;
  const agentName = (id: string | null) => {
    const agent = agents.find((item) => item.id === id);
    return agent ? agent.display_name || agent.email : '';
  };
  const tagName = (id: string) => tags.find((tag) => tag.id === id)?.name;
  const tagNames = suggestion.tag_ids.map(tagName).filter((name): name is string => Boolean(name));
  const done = (label: string) => <span className="ai-triage-done"><CheckIcon />{label}</span>;
  const row = (label: string, value: ReactNode, action: ReactNode, stale = false, key = label) => (
    <div role="group" aria-label={label} className={stale ? 'ai-triage-row stale' : 'ai-triage-row'}>
      <div><div className="ai-triage-key">{key}</div><div className="ai-triage-value">{value}</div></div>
      {action}
    </div>
  );
  const button = (name: string, pick: TriagePick) => open && <button type="button" aria-label={name} disabled={busy} onClick={() => onAccept(pick)}>{text.accept}</button>;
  // A part AI applied by itself says so and has its own Undo.
  const undoButton = (part: TriagePart, value: string) => <button type="button" aria-label={text.undoPart(part, value)} disabled={busy} onClick={() => onUndo(part)}>{text.undo}</button>;
  const skipped = (part: 'priority' | 'assignee') => {
    const skip = suggestion.skipped[part];
    if (!skip) return null;
    const shown = part === 'priority' ? messages.priorities[(skip.value ?? 'normal') as keyof typeof messages.priorities] ?? skip.value ?? '' : agentName(skip.value);
    const label = part === 'priority' ? text.priority : text.assignee;
    return row(label, text.skippedWhy[skip.reason](shown), null, true);
  };
  const autoTags = auto.tag_ids.map(tagName).filter((name): name is string => Boolean(name));
  const personTags = tagNames.filter((name) => !autoTags.includes(name));
  return (
    <div className="ai-triage-rows">
      {autoTags.length > 0 && row(text.tags, autoTags.map((name) => <span key={name} className="ai-triage-tag">{name}</span>), undoButton('tags', autoTags.join(', ')), false,
        `${text.tags} · ${text.addedLower}`)}
      {personTags.length > 0 && row(text.tags, personTags.map((name) => <span key={name} className="ai-triage-tag">{name}</span>),
        parts.tags.length > 0 ? button(text.acceptTags(parts.tags.map((id) => tagName(id) ?? '').join(', ')), { tag_ids: parts.tags }) : done(text.added))}
      {auto.priority ? row(text.priority, messages.priorities[auto.priority as keyof typeof messages.priorities] ?? auto.priority,
        undoButton('priority', messages.priorities[auto.priority as keyof typeof messages.priorities] ?? auto.priority), false, `${text.priority} · ${text.setLower}`)
        : suggestion.priority && row(text.priority, messages.priorities[suggestion.priority],
          parts.priority ? button(text.acceptPriority(messages.priorities[suggestion.priority]), { priority: suggestion.priority }) : done(text.set))}
      {skipped('priority')}
      {suggestion.stale.priority && row(text.priority, text.stalePriority(messages.priorities[suggestion.stale.priority]), null, true)}
      {auto.assignee_agent_id ? row(text.assignee, agentName(auto.assignee_agent_id), undoButton('assignee', agentName(auto.assignee_agent_id)), false, `${text.assignee} · ${text.setLower}`)
        : suggestion.assignee_agent_id && row(text.assignee, agentName(suggestion.assignee_agent_id),
          parts.assignee ? button(text.acceptAssignee(agentName(suggestion.assignee_agent_id)), { assignee_agent_id: suggestion.assignee_agent_id }) : done(text.assigned))}
      {skipped('assignee')}
      {suggestion.stale.assignee_agent_id && row(text.assignee, text.staleAssignee(agentName(suggestion.stale.assignee_agent_id)), null, true)}
    </div>
  );
}
