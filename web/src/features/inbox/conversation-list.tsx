import { Fragment, useEffect, useRef } from 'react';
import type { ReactNode, Ref } from 'react';
import type { Conversation, OnboardingGoal } from '../../api/schemas';
import { dayGroup, formatDate, formatShortDate } from '../../lib/format-date';
import { useNow } from '../../lib/use-now';
import { messages } from '../../messages';
import { ChevronLeftIcon, ChevronRightIcon } from '../../ui/icons';
import { PriorityLabel, StatusLabel } from './conversation-labels';

interface ConversationListProps {
  items: Conversation[];
  selectedId?: string;
  timezone: string;
  onSelect?: (id: string) => void;
  page: number;
  hasNextPage: boolean;
  onPage?: (page: number) => void;
  filtered?: boolean;
  // The Open view is empty but the workspace has conversations: caught up, not new.
  nothingOpen?: boolean;
  loading?: boolean;
  // refreshing: a refetch is running while the previous rows stay on screen.
  refreshing?: boolean;
  failed?: boolean;
  onRetry?: () => void;
  onClearFilters?: () => void;
  canManage?: boolean;
  // onOpenSettings opens a settings page in the app: Settings, or the import page.
  onOpenSettings?: (to?: SettingsTarget) => void;
  // checkedIds are the rows ticked for a bulk action; ticking never opens the conversation.
  checkedIds?: ReadonlySet<string>;
  onToggle?: (id: string) => void;
  onToggleAll?: (checked: boolean) => void;
  selectAllRef?: Ref<HTMLInputElement>;
  // toolbar takes the place of the select all caption while rows are ticked.
  toolbar?: ReactNode;
  // draftIds are the conversations with a reply draft an AI agent left for a person to send.
  draftIds?: ReadonlySet<string>;
  // grouped splits the rows under Today and Earlier; only the latest-activity order keeps the groups contiguous.
  grouped?: boolean;
  // onboardingGoal is what the team said it wants first at sign-up; its step leads the get-started list.
  onboardingGoal?: OnboardingGoal | null;
}

type Step = 'connect' | 'import' | 'invite' | 'mcp';

// The step each sign-up goal starts with; any other goal keeps the usual order.
const firstStep: Partial<Record<OnboardingGoal, Step>> = { leave_helpdesk: 'import', shared_inbox: 'connect', lower_cost: 'invite', ai_agents: 'mcp' };

// getStartedSteps lists the steps this agent can take, the goal's step first. The settings steps stay
// admin-only; a goal whose step this agent cannot take recommends nothing.
export type SettingsTarget = '/settings' | '/settings/import-freshdesk';

function getStartedSteps(goal: OnboardingGoal | null | undefined, canManage: boolean): { steps: Step[]; recommended?: Step } {
  const lead = goal ? firstStep[goal] : undefined;
  const steps = (['connect', 'import', 'invite'] as Step[]).filter((step) => canManage || step === 'import');
  if (lead === 'mcp') return { steps: ['mcp', ...steps], recommended: 'mcp' };
  if (!lead || !steps.includes(lead)) return { steps };
  return { steps: [lead, ...steps.filter((step) => step !== lead)], recommended: lead };
}

const visibleTags = 2;

