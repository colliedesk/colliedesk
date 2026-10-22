import { useEffect, useRef, useState } from 'react';
import type { ConversationViewer, Message } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { Avatar } from '../../ui/avatar';

// Who else has a conversation open: the marks on the inbox list, the lines under the open conversation's
// title, and the announces of this tab. A part of its own (presenceViewPart), loaded once a conversation is
// open or somebody else is there, so the first bundle carries none of it.

export { startPresence } from './presence-runtime';

const text = {
  writing: (name: string) => `${name} is writing a reply`,
  leftOpen: (name: string, since: string) => `${name} has a reply not sent yet, untouched since ${since}. Ask them before replying.`,
  viewing: (name: string) => `${name} is viewing`,
  via: (name: string, labels: string[]) => `${name} via ${labels.join(' and ')}`,
  openNow: (names: string[]) => `Open now: ${names.join(', ')}`,
  writingShort: (name: string) => `${name} (writing a reply)`,
  leftOpenShort: (name: string) => `${name} (reply not sent yet)`,
  replying: 'Replying',
  draftOpen: 'Draft open',
  more: (count: number) => `Show ${count} more`,
  conflictTitle: (count: number) => `Someone replied while you were writing (${count === 1 ? '1 new reply' : `${count} new replies`}). Nothing was sent; your text is still here.`,
  newReplies: 'New replies',
  teammate: 'A teammate',
  imported: (system: string) => `Imported from ${systemName(system)}`,
  importedTitle: (count: number, system: string) => `${count === 1 ? '1 reply' : `${count} replies`} imported from ${systemName(system)} arrived after you started writing; ${count === 1 ? 'it was' : 'they were'} written earlier, at the time shown. Nothing was sent; your text is still here.`,
  sendAnyway: 'Send anyway',
  editReply: 'Edit reply',
  fewer: 'Show fewer',
};

// The helpdesks an import comes from, by the name they go by.
const systems: Record<string, string> = { freshdesk: 'Freshdesk', helpscout: 'Help Scout' };
const systemName = (system: string) => systems[system] ?? system.charAt(0).toUpperCase() + system.slice(1);

// A reply nobody changed for this long is a draft left open in a tab, not someone typing now.
const typingWithin = 2 * 60_000;
const shownLines = 3;
const shownFaces = 3;

type State = 'typing' | 'left' | 'viewing';

// One person, or the AI agents working as that person: their tabs and tokens are one entry.
interface Entry { key: string; name: string; agent: boolean; labels: string[]; state: State; editedAt?: string }

function entries(viewers: ConversationViewer[], now: number): Entry[] {
  const grouped = new Map<string, Entry>();
  for (const viewer of viewers) {
    const agent = viewer.agent_token !== null;
    const key = `${viewer.agent_id}:${agent ? 'agent' : 'person'}`;
    const entry = grouped.get(key) ?? { key, name: viewer.name, agent, labels: [], state: 'viewing' as State };
    if (viewer.agent_token) entry.labels.push(viewer.agent_token.label);
    if (viewer.writing && viewer.edited_at && (!entry.editedAt || viewer.edited_at > entry.editedAt)) entry.editedAt = viewer.edited_at;
    grouped.set(key, entry);
  }
  for (const entry of grouped.values()) {
    if (entry.editedAt) entry.state = entry.agent || now - Date.parse(entry.editedAt) <= typingWithin ? 'typing' : 'left';
  }
  // A reply being written comes first: it is what decides whether to answer now.
  const order: Record<State, number> = { typing: 0, left: 1, viewing: 2 };
  return [...grouped.values()].sort((a, b) => order[a.state] - order[b.state]);
}

const nameOf = (entry: Entry) => (entry.agent ? text.via(entry.name, entry.labels) : entry.name);

function sentence(entry: Entry, timezone: string): string {
  const name = nameOf(entry);
  if (entry.state === 'typing') return text.writing(name);
  if (entry.state === 'left') return text.leftOpen(name, formatDate(entry.editedAt!, timezone));
  return text.viewing(name);
}

const Face = ({ entry }: { entry: Entry }) => <Avatar name={entry.name} size="xs" agent={entry.agent} />;

