import { useEffect, useId, useState } from 'react';
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import type { QueryClient } from '@tanstack/react-query';
import { api, ConflictError, InvalidRequestError } from '../../api/client';
import { useCreateAction } from '../../api/use-create';
import type { Conversation, Message } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { apiQuery } from '../../lib/inbox-query';
import { Dialog, Field } from '../../ui/dialog';
import { RotateCcwIcon } from '../../ui/icons';
import { ActionMenu } from './action-menu';
import { ConversationPicker } from './conversation-picker';
import { messageMovesApi } from './message-moves-api';
import type { MessageMove, MessageMoveResult, MoveInput } from './message-moves-api';
import { moveMessages as m } from './message-moves-messages';
import './message-moves.css';

// Moving customer messages out of a conversation (docs/user/inbox.md "Move messages to another
// conversation"): a menu on each customer message, the dialog that moves or splits them, and the moves log at
// the top of the timeline, where each move can be undone. A lazy part: the inbox loads it once a conversation
// is open.

type Mode = 'move' | 'split';

const movesKey = (conversationID: string) => ['message-moves', conversationID];
const intakeKey = (conversationID: string) => ['conversation-intake', conversationID];
// The warnings of a move made on this screen, kept for its line in the log.
const warningsKey = (moveID: string) => ['message-move-warnings', moveID];

function useMoves(conversationID: string) {
  return useQuery({ queryKey: movesKey(conversationID), queryFn: () => messageMovesApi.list(conversationID) });
}

function useIntake(conversationID: string) {
  return useQuery({ queryKey: intakeKey(conversationID), queryFn: () => messageMovesApi.intake(conversationID) });
}

// A move changes both conversations' timelines, the list, the conversations' moves and the intake queue.
function refreshAfterMove(client: QueryClient) {
  for (const key of ['messages', 'conversations', 'message-moves', 'conversation-intake', 'intake', 'intake-card', 'intake-summary', 'intake-stats']) {
    void client.invalidateQueries({ queryKey: [key] });
  }
}

const refusal = (error: unknown, fallback: string) => (error instanceof InvalidRequestError ? error.message : fallback);

const firstLine = (text: string) => text.split('\n').map((line) => line.trim()).find(Boolean)?.slice(0, 200) ?? '';

interface ActionsProps {
  conversation: Conversation;
  message: Message;
  // The conversation's timeline, to offer its other customer messages with this one.
  timeline: Message[];
  timezone: string;
}

// MessageIntakeLabel says why a chat message is in this conversation: the channel's rules (and which one),
// a person in Review, or AI. Email and web form messages say nothing: every one of them is the rules'.
export function MessageIntakeLabel({ conversationID, messageID }: { conversationID: string; messageID: string }) {
  const intake = useIntake(conversationID).data?.messages.find((entry) => entry.message_id === messageID);
  return intake?.gathering_id ? <span className="message-kind message-intake">{addedLabel(intake.decision_basis, intake.decision_rule)}</span> : null;
}

// useMoveAction sends a move under the key of its press: a press sent again (a double click, a retry after a
// lost answer) answers the first move and moves nothing more.
function useMoveAction() {
  return useCreateAction(({ conversationID, input }: { conversationID: string; input: MoveInput }, key: string) => messageMovesApi.move(conversationID, input, key));
}

// MessageMoveMenu is the menu on a customer message that moves it: to another conversation, into a new one,
// and for a chat message, back to Review.
export function MessageMoveMenu({ conversation, message, timeline, timezone }: ActionsProps) {
  const client = useQueryClient();
  const intake = useIntake(conversation.id).data?.messages.find((entry) => entry.message_id === message.id);
  const move = useMoveAction();
  const [mode, setMode] = useState<Mode>();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const chat = Boolean(intake?.gathering_id);
  const toReview = async () => {
    setBusy(true);
    setProblem(undefined);
    try {
      const result = await move.run({ conversationID: conversation.id, input: { message_ids: [message.id], to: { queue: true } } });
      client.setQueryData(warningsKey(result.move.id), result.warnings);
      refreshAfterMove(client);
    } catch (error) {
      setProblem(refusal(error, m.failed));
    } finally {
      setBusy(false);
    }
  };
  return <>
    <span className="message-move-menu">
      <ActionMenu<'move' | 'split' | 'review'> label={m.menu} icon={null} disabled={busy}
        onApply={(choice) => (choice === 'review' ? void toReview() : setMode(choice))}>
        {(apply) => <>
          <button type="button" onClick={() => apply('move')}>{m.moveTo}</button>
          <button type="button" onClick={() => apply('split')}>{m.split}</button>
          {chat && <button type="button" onClick={() => apply('review')}>{m.toReview}</button>}
        </>}
      </ActionMenu>
    </span>
    {problem && <p role="alert" className="error-copy message-move-problem">{problem}</p>}
    {mode && <MoveDialog mode={mode} conversation={conversation} timeline={timeline} picked={[message.id]} timezone={timezone} onClose={() => setMode(undefined)} />}
  </>;
}

