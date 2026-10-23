import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { Tag, TeamMember } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { CheckIcon, SparklesIcon } from '../../ui/icons';
import { aiTriageApi } from './ai-triage-api';
import type { AITriage, TriagePick } from './ai-triage-api';
import { refusalCode, SettingsWay } from './ai-failure';
import { AISources } from './ai-sources';
import './ai-triage.css';

// Strings of the suggestion card; it is a lazy part, so they live in its chunk.
const text = {
  heading: 'Suggested by AI',
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
  stalePriority: (value: string) => `Not suggested: you set ${value} before this arrived.`,
  staleAssignee: (name: string) => `Not suggested: you assigned ${name} before this arrived.`,
  acceptedBy: (who: string, at: string) => `Accepted by ${who}, ${at}. Change a field below to undo.`,
  you: 'you',
  someone: 'a teammate',
  partly: 'Part of it was applied; the rest was dismissed.',
  dismissed: 'Dismissed.',
  expired: 'Nobody used it for 7 days, so it expired.',
  superseded: 'Replaced by a newer suggestion.',
  stopped: 'Stopped.',
  loadFailed: 'The suggestion could not be loaded. Try again in a moment.',
  refusedFallback: 'The suggestion could not be asked for. Try again in a moment.',
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

function hasOpenPart(suggestion: AITriage) {
  const parts = openParts(suggestion);
  return parts.tags.length > 0 || parts.priority || parts.assignee;
}

// AITriageCard shows the conversation's newest AI triage suggestion: tags, priority and assignee, each
// with Accept, then Accept all or Dismiss, its reason and its sources. A new ticket gets one by itself;
// a person can ask again. Nothing changes on the conversation until someone accepts.
export function AITriageCard({ conversationId, tags, agents, currentAgentId, timezone = 'UTC', canManage = false, onApplied, onWaiting }: AITriageCardProps) {
  const client = useQueryClient();
  const key = ['ai-triage', conversationId];
  // While it is written the card reads it again every 3 s; a failed read stops that (Try again is the query's own).
  const latest = useQuery({ queryKey: key, queryFn: () => aiTriageApi.latest(conversationId),
    refetchInterval: (query) => (query.state.status !== 'error' && writing(query.state.data?.suggestion) ? 3000 : false) });
  const suggestion = latest.data?.suggestion ?? null;
  const waiting = suggestion?.status === 'ready' && suggestion.outcome === null && hasOpenPart(suggestion);
  useEffect(() => {
    onWaiting?.(waiting);
  }, [onWaiting, waiting]);
  useEffect(() => () => onWaiting?.(false), [onWaiting]);
  const set = (next: AITriage) => client.setQueryData(key, { suggestion: next });
  const ask = useMutation({ mutationFn: () => aiTriageApi.create(conversationId), onSuccess: set });
  const accept = useMutation({ mutationFn: ({ target, pick }: { target: AITriage; pick?: TriagePick }) => aiTriageApi.accept(conversationId, target.id, pick),
    onSuccess: (next) => { set(next); onApplied(); } });
  const dismiss = useMutation({ mutationFn: (target: AITriage) => aiTriageApi.dismiss(conversationId, target.id), onSuccess: set });
  const refused = [ask, accept, dismiss].find((mutation) => mutation.isError)?.error;
  const busy = ask.isPending || accept.isPending || dismiss.isPending;
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
    body = <p className="ai-triage-message">{message} <SettingsWay code={suggestion.error_code} canManage={canManage} /></p>;
    actions = askButton(suggestion.error_code === 'automatic_limit' ? text.suggestNow : text.tryAgain);
  } else if (suggestion.status === 'cancelled') {
    body = <p className="ai-triage-message">{text.stopped}</p>;
    actions = askButton(text.again);
  } else {
    const open = suggestion.outcome === null;
    const anything = suggestion.tag_ids.length > 0 || suggestion.priority || suggestion.assignee_agent_id;
    const stale = suggestion.stale.priority || suggestion.stale.assignee_agent_id;
    if (!anything && !stale) {
      body = <p className="ai-triage-message">{text.nothing}</p>;
      actions = askButton(text.again);
    } else {
      body = (
        <>
          <TriageRows suggestion={suggestion} tags={tags} agents={agents} open={open} busy={busy} onAccept={(pick) => accept.mutate({ target: suggestion, pick })} />
          {suggestion.reason && <p className="ai-triage-reason">{suggestion.reason}</p>}
          <AISources sources={suggestion.sources} />
          {open && <p className="muted-copy ai-triage-note">{text.generated}</p>}
        </>
      );
      if (open) {
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
        body = <>{body}<p className="muted-copy">{after}</p></>;
        actions = suggestion.outcome === 'accepted' ? null : askButton(text.again);
      }
    }
  }
  return (
    <section className="details-section ai-triage" aria-label={text.heading} aria-busy={latest.isPending || writing(suggestion)}>
      <h2><SparklesIcon />{text.heading}</h2>
      {body}
      {refused && <p role="alert" className="error-copy">{refused instanceof Error && refused.message ? refused.message : text.refusedFallback}
        {' '}<SettingsWay code={refusalCode(refused)} canManage={canManage} /></p>}
      {actions && <div className="ai-triage-actions">{actions}</div>}
    </section>
  );
}

