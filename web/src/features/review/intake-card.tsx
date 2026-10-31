import { useState } from 'react';
import type { FormEvent } from 'react';
import type { ChatItem, IntakeDecideInput, IntakeGathering, IntakeGatheringDetail, IntakeIgnoreReason } from '../../api/intake';
import { formatDate } from '../../lib/format-date';
import { PlainTextBody } from '../inbox/plain-text-body';
import { Choice } from '../../ui/choice';
import { AISuggestion } from './ai-suggestion';
import { CandidatePicker } from './candidate-picker';
import { reviewMessages as m } from './review-messages';

type Action = IntakeDecideInput['action'];

interface IntakeCardProps {
  item: IntakeGathering;
  // detail is the card with its messages, the conversations it may join and the channel's commands,
  // loaded once the card is open.
  detail?: IntakeGatheringDetail;
  detailFailed?: boolean;
  onRetryDetail?: () => void;
  open: boolean;
  onToggle: () => void;
  timezone: string;
  channelName: (id: string) => string;
  agentName: (id: string | null) => string;
  // Each resolves once the server saved it (or someone decided the card first; the page says so) and rejects
  // when it was not saved.
  onDecide: (input: IntakeDecideInput) => Promise<unknown>;
  onRequeue: () => Promise<unknown>;
}

const ignoreReasons: IntakeIgnoreReason[] = ['chat', 'handled_in_chat', 'duplicate', 'other'];

const placeName = (item: IntakeGathering) => (item.place?.kind === 'group' ? m.groupPlace(item.place.name) : item.place?.kind === 'direct' ? m.directPlace : undefined);

// subjectOf is the first line the customer wrote, without a command they typed in front of it.
function subjectOf(items: ChatItem[], commands: string[]) {
  const first = items.find((entry) => entry.role === 'customer' && entry.body.trim() !== '')?.body ?? '';
  let line = first.split('\n').find((text) => text.trim() !== '')?.trim() ?? '';
  const command = commands.find((name) => line.toLowerCase() === name.toLowerCase() || line.toLowerCase().startsWith(`${name.toLowerCase()} `));
  if (command) line = line.slice(command.length).trim();
  return line.slice(0, 200);
}

function whoDecided(item: IntakeGathering, agentName: (id: string | null) => string) {
  if (item.decided_by === 'rules') return item.rules.basis === 'match' ? m.rulesMatch : m.rulesDefault;
  if (item.decided_by === 'ai') return m.ai;
  return agentName(item.decided_by_agent_id);
}

// IntakeCard is one gathering of the intake queue: what one customer wrote in one go on a chat channel.
// While it waits it offers the three decisions (docs/user/review.md); once decided it says what
// happened and who decided, with Put back for a card set aside.
export function IntakeCard({ item, detail, detailFailed, onRetryDetail, open, onToggle, timezone, channelName, agentName, onDecide, onRequeue }: IntakeCardProps) {
  const headingId = `intake-${item.id}`;
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setProblem(undefined);
    try {
      await action();
    } catch {
      setProblem(m.failedAction);
    } finally {
      setBusy(false);
    }
  };
  const waiting = item.state === 'review';
  const where = [placeName(item), channelName(item.channel_id)].filter(Boolean).join(' · ');
  // A card a person made by taking messages back out of a conversation (docs/user/inbox.md) says so.
  const takenBack = item.rules.rule === 'detached';
  const reason = takenBack ? (item.requeued_by_agent_id ? m.takenBackBy(agentName(item.requeued_by_agent_id)) : m.takenBack)
    : item.review_reason === 'requeued' && item.requeued_by_agent_id ? m.requeuedBy(agentName(item.requeued_by_agent_id))
      : item.review_reason ? m.reason[item.review_reason] : undefined;
  const settled = !waiting && item.state !== 'deciding';
  return (
    <article className="intake-card" aria-labelledby={headingId} data-state={item.state}>
      <header className="intake-head">
        <div>
          <h3 id={headingId}>{item.sender.name ?? m.someone}</h3>
          <p className="muted-copy">{where}</p>
        </div>
        {waiting && reason && <span className="badge badge-intake" data-reason={item.review_reason}>{reason}</span>}
        {settled && <span className="badge badge-intake-done">{item.state === 'ignored' && item.ignore_reason ? m.setAsideWhy(m.ignoreReasons[item.ignore_reason]) : m.outcome[item.state]}</span>}
      </header>
      {/* A password in the messages: set aside for it, or AI held its suggestion back for it whatever else the card waits for. */}
      {(item.review_reason === 'credential' || item.suggestion?.not_applied_reason === 'credential') && waiting
        && <p className="notice notice-danger" role="note"><span>{m.credentialWarning}</span></p>}
      {item.review_reason === 'lane_blocked' && waiting && <p className="muted-copy">{m.laneBlocked}</p>}
      {!open && <p className="intake-preview">{item.preview}</p>}
      <p className="intake-meta muted-copy">
        <time dateTime={item.opened_at}>{formatDate(item.opened_at, timezone)}</time>
        <span>{m.messagesCount(item.item_count)}</span>
        <button type="button" className="intake-toggle" aria-expanded={open} onClick={onToggle}>{open ? m.hideMessages : waiting ? m.decide : m.showMessages}</button>
      </p>
      {/* Filed by the rules while AI only suggests: the suggestion shows, but the card is decided (move messages to change it). */}
      {(waiting || item.state === 'applied') && item.suggestion && <AISuggestion suggestion={item.suggestion} busy={busy}
        onFollow={waiting ? (input) => void run(() => onDecide(input)) : undefined} />}
      {settled && <dl className="intake-decided">
        <dt>{m.decidedBy}</dt><dd><span>{whoDecided(item, agentName)}</span>{item.decided_at && <> · <time dateTime={item.decided_at}>{formatDate(item.decided_at, timezone)}</time></>}</dd>
        {item.fallback && <dd className="muted-copy">{m.fallback}</dd>}
      </dl>}
      {open && detailFailed && <p role="alert" className="error-copy">{m.detailFailed} <button type="button" className="link-action" onClick={onRetryDetail}>{m.tryAgain}</button></p>}
      {open && !detail && !detailFailed && <p role="status" className="muted-copy">{m.loading}</p>}
      {open && detail && <ol className="intake-items">
        {detail.items.map((entry) => <li key={entry.id} className="intake-item" data-role={entry.role}>
          <p className="intake-item-head"><strong>{entry.sender.name ?? m.someone}</strong> <time dateTime={entry.occurred_at}>{formatDate(entry.occurred_at, timezone)}</time></p>
          {entry.body && <div className="intake-item-body"><PlainTextBody text={entry.body} keepBreaks /></div>}
          {entry.files.length > 0 && <ul className="intake-files">{entry.files.map((file, index) => <li key={`${file.name}-${index}`}>{file.name}</li>)}</ul>}
        </li>)}
      </ol>}
      {open && detail && waiting && <DecideForm key={detail.id} item={item} detail={detail} timezone={timezone} busy={busy} onDecide={(input) => void run(() => onDecide(input))} />}
      {settled && item.conversation_id && <a href={`/conversations/${item.conversation_id}`}>{m.openConversation}</a>}
      {item.state === 'ignored' && <div className="request-actions"><button type="button" disabled={busy} onClick={() => void run(onRequeue)}>{m.putBack}</button></div>}
      {problem && <p role="alert" className="error-copy">{problem}</p>}
    </article>
  );
}

