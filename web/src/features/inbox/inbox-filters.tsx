import { messages } from '../../messages';
import type { Tag } from '../../api/schemas';
import { SearchIcon } from '../../ui/icons';

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
  // Each select's first option names its filter ("All statuses"), so the labels stay visually hidden.
  return (
    <form className="inbox-filters" aria-label={messages.inbox} onSubmit={(event) => event.preventDefault()}>
      <label className="search-field"><SearchIcon />
        <input name="search" type="search" aria-label={messages.searchConversations} placeholder={messages.searchConversations} onChange={update} /></label>
      <div className="filter-row">
        <label><span className="sr-only">{messages.status}</span><select name="status" aria-label={messages.status} onChange={update}>
          <option value="">{messages.allStatuses}</option>
          {Object.entries(messages.statuses).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select></label>
        <label><span className="sr-only">{messages.assignee}</span><select name="assignee" aria-label={messages.assignee} onChange={update}>
          <option value="">{messages.allAssignees}</option>
          <option value="me">{messages.assignedToMe}</option>
          <option value="unassigned">{messages.unassigned}</option>
        </select></label>
        <label><span className="sr-only">{messages.tag}</span><select name="tag" aria-label={messages.tag} onChange={update}>
          <option value="">{messages.allTags}</option>
          {tags.map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}
        </select></label>
      </div>
    </form>
  );
}
