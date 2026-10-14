import { messages } from '../../messages';
import type { Tag } from '../../api/schemas';

interface InboxFiltersProps {
  tags: Tag[];
  onChange?: (filters: Record<string, string>) => void;
}

export function InboxFilters({ tags, onChange }: InboxFiltersProps) {
  const update = (event: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const form = event.currentTarget.form;
    if (!form) return;
    onChange?.(Object.fromEntries(new FormData(form).entries()) as Record<string, string>);
  };
  return (
    <form className="inbox-filters" aria-label={messages.inbox}>
      <input name="search" type="search" aria-label={messages.searchConversations} placeholder={messages.searchConversations} onChange={update} />
      <label><span>{messages.status}</span><select name="status" aria-label={messages.status} onChange={update}>
        <option value="">{messages.allStatuses}</option>
        {Object.entries(messages.statuses).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <label><span>{messages.assignee}</span><select name="assignee" aria-label={messages.assignee} onChange={update}>
        <option value="">{messages.allAssignees}</option>
        <option value="me">{messages.assignedToMe}</option>
        <option value="unassigned">{messages.unassigned}</option>
      </select></label>
      <label><span>{messages.tag}</span><select name="tag" aria-label={messages.tag} onChange={update}>
        <option value="">{messages.allTags}</option>
        {tags.map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}
      </select></label>
    </form>
  );
}
