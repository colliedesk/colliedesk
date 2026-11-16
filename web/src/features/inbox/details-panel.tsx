import { useId, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { UpdateConversationRequest } from '../../api/client';
import type { TeamMember, Conversation, ConversationOrigin, SettableStatus, Tag } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { clockLine, rowDue, slaOf } from '../../lib/sla';
import { useMinuteNow } from '../../lib/use-now';
import { messages } from '../../messages';
import { CheckIcon, ChevronDownIcon, ChevronRightIcon, UserIcon } from '../../ui/icons';
import { ActionMenu } from './action-menu';
import { AITriageSlot } from './ai-triage-slot';
import { StatusLabel } from './conversation-labels';
import { mayTell, noticesOf } from './status-tell';
import { AutofixDetails } from '../autofix/autofix-slot';
import { useHolds } from '../../lib/permissions-context';

interface DetailsPanelProps {
  conversation?: Conversation;
  agents: TeamMember[];
  tags: Tag[];
  // Each change settles when the server answered; a refusal rejects, and the details say it was not saved.
  onUpdate?: (conversationId: string, value: UpdateConversationRequest) => void | Promise<unknown>;
  onAddTag?: (conversationId: string, tagId: string) => void | Promise<unknown>;
  onRemoveTag?: (conversationId: string, tagId: string) => void | Promise<unknown>;
  onCreateTag?: (conversationId: string, name: string) => void;
  timezone?: string;
  onClose?: () => void;
  // Narrows the list to this customer's conversations.
  onShowContactConversations?: (contactId: string) => void;
  // Narrows the list to the conversations of the customer's organization (the organization filter).
  onShowOrganizationConversations?: (organization: string) => void;
  // Triage suggestions are on in the workspace; onTriageApplied reads the conversation again after a part is applied.
  aiTriage?: boolean;
  onTriageApplied?: () => void;
  // The signed-in agent and whether they are an admin, for the suggestion's wording and next steps.
  currentAgentId?: string;
  canManage?: boolean;
  // Told whether the suggestion has a part nobody accepted, for the toggle that hides the panel.
  onTriageWaiting?: (waiting: boolean) => void;
  // The server runs Autofix: the details open with its block.
  autofix?: boolean;
}

const statusOrder = ['pending', 'resolved', 'closed', 'open'] as const;

type Field = 'status' | 'priority' | 'assignee' | 'tags';
const fieldNames: Record<Field, string> = { status: messages.status, priority: messages.priority, assignee: messages.assignee, tags: messages.tags };

// The value an edit key ('status', 'priority', 'assignee', 'tag:<id>') has on the server, to tell whether a refused
// change still describes the row.
const serverValue = (conversation: Conversation, key: string) => key === 'status' ? conversation.status
  : key === 'priority' ? conversation.priority : key === 'assignee' ? conversation.assignee_id ?? ''
    : String(conversation.tags.some((tag) => `tag:${tag.id}` === key));

// useEdits keeps the details honest about a change on its way: while it saves, the value picked shows (not the old
// one), and one the server refuses goes back to the conversation's value with a message under its row. The message
// is about the latest pick of that field only, and goes once the field changes from elsewhere (the header, a teammate).
function useEdits(conversation?: Conversation) {
  const conversationId = conversation?.id;
  const [saving, setSaving] = useState<Record<string, { value: unknown; token: object }>>({});
  const [failed, setFailed] = useState<{ conversationId: string; field: Field; key: string; was: string }>();
  // The latest pick of each value: an earlier pick's answer, refused or not, no longer speaks for it.
  const latest = useRef<Record<string, object>>({});
  const shown = <T,>(key: string, value: T): T => {
    const edit = conversationId ? saving[`${conversationId}:${key}`] : undefined;
    return edit ? edit.value as T : value;
  };
  const save = (field: Field, key: string, value: unknown, change: () => void | Promise<unknown>) => {
    if (!conversation || !conversationId) return;
    const at = `${conversationId}:${key}`;
    const token = {};
    const was = serverValue(conversation, key);
    latest.current[at] = token;
    setFailed(undefined);
    setSaving((now) => ({ ...now, [at]: { value, token } }));
    const settle = () => setSaving((now) => {
      if (now[at]?.token !== token) return now;
      const rest = { ...now };
      delete rest[at];
      return rest;
    });
    let result: void | Promise<unknown>;
    try { result = change(); } catch (error) { result = Promise.reject(error); }
    Promise.resolve(result).then(settle, () => {
      settle();
      if (latest.current[at] === token) setFailed({ conversationId, field, key, was });
    });
  };
  const error = (field: Field) => conversation && failed && failed.conversationId === conversationId && failed.field === field && failed.was === serverValue(conversation, failed.key)
    ? <p role="alert" className="error-copy property-error">{messages.notSaved(fieldNames[field])}</p> : null;
  return { shown, save, error };
}

// Status is a menu, not a select: a closed select fires change on every arrow key in some browsers, and each
// step would change the conversation (and on a chat tell the customer). Where a status may tell, Tell the
// customer comes first, ticked each time the menu opens.
function StatusPick({ conversation, status = conversation?.status ?? 'open', onUpdate }: { conversation?: Conversation; status?: Conversation['status']; onUpdate?: DetailsPanelProps['onUpdate'] }) {
  const [tell, setTell] = useState(true);
  const id = useId();
  // Asked only where a status may tell: a chat whose channel is on and has words (the server's word).
  const asks = conversation ? mayTell(conversation) : false;
  const channel = conversation?.channel_ref?.name ?? messages.theChannel;
  const others = statusOrder.filter((value) => value !== status);
  const notices = conversation ? noticesOf(conversation) : [];
  const told = others.filter((value) => notices?.includes(value));
  const quiet = others.filter((value) => !told.includes(value));
  return <div className="property-row status-pick-row"><span id={`${id}-label`}>{messages.status}</span>
    {/* A conversation in Spam keeps its status until Not spam (the header) takes it out. */}
    <ActionMenu<SettableStatus> className="status-pick" label={messages.status} labelledBy={`${id}-label ${id}-value`} disabled={!conversation || !onUpdate || status === 'spam'}
      summary={<span id={`${id}-value`}><StatusLabel status={status} /></span>}
      onToggle={(open) => { if (open) setTell(true); }}
      onApply={(value) => conversation && onUpdate?.(conversation.id, !asks || tell ? { status: value } : { status: value, notify: false })}>
      {(apply) => <>
        {asks && <>
          <label className="field-inline status-pick-tell"><input type="checkbox" checked={tell} onChange={(event) => setTell(event.target.checked)} /><span>{messages.tellCustomer}</span></label>
          <small className="status-pick-which">{!tell ? messages.tellUnticked : notices === undefined ? messages.tellUnknown(channel)
            : told.length === 0 ? messages.tellNone(channel) : messages.tellWhich(channel, told.map((value) => messages.statusChange[value]), quiet.map((value) => messages.statusChange[value]))}</small>
        </>}
        {(['open', 'pending', 'resolved', 'closed'] as const).filter((value) => value !== status)
          .map((value) => <button type="button" key={value} onClick={() => apply(value)}><StatusLabel status={value} /></button>)}
      </>}
    </ActionMenu></div>;
}

export function DetailsPanel({ conversation, agents, tags, onUpdate, onAddTag, onRemoveTag, onCreateTag, timezone = 'UTC', onClose, onShowContactConversations, onShowOrganizationConversations, aiTriage = false, onTriageApplied, currentAgentId, canManage = false, onTriageWaiting, autofix = false }: DetailsPanelProps) {
  const holds = useHolds();
  const readsTeam = holds('team:read');
  const readsTags = holds('tags:read');
  const [tagName, setTagName] = useState('');
  const edits = useEdits(conversation);
  // A select cannot wrap, so a long value is cut on screen; its title holds the full text.
  const priority = edits.shown('priority', conversation?.priority ?? 'normal');
  const assignee = edits.shown('assignee', conversation?.assignee_id ?? '');
  const holder = agents.find((agent) => agent.id === assignee);
  const assigneeName = !assignee ? messages.unassigned : assignee === currentAgentId ? messages.assignToMe
    : holder ? holder.display_name || holder.email : messages.anotherTeammate;
  const update = (field: Field, key: string, value: unknown, changes: UpdateConversationRequest) =>
    conversation && onUpdate && edits.save(field, key, value, () => onUpdate(conversation.id, changes));
  const createTag = () => {
    if (!conversation || !tagName.trim()) return;
    onCreateTag?.(conversation.id, tagName.trim());
    setTagName('');
  };
  return (
    <aside id="conversation-details" className="details-panel panel" aria-label={messages.conversationDetails}
      onKeyDown={(event) => { if (event.key === 'Escape' && onClose && !(event.target instanceof HTMLSelectElement)) onClose(); }}>
      {onClose && <button type="button" className="drawer-close" onClick={onClose} autoFocus>{messages.hideDetails}</button>}
      {autofix && conversation && <AutofixDetails conversationId={conversation.id} customerName={conversation.contact_name} timezone={timezone} />}
      {aiTriage && conversation && <AITriageSlot conversationId={conversation.id} tags={tags} agents={agents} onApplied={() => onTriageApplied?.()}
        currentAgentId={currentAgentId} timezone={timezone} canManage={canManage} onWaiting={onTriageWaiting} />}
      <section className="details-section">
        <h2 className="sr-only">{messages.properties}</h2>
        <StatusPick key={conversation?.id} conversation={conversation} status={edits.shown('status', conversation?.status ?? 'open')}
          onUpdate={onUpdate && ((_id, changes) => update('status', 'status', changes.status, changes))} />
        {edits.error('status')}
        <label className="property-row"><span>{messages.priority}</span><select value={priority} title={messages.priorities[priority]} disabled={!conversation}
          onChange={(event) => { const priority = event.target.value as UpdateConversationRequest['priority']; update('priority', 'priority', priority, { priority }); }}>
          {Object.entries(messages.priorities).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select></label>
        {edits.error('priority')}
        <label className="property-row"><span>{messages.assignee}</span><select value={assignee} title={assigneeName} disabled={!conversation}
          onChange={(event) => update('assignee', 'assignee', event.target.value, { assignee_id: event.target.value || null })}>
          {/* The signed-in agent is Me, first; teammates follow and Unassigned comes last, as in the Filter menu. */}
          {currentAgentId && <option value={currentAgentId}>{messages.assignToMe}</option>}
          {agents.filter((agent) => agent.id !== currentAgentId && (!agent.deactivated_at || agent.id === conversation?.assignee_id)).map((agent) =>
            <option key={agent.id} value={agent.id}>{agent.display_name || agent.email}{agent.deactivated_at ? ` ${messages.deactivatedSuffix}` : ''}</option>)}
          {/* Without the team list (team:read) a teammate's name is unknown here; the choice still shows who holds it. */}
          {!readsTeam && conversation?.assignee_id && conversation.assignee_id !== currentAgentId && <option value={conversation.assignee_id}>{messages.anotherTeammate}</option>}
          <option value="">{messages.unassigned}</option>
        </select></label>
        {edits.error('assignee')}
        <div className="property-row tags-row"><span>{messages.tags}</span>
          {/* Without the tag list (tags:read) the conversation's own tags are named, and nothing else is offered. */}
          {!readsTags ? <span className="tag-names">{tagNames(conversation)}</span> : <PropertyMenu className="tags-pick" label={messages.tags} disabled={!conversation} summary={<span title={tagNames(conversation)}>{tagNames(conversation)}</span>}>
            {tags.length === 0 && <p className="muted-copy">{messages.noTagsYet}</p>}
            {tags.map((tag) => {
              const checked = edits.shown(`tag:${tag.id}`, conversation?.tags.some((item) => item.id === tag.id) ?? false);
              const change = checked ? onRemoveTag : onAddTag;
              return <label className="tag-check" key={tag.id}><input type="checkbox" checked={checked}
                onChange={() => conversation && change && edits.save('tags', `tag:${tag.id}`, !checked, () => change(conversation.id, tag.id))} />{tag.name}</label>;
            })}
            {holds('tags:create') && <form className="tag-create" onSubmit={(event) => { event.preventDefault(); createTag(); }}>
              <input aria-label={messages.newTagName} placeholder={messages.newTagName} value={tagName} onChange={(event) => setTagName(event.target.value)} />
              <button type="submit" disabled={!conversation || !tagName.trim()}>{messages.createTag}</button>
            </form>}
          </PropertyMenu>}
        </div>
        {edits.error('tags')}
        {conversation && slaOf(conversation) && <DueRow sla={slaOf(conversation)!} timezone={timezone} canManage={canManage} />}
      </section>
      {conversation && (
        <section className="details-section conversation-source" aria-labelledby="details-customer">
          <h2 id="details-customer">{messages.customer}</h2>
          <strong className="customer-name">{conversation.contact_name}</strong>
          {conversation.contact_organization && <div className="property-row conversation-organization"><span>{messages.organization}</span>
            {onShowOrganizationConversations
              ? <button type="button" className="link-button" title={messages.showOrganizationConversations(conversation.contact_organization)}
                onClick={() => onShowOrganizationConversations(conversation.contact_organization ?? '')}>{conversation.contact_organization}</button>
              : <span>{conversation.contact_organization}</span>}</div>}
          {onShowContactConversations && <button type="button" className="details-link" title={messages.showContactConversations(conversation.contact_name)} onClick={() => onShowContactConversations(conversation.contact_id)}>
            <UserIcon /><span>{messages.showContactConversations(conversation.contact_name)}</span><ChevronRightIcon /></button>}
        </section>
      )}
      {conversation?.origin && <ConversationOriginBlock origin={conversation.origin} timezone={timezone} />}
    </aside>
  );
}

// The Tags row names the conversation's tags on one line, as the design writes them ("billing · refund").
const tagNames = (conversation?: Conversation) => conversation?.tags.map((tag) => tag.name).join(' · ') || messages.none;

// PropertyMenu is a property's value that opens a popover in place: ticking in it keeps it open, so several
// tags change in one visit. Escape or focus leaving closes it.
function PropertyMenu({ className, label, summary, disabled, children }: { className: string; label: string; summary: ReactNode; disabled?: boolean; children: ReactNode }) {
  const menu = useRef<HTMLDetailsElement>(null);
  const close = () => { if (menu.current) menu.current.open = false; };
  return <details ref={menu} className={`action-menu property-menu ${className}`} data-disabled={disabled || undefined}
    onKeyDown={(event) => { if (event.key === 'Escape' && menu.current?.open) { event.stopPropagation(); close(); menu.current.querySelector('summary')?.focus(); } }}
    onBlur={(event) => {
      if (menu.current?.contains(event.relatedTarget as Node | null)) return;
      // Firefox blurs a button the moment it becomes disabled (Create tag after its name is cleared); focus
      // has not left the menu, so it stays open and focus returns to its summary.
      if (event.relatedTarget === null && (event.target as HTMLElement).matches(':disabled')) { menu.current?.querySelector('summary')?.focus(); return; }
      close();
    }}>
    <summary aria-disabled={disabled} onClick={(event) => { if (disabled) event.preventDefault(); }}><span>{summary}</span><ChevronDownIcon /></summary>
    <div className="action-popover" role="group" aria-label={label}>{children}</div>
  </details>;
}

// The Due row: when the reply is due, or how late, as the list row says it; its menu holds each clock in full.
function DueRow({ sla, timezone, canManage }: { sla: NonNullable<ReturnType<typeof slaOf>>; timezone: string; canManage: boolean }) {
  const due = rowDue(sla, timezone, useMinuteNow());
  const first = clockLine(sla.first_response, timezone);
  const text = due?.text ?? first.text;
  const tone = due ? (due.late ? 'late' : undefined) : first.tone;
  return <div className="property-row due-row"><span>{messages.dueAt}</span>
    <PropertyMenu className="due-pick" label={messages.targets} summary={<span className={tone ? `target-${tone}` : undefined} title={due?.full ?? text}>{text}</span>}>
      <Targets sla={sla} timezone={timezone} canManage={canManage} />
    </PropertyMenu></div>;
}

function ConversationOriginBlock({ origin, timezone }: { origin: ConversationOrigin; timezone: string }) {
  const system = messages.systemNames[origin.system] ?? origin.system;
  return (
    <section className="details-section conversation-origin">
      <span>{messages.importedTicket(system, origin.external_id)}</span>
      {origin.type && <span>{messages.ticketType(origin.type)}</span>}
      {origin.due_at && <span>{messages.dueAt} <time dateTime={origin.due_at}>{formatDate(origin.due_at, timezone)}</time></span>}
    </section>
  );
}

// Targets: each clock in full, read only. The server counts business hours; the details say what it counted.
function Targets({ sla, timezone, canManage }: { sla: NonNullable<ReturnType<typeof slaOf>>; timezone: string; canManage: boolean }) {
  const rows: [string, ReturnType<typeof clockLine>][] = [[messages.targetFirstReply, clockLine(sla.first_response, timezone)], [messages.targetResolve, clockLine(sla.resolution, timezone)]];
  return <section className="details-section" aria-labelledby="details-targets">
    <h2 id="details-targets">{messages.targets}</h2>
    {rows.map(([label, line]) => <div className="target-row" key={label}><span>{label}</span>
      <span className={`target-value${line.tone ? ` target-${line.tone}` : ''}`}><span>{line.tone === 'met' && <CheckIcon />}{line.text}</span>{line.detail && <small>{line.detail}</small>}</span></div>)}
    <p className="targets-foot">{messages.targetsFoot}{canManage && <> <a href="/settings/targets">{messages.editTargets}</a></>}</p>
  </section>;
}
