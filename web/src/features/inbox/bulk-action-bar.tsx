import { useRef } from 'react';
import type { BulkConversationChanges } from '../../api/client';
import type { Agent, Tag } from '../../api/schemas';
import { messages } from '../../messages';

interface BulkActionBarProps {
  count: number;
  agents: Agent[];
  tags: Tag[];
  currentAgentId?: string;
  busy: boolean;
  onApply: (changes: BulkConversationChanges) => void;
  onClear: () => void;
}

interface ActionMenuProps {
  label: string;
  disabled: boolean;
  children: (apply: (changes: BulkConversationChanges) => void) => React.ReactNode;
  onApply: (changes: BulkConversationChanges) => void;
}

// A menu of choices that each apply at once. A button per choice, not a select: a select fires on
// every arrow key in some browsers, and one stray key would rewrite fifty conversations.
function ActionMenu({ label, disabled, children, onApply }: ActionMenuProps) {
  const menu = useRef<HTMLDetailsElement>(null);
  const apply = (changes: BulkConversationChanges) => {
    if (menu.current) {
      menu.current.open = false;
      // The pressed button is about to be hidden; keep focus on the menu that opened it.
      menu.current.querySelector('summary')?.focus();
    }
    onApply(changes);
  };
  const closeOnEscape = (event: React.KeyboardEvent<HTMLDetailsElement>) => {
    if (event.key !== 'Escape' || !menu.current?.open) return;
    menu.current.open = false;
    menu.current.querySelector('summary')?.focus();
  };
  const closeOnFocusOut = (event: React.FocusEvent<HTMLDetailsElement>) => {
    if (menu.current?.open && !menu.current.contains(event.relatedTarget as Node | null)) menu.current.open = false;
  };
  return (
    <details className="filter-menu action-menu" ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut} data-disabled={disabled || undefined}>
      <summary aria-disabled={disabled} onClick={(event) => { if (disabled) event.preventDefault(); }}>{label}</summary>
      <div className="filter-popover action-popover" role="group" aria-label={label}>{children(apply)}</div>
    </details>
  );
}

export function BulkActionBar({ count, agents, tags, currentAgentId, busy, onApply, onClear }: BulkActionBarProps) {
  const others = agents.filter((agent) => agent.id !== currentAgentId);
  const tagChoices = (key: 'add_tag_ids' | 'remove_tag_ids') => (apply: (changes: BulkConversationChanges) => void) => (
    tags.length === 0 ? <p className="muted-copy">{messages.noTagsToPick}</p>
      : tags.map((tag) => <button type="button" key={tag.id} onClick={() => apply({ [key]: [tag.id] })}>{tag.name}</button>)
  );
  return (
    <div className="bulk-bar" role="group" aria-label={messages.bulkActions}>
      <strong className="bulk-count">{messages.selectedCount(count)}</strong>
      <div className="bulk-actions">
        <ActionMenu label={messages.setStatus} disabled={busy} onApply={onApply}>
          {(apply) => Object.entries(messages.statuses).map(([value, label]) => (
            <button type="button" key={value} onClick={() => apply({ status: value as keyof typeof messages.statuses })}>{label}</button>))}
        </ActionMenu>
        <ActionMenu label={messages.assignTo} disabled={busy} onApply={onApply}>
          {(apply) => <>
            {currentAgentId && <button type="button" onClick={() => apply({ assignee_id: currentAgentId })}>{messages.assignToMe}</button>}
            <button type="button" onClick={() => apply({ assignee_id: null })}>{messages.unassigned}</button>
            {others.map((agent) => <button type="button" key={agent.id} onClick={() => apply({ assignee_id: agent.id })}>{agent.display_name || agent.email}</button>)}
          </>}
        </ActionMenu>
        <ActionMenu label={messages.addTag} disabled={busy} onApply={onApply}>{tagChoices('add_tag_ids')}</ActionMenu>
        <ActionMenu label={messages.removeTag} disabled={busy} onApply={onApply}>{tagChoices('remove_tag_ids')}</ActionMenu>
      </div>
      <button type="button" className="bulk-clear" disabled={busy} onClick={onClear}>{messages.clearSelection}</button>
    </div>
  );
}
