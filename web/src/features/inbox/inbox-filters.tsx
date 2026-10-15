import { useEffect, useRef, useState } from 'react';
import { messages } from '../../messages';
import type { Tag } from '../../api/schemas';
import { FilterIcon, SearchIcon } from '../../ui/icons';

interface InboxFiltersProps {
  tags: Tag[];
  onChange?: (filters: Record<string, string>) => void;
  focusSearch?: boolean;
}

const statusTabs = [['', messages.allConversations], ...Object.entries(messages.statuses)] as const;

// Search and the Filter menu sit above the status tabs, in the same order on screen and for Tab.
// Status is the view an agent switches most, so it is a row of tabs; assignee and tag are rarer and
// share one Filter menu that shows how many are on.
export function InboxFilters({ tags, onChange, focusSearch = false }: InboxFiltersProps) {
  const [active, setActive] = useState(0);
  const menu = useRef<HTMLDetailsElement>(null);
  const update = (event: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const form = event.currentTarget.form;
    if (!form) return;
    const values = Object.fromEntries(new FormData(form).entries()) as Record<string, string>;
    setActive([values.assignee, values.tag].filter(Boolean).length);
    onChange?.(values);
  };
  useEffect(() => {
    const close = (event: PointerEvent) => { if (menu.current?.open && !menu.current.contains(event.target as Node)) menu.current.open = false; };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, []);
  // Keyboard users leave the menu with Tab; it closes like it does on an outside click.
  const closeOnFocusOut = (event: React.FocusEvent<HTMLDetailsElement>) => {
    if (menu.current?.open && !menu.current.contains(event.relatedTarget as Node | null)) menu.current.open = false;
  };
  const closeOnEscape = (event: React.KeyboardEvent<HTMLDetailsElement>) => {
    if (event.key !== 'Escape' || !menu.current?.open) return;
    menu.current.open = false;
    menu.current.querySelector('summary')?.focus();
  };
  return (
    <form className="inbox-filters" aria-label={messages.inbox} onSubmit={(event) => event.preventDefault()}>
      <div className="filter-bar">
        <label className="search-field"><SearchIcon />
          <input name="search" type="search" aria-label={messages.searchConversations} placeholder={messages.searchConversations} autoFocus={focusSearch} onChange={update} /></label>
        <details className="filter-menu" ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut}>
          <summary><FilterIcon /><span>{messages.filter}</span>
            {active > 0 && <span className="filter-count" aria-label={messages.filtersActive(active)}>{active}</span>}</summary>
          <div className="filter-popover">
            <label className="field"><span>{messages.assignee}</span><select name="assignee" aria-label={messages.assignee} onChange={update}>
              <option value="">{messages.allAssignees}</option>
              <option value="me">{messages.assignedToMe}</option>
              <option value="unassigned">{messages.unassigned}</option>
            </select></label>
            <label className="field"><span>{messages.tag}</span><select name="tag" aria-label={messages.tag} onChange={update}>
              <option value="">{messages.allTags}</option>
              {tags.map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}
            </select></label>
          </div>
        </details>
      </div>
      <div className="status-tabs" role="radiogroup" aria-label={messages.status}>
        {statusTabs.map(([value, label]) => (
          <label key={value}><input type="radio" name="status" value={value} defaultChecked={value === ''} onChange={update} /><span>{label}</span></label>
        ))}
      </div>
    </form>
  );
}