// ViewerStack marks a row of the inbox list: the initials of the others (an AI agent as its own shape,
// once per person), Replying while one of them types a reply, Draft open for a reply left untouched. The
// row's accessible name carries the same in words; the tip lists one entry per line.
export function ViewerStack({ viewers, timezone }: { viewers: ConversationViewer[]; timezone: string }) {
  const list = entries(viewers, Date.now());
  if (list.length === 0) return null;
  const words = list.map((entry) => (entry.state === 'typing' ? text.writingShort(nameOf(entry)) : entry.state === 'left' ? text.leftOpenShort(nameOf(entry)) : nameOf(entry)));
  return <>
    <span className="viewer-stack" data-tip={list.map((entry) => sentence(entry, timezone)).join('\n')} aria-hidden="true">
      {list.slice(0, shownFaces).map((entry) => <Face key={entry.key} entry={entry} />)}
      {list.length > shownFaces && <span className="viewer-more">+{list.length - shownFaces}</span>}
    </span>
    {list.some((entry) => entry.state === 'typing') ? <span className="badge badge-replying" aria-hidden="true">{text.replying}</span>
      : list.some((entry) => entry.state === 'left') && <span className="badge badge-draft-open" aria-hidden="true">{text.draftOpen}</span>}
    <span className="sr-only">{text.openNow(words)}</span>
  </>;
}

// ConversationViewers names the others who have the open conversation in front of them, one per line,
// so nobody answers the customer over a teammate's reply. A screen reader hears what changed (a new line),
// not the whole list again at every heartbeat.
export function ConversationViewers({ viewers, timezone }: { viewers: ConversationViewer[]; timezone: string }) {
  const lines = entries(viewers, Date.now()).map((entry) => ({ entry, words: sentence(entry, timezone) }));
  const said = lines.map((line) => line.words).join('\n');
  const previous = useRef<string[]>([]);
  const [announcement, setAnnouncement] = useState('');
  useEffect(() => {
    const now = said ? said.split('\n') : [];
    const added = now.filter((line) => !previous.current.includes(line));
    previous.current = now;
    if (added.length > 0) setAnnouncement(added.join('. '));
  }, [said]);
  // Past three lines the rest waits behind a button, which works the same with a pointer, a finger or a key.
  const [expanded, setExpanded] = useState(false);
  const hidden = expanded ? [] : lines.slice(shownLines);
  return (
    <div className="conversation-viewers">
      <span className="sr-only" aria-live="polite">{announcement}</span>
      {lines.length > 0 && <ul>
        {lines.slice(0, expanded ? lines.length : shownLines).map(({ entry, words }) => <li key={entry.key} data-state={entry.state}><Face entry={entry} /><span>{words}</span></li>)}
      </ul>}
      {(hidden.length > 0 || (expanded && lines.length > shownLines)) && <button type="button" className="link-action viewer-more" aria-expanded={expanded}
        onClick={() => setExpanded((open) => !open)}>{expanded ? text.fewer : text.more(hidden.length)}</button>}
    </div>
  );
}

// ReplyConflict stands above the reply box when a reply was refused because someone replied after its
// writer started: their replies by time (an imported one keeps its own time and says where it came
// from), then Send anyway or Edit reply. Both count those replies as read, so they are asked about once.
export function ReplyConflict({ replies, timezone, onSendAnyway, onEdit }: { replies: Message[]; timezone: string; onSendAnyway: () => void; onEdit: () => void }) {
  // On a phone the timeline sits above the reply box and the new replies push it down: the warning is
  // brought on screen and its safe choice takes the focus, so the refusal never goes unseen.
  const panel = useRef<HTMLElement>(null);
  const edit = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    panel.current?.scrollIntoView?.({ block: 'nearest' });
    edit.current?.focus({ preventScroll: true });
  }, []);
  return (
    <section className="reply-conflict" aria-label={text.newReplies} ref={panel}>
      {/* Only the fact is read out at once; the replies are there to read, not to be read aloud whole. */}
      {/* Replies a re-run import added are not someone answering now: the title says so when they are all there is. */}
      <p role="alert" className="reply-conflict-title">{replies.length > 0 && replies.every((reply) => reply.origin)
        ? text.importedTitle(replies.length, replies[0].origin!.system) : text.conflictTitle(replies.length)}</p>
      <ul tabIndex={0} aria-label={text.newReplies}>
        {replies.map((reply) => <li key={reply.id}>
          <p className="reply-conflict-by">{reply.author_name ?? text.teammate} · <time dateTime={reply.created_at}>{formatDate(reply.created_at, timezone)}</time>
            {reply.origin && <> · {text.imported(reply.origin.system)}</>}</p>
          <p className="reply-conflict-text">{reply.body}</p>
        </li>)}
      </ul>
      {/* Editing is the safe default; sending anyway answers the customer a second time. */}
      <div className="reply-conflict-actions">
        <button type="button" className="primary-action" ref={edit} onClick={onEdit}>{text.editReply}</button>
        <button type="button" className="send-anyway" onClick={onSendAnyway}>{text.sendAnyway}</button>
      </div>
    </section>
  );
}
