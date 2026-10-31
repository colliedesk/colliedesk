import type { BulkConversationChanges } from '../../api/client';
import type { Conversation, TeamMember, Tag } from '../../api/schemas';
import { messages } from '../../messages';
import { CheckCircleIcon, CheckIcon, CircleDotIcon, MinusIcon, TagIcon, UserIcon, XIcon } from '../../ui/icons';
import { ActionMenu } from './action-menu';
import { StatusLabel } from './conversation-labels';

interface BulkActionBarProps {
  selected: Conversation[];
  agents: TeamMember[];
  tags: Tag[];
  currentAgentId?: string;
  busy: boolean;
  onApply: (changes: BulkConversationChanges) => void;
  onClear: () => void;
}

// tagState says whether every, some or none of the selected conversations carry the tag.
function tagState(selected: Conversation[], tagId: string): 'true' | 'mixed' | 'false' {
  const carrying = selected.filter((item) => item.tags.some((tag) => tag.id === tagId)).length;
  if (carrying === 0) return 'false';
  return carrying === selected.length ? 'true' : 'mixed';
}

export function BulkActionBar({ selected, agents, tags, currentAgentId, busy, onApply, onClear }: BulkActionBarProps) {
  const others = agents.filter((agent) => agent.id !== currentAgentId && !agent.deactivated_at);
  const clearOnEscape = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape' && !busy) onClear();
  };
  return (
    <div className="bulk-bar" role="group" aria-label={messages.bulkActions} onKeyDown={clearOnEscape}>
      <strong className="bulk-count">{messages.selectedCount(selected.length)}</strong>
      <div className="bulk-actions">
        <button type="button" className="icon-button bulk-resolve" aria-label={messages.resolve} data-tip={messages.resolve} disabled={busy}
          onClick={() => onApply({ status: 'resolved' })}><CheckCircleIcon /></button>
        <ActionMenu label={messages.setStatus} icon={<CircleDotIcon />} disabled={busy} onApply={onApply}>
          {(apply) => Object.keys(messages.statuses).map((value) => {
            const status = value as keyof typeof messages.statuses;
            return <button type="button" key={status} onClick={() => apply({ status })}><StatusLabel status={status} /></button>;
          })}
        </ActionMenu>
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
        <button type="button" className="icon-button" aria-label={messages.clearSelection} data-tip={messages.clearSelection} disabled={busy} onClick={onClear}><XIcon /></button>
      </div>
    </div>
  );
}
