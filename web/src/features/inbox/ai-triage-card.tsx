import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { Tag, TeamMember } from '../../api/schemas';
import { messages } from '../../messages';
import { SparklesIcon } from '../../ui/icons';
import { aiTriageApi } from './ai-triage-api';
import type { AITriage } from './ai-triage-api';
import './ai-triage.css';

// Strings of the suggestion card; it is a lazy part, so they live in its chunk.
const text = {
  heading: 'Suggested by AI',
  writing: 'Suggesting tags, priority and assignee…',
  apply: 'Apply',
  applySelected: 'Apply selected',
  dismiss: 'Dismiss',
  suggest: 'Suggest tags and priority',
  again: 'Suggest again',
  nothing: 'No tag, priority or assignee to suggest.',
  priority: (value: string) => `Priority: ${value}`,
  assignee: (name: string) => `Assign to ${name}`,
  applied: 'Applied. Change anything above as usual.',
  dismissed: 'Dismissed.',
  failed: 'No suggestion this time. Try again in a moment.',
  stopped: 'The suggestion was stopped.',
  loadFailed: 'The suggestion could not be loaded.',
  refusedFallback: 'The suggestion could not be asked for.',
  generated: 'AI suggestions; nothing changes until you apply them.',
};

type Item = { key: string; label: string };

interface AITriageCardProps {
  conversationId: string;
  tags: Tag[];
  agents: TeamMember[];
  // Called after a suggestion was applied, so the conversation is read again.
  onApplied: () => void;
}

const writing = (suggestion: AITriage | null | undefined) => suggestion?.status === 'queued' || suggestion?.status === 'running';

// AITriageCard shows the conversation's newest AI triage suggestion with a box for each part, applies the
// ones left checked, or dismisses it. A new ticket gets one by itself; a person can ask again.
export function AITriageCard({ conversationId, tags, agents, onApplied }: AITriageCardProps) {
  const client = useQueryClient();
  const key = ['ai-triage', conversationId];
  const latest = useQuery({ queryKey: key, queryFn: () => aiTriageApi.latest(conversationId),
    refetchInterval: (query) => (writing(query.state.data?.suggestion) ? 3000 : false) });
  const suggestion = latest.data?.suggestion ?? null;
  const items: Item[] = [];
  if (suggestion?.status === 'ready') {
    for (const id of suggestion.tag_ids) {
      const tag = tags.find((item) => item.id === id);
      if (tag) items.push({ key: `tag:${id}`, label: tag.name });
    }
    if (suggestion.priority) items.push({ key: 'priority', label: text.priority(messages.priorities[suggestion.priority]) });
    if (suggestion.assignee_agent_id) {
      const agent = agents.find((item) => item.id === suggestion.assignee_agent_id);
      if (agent) items.push({ key: 'assignee', label: text.assignee(agent.display_name || agent.email) });
    }
  }
  const [unchecked, setUnchecked] = useState<string[]>([]);
  useEffect(() => { setUnchecked([]); }, [suggestion?.id]);
  const set = (next: AITriage) => client.setQueryData(key, { suggestion: next });
  const ask = useMutation({ mutationFn: () => aiTriageApi.create(conversationId), onSuccess: set });
  const apply = useMutation({
    mutationFn: (target: AITriage) => {
      if (unchecked.length === 0) return aiTriageApi.accept(conversationId, target.id);
      const kept = items.filter((item) => !unchecked.includes(item.key)).map((item) => item.key);
      return aiTriageApi.accept(conversationId, target.id, {
        tag_ids: kept.filter((item) => item.startsWith('tag:')).map((item) => item.slice(4)),
        priority: kept.includes('priority') ? target.priority : null,
        assignee_agent_id: kept.includes('assignee') ? target.assignee_agent_id : null,
      });
    },
    onSuccess: (next) => { set(next); onApplied(); },
  });
  const dismiss = useMutation({ mutationFn: (target: AITriage) => aiTriageApi.dismiss(conversationId, target.id), onSuccess: set });
  const refusal = [ask, apply, dismiss].find((mutation) => mutation.isError)?.error;
  const open = suggestion?.status === 'ready' && suggestion.outcome === null;
  const busy = ask.isPending || apply.isPending || dismiss.isPending;
  return (
    <section className="details-section ai-triage" aria-label={text.heading} aria-busy={latest.isPending || writing(suggestion)}>
      <h2><SparklesIcon />{text.heading}</h2>
      {latest.isError && <p role="alert" className="error-copy">{text.loadFailed}</p>}
      {writing(suggestion) && <p role="status" className="muted-copy">{text.writing}</p>}
      {open && (items.length === 0 ? <p className="muted-copy">{text.nothing}</p> : (
        <>
          <ul className="ai-triage-items">
            {items.map((item) => (
              <li key={item.key}><label><input type="checkbox" checked={!unchecked.includes(item.key)}
                onChange={() => setUnchecked((current) => current.includes(item.key) ? current.filter((value) => value !== item.key) : [...current, item.key])} />{item.label}</label></li>
            ))}
          </ul>
          {suggestion.reason && <p className="ai-triage-reason">{suggestion.reason}</p>}
          <p className="muted-copy ai-triage-note">{text.generated}</p>
        </>
      ))}
      {suggestion?.outcome === 'accepted' || suggestion?.outcome === 'partly_accepted' ? <p className="muted-copy">{text.applied}</p>
        : suggestion?.outcome === 'dismissed' ? <p className="muted-copy">{text.dismissed}</p>
        : suggestion?.status === 'failed' ? <p className="muted-copy">{text.failed}</p>
        : suggestion?.status === 'cancelled' ? <p className="muted-copy">{text.stopped}</p> : null}
      {refusal && <p role="alert" className="error-copy">{refusal instanceof Error && refusal.message ? refusal.message : text.refusedFallback}</p>}
      <div className="ai-triage-actions">
        {open && items.length > 0 && <button type="button" className="primary-action" disabled={busy || unchecked.length === items.length} onClick={() => apply.mutate(suggestion)}>
          {unchecked.length > 0 ? text.applySelected : text.apply}</button>}
        {open && <button type="button" disabled={busy} onClick={() => dismiss.mutate(suggestion)}>{text.dismiss}</button>}
        {!open && !writing(suggestion) && !latest.isPending && <button type="button" disabled={busy} onClick={() => ask.mutate()}>{suggestion ? text.again : text.suggest}</button>}
      </div>
    </section>
  );
}