export function ConversationList({ items, selectedId, timezone, onSelect, page, hasNextPage, onPage, filtered = false, nothingOpen = false, loading = false, refreshing = false,
  failed = false, onRetry, onClearFilters, canManage = false, onOpenSettings, checkedIds, onToggle, onToggleAll, selectAllRef, toolbar, draftIds, grouped = true, onboardingGoal }: ConversationListProps) {
  // An empty list is only "new workspace" when it loaded; a failed request must never look empty.
  const now = useNow(timezone);
  const settled = !loading && !failed;
  const brandNew = items.length === 0 && !filtered && !nothingOpen && settled && page === 1;
  const checkedCount = items.filter((item) => checkedIds?.has(item.id)).length;
  const allChecked = items.length > 0 && checkedCount === items.length;
  const selectAll = useRef<HTMLInputElement | null>(null);
  const setSelectAll = (node: HTMLInputElement | null) => {
    selectAll.current = node;
    if (typeof selectAllRef === 'function') selectAllRef(node);
    else if (selectAllRef) selectAllRef.current = node;
  };
  useEffect(() => {
    if (selectAll.current) selectAll.current.indeterminate = checkedCount > 0 && !allChecked;
  }, [checkedCount, allChecked]);
  const settingsLink = (label: string, to: SettingsTarget = '/settings') => <a href={to} onClick={(event) => {
    if (!onOpenSettings) return;
    event.preventDefault();
    onOpenSettings(to);
  }}>{label}</a>;
  const getStarted = getStartedSteps(onboardingGoal, canManage);
  const stepLink: Record<Step, ReactNode> = {
    connect: settingsLink(messages.connectEmail),
    // An admin imports in the app; an agent reads how, to ask an admin.
    import: canManage ? settingsLink(messages.importFreshdesk, '/settings/import-freshdesk')
      : <a href={messages.importFreshdeskURL} target="_blank" rel="noreferrer">{messages.importFreshdesk}</a>,
    invite: settingsLink(messages.inviteTeam),
    mcp: <a href={messages.mcpURL} target="_blank" rel="noreferrer">{messages.connectAIAgent}</a>,
  };
  return (
    <div className="conversation-scroll" aria-busy={refreshing}>
      {loading && items.length === 0 && <p role="status" className="empty-copy">{messages.loading}</p>}
      {brandNew && <section className="get-started" aria-label={messages.getStarted}>
        <h2>{messages.noConversationsYet}</h2>
        <p className="muted-copy">{messages.getStartedExplanation}</p>
        <ul>
          {getStarted.steps.map((step) => <li key={step} data-recommended={step === getStarted.recommended || undefined}>
            {step === getStarted.recommended && <><span className="recommended-step">{messages.recommendedFirstStep}</span>{' '}</>}{stepLink[step]}</li>)}
        </ul>
      </section>}
      {failed && <div className="empty-copy"><p role="alert" className="error-copy">{messages.conversationsFailed}</p>
        <button type="button" onClick={() => onRetry?.()}>{messages.tryAgain}</button></div>}
      {items.length === 0 && !brandNew && settled && <div className="empty-copy"><p>{nothingOpen ? messages.noOpenConversations : messages.noConversations}</p>
        {filtered && onClearFilters && <button type="button" onClick={onClearFilters}>{messages.clearFilters}</button>}</div>}
      {items.length > 0 && <div className="select-all" data-active={toolbar ? true : undefined}>
        <label>
          <input type="checkbox" ref={setSelectAll} checked={allChecked} onChange={(event) => onToggleAll?.(event.target.checked)} />
          <span className={toolbar ? 'sr-only' : undefined}>{messages.selectAllOnPage}</span>
        </label>
        {toolbar}
      </div>}
      {items.map((item, index) => {
        const group = dayGroup(item.updated_at, timezone, now);
        const startsGroup = grouped && (index === 0 || group !== dayGroup(items[index - 1].updated_at, timezone, now));
        const full = formatDate(item.updated_at, timezone, now);
        return (
          <Fragment key={item.id}>
          {startsGroup && <h2 className="group-label">{messages.dayGroups[group]}</h2>}
          <div className="conversation-item">
          <label className="row-check">
            <input type="checkbox" aria-label={messages.selectRow(item.subject, full)} checked={checkedIds?.has(item.id) ?? false} onChange={() => onToggle?.(item.id)} />
          </label>
          <button className="conversation-row" aria-current={item.id === selectedId ? true : undefined} data-tip={full} onClick={() => onSelect?.(item.id)}>
            <span className="conversation-subject">{item.subject}</span>
            <time dateTime={item.updated_at}>
              <span aria-hidden="true" className="time-short">{formatShortDate(item.updated_at, timezone, now)}</span>
              <span className="time-full">{full}</span>
            </time>
            <span className="conversation-meta">
              <StatusLabel status={item.status} />
              <PriorityLabel priority={item.priority} />
              {draftIds?.has(item.id) && <span className="badge badge-draft">{messages.draftWaiting}</span>}
              {item.tags.slice(0, visibleTags).map((tag) => <span className="tag-pill" key={tag.id}>{tag.name}</span>)}
              {item.tags.length > visibleTags && <span className="tag-pill">+{item.tags.length - visibleTags}</span>}
            </span>
          </button>
          </div>
          </Fragment>
        );
      })}
      {(page > 1 || hasNextPage) && <nav className="pagination" aria-label={messages.conversationList}>
        <button type="button" className="icon-button" aria-label={messages.previousPage} data-tip={messages.previousPage} disabled={page <= 1} onClick={() => onPage?.(page - 1)}><ChevronLeftIcon /></button>
        <span>{page}</span>
        <button type="button" className="icon-button" aria-label={messages.nextPage} data-tip={messages.nextPage} disabled={!hasNextPage} onClick={() => onPage?.(page + 1)}><ChevronRightIcon /></button>
      </nav>}
    </div>
  );
}