function addedLabel(basis: string | null, rule: string | null) {
  if (basis === 'model') return m.addedByAI;
  if (basis === 'person') return m.addedByPerson;
  return (rule && m.added[rule]) || m.addedByRules;
}

interface DialogProps {
  mode: Mode;
  conversation: Conversation;
  timeline: Message[];
  picked: string[];
  // Closing the conversation as no new request starts checked when every customer message is picked
  // from "Not a new request?".
  closeSource?: boolean;
  timezone: string;
  onClose: () => void;
}

// MoveDialog moves the picked customer messages to another conversation (searched, this customer's others
// first) or splits them into a new one.
export function MoveDialog({ mode, conversation, timeline, picked, closeSource = false, timezone, onClose }: DialogProps) {
  const client = useQueryClient();
  const formID = useId();
  const incoming = timeline.filter((item) => item.kind === 'incoming');
  const [ids, setIDs] = useState<string[]>(picked);
  const [search, setSearch] = useState('');
  const [typed, setTyped] = useState('');
  const [target, setTarget] = useState<string>();
  const [subject, setSubject] = useState(() => firstLine(incoming.find((item) => picked.includes(item.id))?.body ?? ''));
  const [close, setClose] = useState(closeSource);
  const move = useMoveAction();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  // The search goes out once typing pauses.
  useEffect(() => {
    const timer = setTimeout(() => setSearch(typed.trim()), 250);
    return () => clearTimeout(timer);
  }, [typed]);
  const targets = useQuery({
    queryKey: ['move-targets', conversation.contact_id, search], enabled: mode === 'move',
    queryFn: () => api.conversations(apiQuery(search ? { search, status: 'all' } : { contact: conversation.contact_id, status: 'all' }, 1)),
  });
  const options = (targets.data?.items ?? []).filter((item) => item.id !== conversation.id).slice(0, 20);
  const all = ids.length === incoming.length;
  const submit = async () => {
    if (ids.length === 0) return setProblem(m.pickOne);
    if (mode === 'move' && !target) return setProblem(m.pickTarget);
    const input: MoveInput = mode === 'split' ? { message_ids: ids, to: { new: { subject: subject.trim() } } }
      : { message_ids: ids, to: { conversation_id: target ?? '' }, ...(close && all ? { close_source: true } : {}) };
    setBusy(true);
    setProblem(undefined);
    try {
      const result: MessageMoveResult = await move.run({ conversationID: conversation.id, input });
      client.setQueryData(warningsKey(result.move.id), result.warnings);
      refreshAfterMove(client);
      onClose();
    } catch (error) {
      setProblem(refusal(error, m.failed));
      setBusy(false);
    }
  };
  const toggle = (id: string) => setIDs((current) => (current.includes(id) ? current.filter((item) => item !== id) : [...current, id]));
  return <Dialog title={mode === 'move' ? m.moveTitle : m.splitTitle} help={m.moveHelp} onClose={onClose} closeLabel={m.close} className="move-dialog"
    onSubmit={() => void submit()} footer={<>
      <button type="button" onClick={onClose}>{m.cancel}</button>
      <button type="submit" className="primary-action" disabled={busy || (mode === 'split' && subject.trim() === '')}>{mode === 'move' ? m.moveButton : m.splitButton}</button>
    </>}>
    <fieldset className="move-messages">
      <legend>{m.which}</legend>
      {incoming.map((item) => <label key={item.id} className="move-message">
        <input type="checkbox" checked={ids.includes(item.id)} onChange={() => toggle(item.id)} />
        <span><span className="move-message-text">{firstLine(item.body) || item.attachments[0]?.filename || '—'}</span>
          <time className="muted-copy" dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time></span>
      </label>)}
    </fieldset>
    {mode === 'split' && <Field id={`${formID}-subject`} label={m.subject}>
      {(field) => <input {...field} value={subject} maxLength={200} onChange={(event) => setSubject(event.target.value)} />}</Field>}
    {mode === 'move' && <>
      <Field id={`${formID}-search`} label={m.search} help={m.searchHelp}>
        {(field) => <input {...field} type="search" value={typed} onChange={(event) => setTyped(event.target.value)} />}</Field>
      {targets.isPending && <p role="status" className="muted-copy">{m.searching}</p>}
      {targets.isError && <p role="alert" className="error-copy">{m.searchFailed}</p>}
      {targets.data && options.length === 0 && <p className="muted-copy">{m.noTargets}</p>}
      {options.length > 0 && <ConversationPicker label={m.target} value={target} onChange={setTarget} timezone={timezone}
        items={options.map((item) => ({ id: item.id, subject: item.subject, status: item.status, contactName: item.contact_name, updatedAt: item.updated_at,
          note: item.status === 'resolved' || item.status === 'closed' ? m.reopens : undefined }))} />}
      {all && <label className="move-close"><input type="checkbox" checked={close} onChange={(event) => setClose(event.target.checked)} />
        <span>{m.closeSource}<small className="muted-copy">{m.closeSourceHelp}</small></span></label>}
    </>}
    {problem && <p role="alert" className="error-copy">{problem}</p>}
  </Dialog>;
}

