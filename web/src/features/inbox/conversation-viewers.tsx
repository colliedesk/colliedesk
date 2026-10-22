import type { ConversationViewer } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { Avatar } from '../../ui/avatar';

// A reply nobody changed for this long is a draft left open in a tab, not someone typing now.
const typingWithin = 2 * 60_000;

function sentence(viewer: ConversationViewer, timezone: string, now: number): string {
  const name = messages.viewerName(viewer);
  if (!viewer.writing) return messages.viewerTip(name, false);
  if (viewer.edited_at && now - Date.parse(viewer.edited_at) > typingWithin) return messages.replyLeftOpen(name, formatDate(viewer.edited_at, timezone));
  return messages.viewerTip(name, true);
}

// ConversationViewers names the others who have the open conversation in front of them, so nobody
// answers the customer over a teammate's reply. It is a live status: a change is read out once.
export function ConversationViewers({ viewers, timezone }: { viewers: ConversationViewer[]; timezone: string }) {
  if (viewers.length === 0) return null;
  const now = Date.now();
  return (
    <p role="status" className="conversation-viewers">
      <span className="viewer-stack" aria-hidden="true">
        {viewers.map((viewer) => <Avatar key={`${viewer.agent_id}:${viewer.agent_token?.id ?? ''}`} name={viewer.name} size="xs" agent={viewer.agent_token !== null} />)}
      </span>
      <span>{viewers.map((viewer, index) => <span key={`${viewer.agent_id}:${viewer.agent_token?.id ?? ''}`} data-writing={viewer.writing || undefined}>
        {index > 0 && ' · '}{sentence(viewer, timezone, now)}</span>)}</span>
    </p>
  );
}
