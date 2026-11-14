import { useState } from 'react';
import type { BulkConversationChanges } from '../../api/client';
import type { Conversation, TeamMember, Tag } from '../../api/schemas';
import { messages } from '../../messages';
import { BanIcon, CheckCircleIcon, CheckIcon, CircleDotIcon, MinusIcon, RotateCcwIcon, TagIcon, UserIcon, XIcon } from '../../ui/icons';
import { ActionMenu } from './action-menu';
import { mayTellCustomers } from './status-tell';
import { StatusLabel } from './conversation-labels';

interface BulkActionBarProps {
  selected: Conversation[];
  // How many rows the page shows, of which the selection is part.
  total: number;
  agents: TeamMember[];
  tags: Tag[];
  currentAgentId?: string;
  busy: boolean;
  onApply: (changes: BulkConversationChanges) => void;
  // Files the selection in Spam (true) or takes it out (false); missing where the caller cannot.
  onSpam?: (spam: boolean) => void;
  onClear: () => void;
}

const settable = ['open', 'pending', 'resolved', 'closed'] as const;

// tagState says whether every, some or none of the selected conversations carry the tag.
function tagState(selected: Conversation[], tagId: string): 'true' | 'mixed' | 'false' {
  const carrying = selected.filter((item) => item.tags.some((tag) => tag.id === tagId)).length;
  if (carrying === 0) return 'false';
  return carrying === selected.length ? 'true' : 'mixed';
}

export function BulkActionBar({ selected, total, agents, tags, currentAgentId, busy, onApply, onSpam, onClear }: BulkActionBarProps) {
  const others = agents.filter((agent) => agent.id !== currentAgentId && !agent.deactivated_at);
  // Ticked again for each new selection: the bar is drawn afresh when rows are picked after none.
  const [tell, setTell] = useState(true);
  // A conversation in Spam keeps its status until Not spam takes it out, so the Spam view offers that instead.
  const inSpam = selected.length > 0 && selected.every((item) => item.status === 'spam');
  const offersTell = !inSpam && mayTellCustomers(selected);
  const quiet = offersTell && !tell;
  const setStatus = (changes: BulkConversationChanges) => onApply(quiet ? { ...changes, notify: false } : changes);
  const resolveLabel = !offersTell ? messages.resolve : quiet ? messages.resolveQuietlyBulk : messages.resolveAndTellBulk;
  const statusLabel = quiet ? messages.setStatusQuietly : messages.setStatus;
  const clearOnEscape = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape' && !busy) onClear();
  };
  return (
    <div className="bulk-bar" role="group" aria-label={messages.bulkActions} onKeyDown={clearOnEscape}>
      <strong className="bulk-count">{messages.selectedCount(selected.length, total)}</strong>
      {offersTell && <label className="bulk-tell" data-tip={messages.tellCustomersTip}>
        <input type="checkbox" checked={tell} disabled={busy} onChange={(event) => setTell(event.target.checked)} />{messages.tellCustomers}</label>}
      <div className="bulk-actions">
        {inSpam ? onSpam && <button type="button" className="icon-button" aria-label={messages.notSpam} data-tip={messages.notSpamTip} disabled={busy}
          onClick={() => onSpam(false)}><RotateCcwIcon /></button>
          : <>
            <button type="button" className="icon-button bulk-resolve" aria-label={resolveLabel} data-tip={resolveLabel} disabled={busy}
              onClick={() => setStatus({ status: 'resolved' })}><CheckCircleIcon /></button>
            <ActionMenu label={statusLabel} icon={<CircleDotIcon />} disabled={busy} onApply={setStatus}>
              {(apply) => settable.map((status) => <button type="button" key={status} onClick={() => apply({ status })}><StatusLabel status={status} /></button>)}
            </ActionMenu>
          </>}
        <ActionMenu label={messages.assignTo} icon={<UserIcon />} disabled={busy} onApply={onApply}>
          {(apply) => <>
            {currentAgentId && <button type="button" onClick={() => apply({ assignee_id: currentAgentId })}>{messages.assignToMe}</button>}
            <button type="button" onClick={() => apply({ assignee_id: null })}>{messages.unassigned}</button>
            {others.map((agent) => <button type="button" key={agent.id} onClick={() => apply({ assignee_id: agent.id })}>{agent.display_name || agent.email}</button>)}
          </>}
        </ActionMenu>
        <ActionMenu label={messages.tags} icon={<TagIcon />} disabled={busy} onApply={onApply}>
          {(apply) => tags.length === 0 ? <p className="muted-copy">{messages.noTagsToPick}</p>
            : tags.map((tag) => {
              // A tag every selected row carries comes off; otherwise it goes on the rows that lack it.
              const state = tagState(selected, tag.id);
              return (
                <button type="button" key={tag.id} aria-pressed={state}
                  onClick={() => apply(state === 'true' ? { remove_tag_ids: [tag.id] } : { add_tag_ids: [tag.id] })}>
                  <span className="tag-mark" aria-hidden="true">{state === 'true' ? <CheckIcon /> : state === 'mixed' ? <MinusIcon /> : null}</span>
                  {tag.name}
                </button>
              );
            })}
        </ActionMenu>
        {!inSpam && onSpam && <button type="button" className="icon-button" aria-label={messages.spam} data-tip={messages.spamTip} disabled={busy}
          onClick={() => onSpam(true)}><BanIcon /></button>}
        <button type="button" className="icon-button" aria-label={messages.clearSelection} data-tip={messages.clearSelection} disabled={busy} onClick={onClear}><XIcon /></button>
      </div>
    </div>
  );
}