function DecideForm({ item, detail, timezone, busy, onDecide }: { item: IntakeGathering; detail: IntakeGatheringDetail; timezone: string; busy: boolean;
  onDecide: (input: IntakeDecideInput) => void }) {
  // The form starts from what AI suggested, else from what the rules would have done.
  const suggested = item.suggestion;
  const offered = (id: string | null | undefined) => detail.candidates.find((entry) => entry.conversation_id === id)?.conversation_id;
  const target = (suggested?.action === 'attach' ? offered(suggested.target_conversation_id) : undefined) ?? offered(item.rules.target_conversation_id);
  const [action, setAction] = useState<Action>(suggested && (suggested.action !== 'attach' || target) ? suggested.action : item.rules.action === 'attach' && target ? 'attach' : 'create');
  const [subject, setSubject] = useState(() => suggested?.subject ?? subjectOf(detail.items, detail.commands));
  const [command, setCommand] = useState(suggested?.command && detail.commands.includes(suggested.command) ? suggested.command : '');
  const [conversation, setConversation] = useState(target ?? detail.candidates[0]?.conversation_id);
  const [why, setWhy] = useState<IntakeIgnoreReason>(suggested?.ignore_reason ?? 'chat');
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (action === 'create') onDecide({ action, subject: subject.trim(), ...(command ? { command } : {}) });
    else if (action === 'attach' && conversation) onDecide({ action, conversation_id: conversation });
    else if (action === 'ignore') onDecide({ action, ignore_reason: why });
  };
  const ready = action === 'create' ? subject.trim() !== '' : action === 'attach' ? Boolean(conversation) : true;
  return <form className="intake-form" onSubmit={submit}>
    <Choice legend={m.action} hideLegend value={action} onChange={setAction}
      items={[{ value: 'create', label: m.create }, { value: 'attach', label: m.attach }, { value: 'ignore', label: m.ignore }]} />
    {action === 'create' && <div className="intake-fields">
      <label className="field"><span>{m.subject}</span><input value={subject} maxLength={200} required onChange={(event) => setSubject(event.target.value)} /></label>
      {detail.commands.length > 0 && <label className="field"><span>{m.command}</span>
        <select value={command} onChange={(event) => setCommand(event.target.value)}>
          <option value="">{m.noCommand}</option>
          {detail.commands.map((name) => <option key={name} value={name}>{name}</option>)}
        </select></label>}
    </div>}
    {action === 'attach' && <CandidatePicker candidates={detail.candidates} value={conversation} onChange={setConversation} timezone={timezone} />}
    {action === 'ignore' && <Choice legend={m.ignoreReason} value={why} onChange={setWhy} items={ignoreReasons.map((value) => ({ value, label: m.ignoreReasons[value] }))} />}
    <div className="request-actions">
      <button type="submit" className="primary-action" disabled={busy || !ready}>
        {action === 'create' ? m.createButton : action === 'attach' ? m.attachButton : m.ignoreButton}</button>
    </div>
  </form>;
}