interface TriageRowsProps {
  suggestion: AITriage;
  tags: Tag[];
  agents: TeamMember[];
  open: boolean;
  busy: boolean;
  onAccept: (pick: TriagePick) => void;
}

// TriageRows is a row for each part: what it suggests, and Accept, or what was done with it.
function TriageRows({ suggestion, tags, agents, open, busy, onAccept }: TriageRowsProps) {
  const parts = openParts(suggestion);
  const agentName = (id: string | null) => {
    const agent = agents.find((item) => item.id === id);
    return agent ? agent.display_name || agent.email : '';
  };
  const tagNames = suggestion.tag_ids.map((id) => tags.find((tag) => tag.id === id)?.name).filter((name): name is string => Boolean(name));
  const done = (label: string) => <span className="ai-triage-done"><CheckIcon />{label}</span>;
  const row = (label: string, value: ReactNode, action: ReactNode, stale = false) => (
    <div role="group" aria-label={label} className={stale ? 'ai-triage-row stale' : 'ai-triage-row'}>
      <div><div className="ai-triage-key">{label}</div><div className="ai-triage-value">{value}</div></div>
      {action}
    </div>
  );
  const button = (name: string, pick: TriagePick) => open && <button type="button" aria-label={name} disabled={busy} onClick={() => onAccept(pick)}>{text.accept}</button>;
  return (
    <div className="ai-triage-rows">
      {tagNames.length > 0 && row(text.tags, tagNames.map((name) => <span key={name} className="ai-triage-tag">{name}</span>),
        parts.tags.length > 0 ? button(text.acceptTags(parts.tags.map((id) => tags.find((tag) => tag.id === id)?.name ?? '').join(', ')), { tag_ids: parts.tags }) : done(text.added))}
      {suggestion.priority && row(text.priority, messages.priorities[suggestion.priority],
        parts.priority ? button(text.acceptPriority(messages.priorities[suggestion.priority]), { priority: suggestion.priority }) : done(text.set))}
      {suggestion.stale.priority && row(text.priority, text.stalePriority(messages.priorities[suggestion.stale.priority]), null, true)}
      {suggestion.assignee_agent_id && row(text.assignee, agentName(suggestion.assignee_agent_id),
        parts.assignee ? button(text.acceptAssignee(agentName(suggestion.assignee_agent_id)), { assignee_agent_id: suggestion.assignee_agent_id }) : done(text.assigned))}
      {suggestion.stale.assignee_agent_id && row(text.assignee, text.staleAssignee(agentName(suggestion.stale.assignee_agent_id)), null, true)}
    </div>
  );
}