interface LogProps {
  conversation: Conversation;
  timeline: Message[];
  timezone: string;
  currentAgentId?: string;
  agentNames?: ReadonlyMap<string, string>;
}

const shown = 3;

// MessageMovesLog is the moves out of and into this conversation, newest first, each with Undo while it can
// be undone; and, on a chat conversation nobody worked on yet, the way to move all of it when it was no new
// request.
export function MessageMovesLog({ conversation, timeline, timezone, currentAgentId, agentNames }: LogProps) {
  const client = useQueryClient();
  // Each Undo is described by its row's sentence, so the buttons of three rows are told apart.
  const rowID = useId();
  const moves = useMoves(conversation.id).data?.items ?? [];
  const intake = useIntake(conversation.id).data;
  const undoMove = useCreateAction((id: string, key: string) => messageMovesApi.undo(id, key));
  const [all, setAll] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [closing, setClosing] = useState(false);
  const visible = all ? moves : moves.slice(0, shown);
  const others = [...new Set(visible.map((move) => (move.from_conversation_id === conversation.id ? move.to_conversation_id : move.from_conversation_id)).filter((id): id is string => Boolean(id)))];
  const subjects = useQueries({ queries: others.map((id) => ({ queryKey: ['conversations', 'one', id], queryFn: () => api.conversation(id) })) });
  const subjectOf = (id: string) => subjects[others.indexOf(id)]?.data?.subject;
  const who = (move: MessageMove, agentID: string | null) => (move.via_token && agentID === move.moved_by_agent_id ? m.anAgent
    : agentID && agentID === currentAgentId ? m.you : (agentID && agentNames?.get(agentID)) || m.aTeammate);
  const undo = async (move: MessageMove) => {
    setProblem(undefined);
    try {
      await undoMove.run(move.id);
    } catch (error) {
      const code = error instanceof ConflictError ? error.detail?.code : undefined;
      setProblem(code === 'move_superseded' ? m.superseded : code === 'move_decided' ? m.decided : refusal(error, m.undoFailed));
    } finally {
      refreshAfterMove(client);
    }
  };
  const incoming = timeline.filter((item) => item.kind === 'incoming');
  const chat = intake?.messages.some((entry) => entry.gathering_id) ?? false;
  const untouched = conversation.status !== 'closed' && conversation.assignee_id === null && timeline.every((item) => item.kind === 'incoming');
  const link = (href: string, text: string) => <a href={href}>{text}</a>;
  if (moves.length === 0 && !(chat && untouched && incoming.length > 0)) return null;
  return <section className="message-moves" aria-label={m.log} aria-live="polite">
    {chat && untouched && incoming.length > 0 && <p className="message-moves-row">
      <button type="button" className="link-action" onClick={() => setClosing(true)}>{m.notANewRequest}</button></p>}
    {closing && <MoveDialog mode="move" conversation={conversation} timeline={timeline} picked={incoming.map((item) => item.id)} closeSource timezone={timezone} onClose={() => setClosing(false)} />}
    {visible.map((move) => {
      const out = move.from_conversation_id === conversation.id;
      const other = out ? move.to_conversation_id : move.from_conversation_id;
      const by = who(move, move.moved_by_agent_id);
      const n = move.message_ids.length;
      const warnings = client.getQueryData<MessageMoveResult['warnings']>(warningsKey(move.id)) ?? [];
      return <div key={move.id} className="message-moves-row" data-undone={move.undone_at ? true : undefined}>
        <RotateCcwIcon />
        <span id={`${rowID}-${move.id}`}>
          {out && move.to_conversation_id === null ? <>{m.queued(n, by)} · {link('/review', m.openReview)}</>
            : <>{out ? (move.created_conversation ? m.splitOut : m.movedOut)(n, by) : m.movedIn(n, by)}{' '}
              {other && link(`/conversations/${other}`, subjectOf(other) ?? m.anotherConversation)}{out && move.closed_source && <> {m.closedSource}</>}</>}
          {' · '}<time dateTime={move.moved_at}>{formatDate(move.moved_at, timezone)}</time>
          {move.undone_at && <> · {m.undone(who(move, move.undone_by_agent_id))}</>}
          {warnings.includes('other_contact') && <small className="muted-copy">{m.otherContact}</small>}
          {warnings.includes('other_channel') && <small className="muted-copy">{m.otherChannel}</small>}
        </span>
        {move.undoable && <button type="button" className="link-action" aria-describedby={`${rowID}-${move.id}`} onClick={() => void undo(move)}>{m.undo}</button>}
      </div>;
    })}
    {moves.length > shown && !all && <button type="button" className="link-action" onClick={() => setAll(true)}>{m.showAll(moves.length)}</button>}
    {problem && <p role="alert" className="error-copy">{problem}</p>}
  </section>;
}
