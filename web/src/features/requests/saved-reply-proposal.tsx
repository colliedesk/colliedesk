import type { AgentRequest } from '../../api/schemas';
import { savedReplyMessages } from '../settings/saved-reply-messages';

const savedRepliesRoute = '/api/v1/saved-replies';

// isSavedReplyRequest is true for a request that creates, edits, deletes or restores a saved reply.
export const isSavedReplyRequest = (item: AgentRequest) => item.route === savedRepliesRoute || item.route.startsWith(`${savedRepliesRoute}/{id}`);

interface Text { title: string; body: string }

function proposedFields(call: AgentRequest['body']): Partial<Text> {
  if (typeof call !== 'object' || call === null || Array.isArray(call)) return {};
  const { title, body } = call as { title?: unknown; body?: unknown };
  return { ...(typeof title === 'string' ? { title } : {}), ...(typeof body === 'string' ? { body } : {}) };
}

// Field shows one part of the reply: unchanged, removed (del), added (ins), or replaced (both).
function Field({ from, to }: { from?: string; to?: string }) {
  if (from !== undefined && to !== undefined && from === to) return <span className="saved-reply-version-body">{from}</span>;
  return <>{from !== undefined && <del>{from}</del>}{to !== undefined && <ins>{to}</ins>}</>;
}

// SavedReplyProposal shows a person deciding what an agent request would do to a saved reply: the reply
// as the agent saw it and what it would become, and whether the reply changed since the agent asked.
export function SavedReplyProposal({ item }: { item: AgentRequest }) {
  const proposed = proposedFields(item.body);
  const before = item.saved_reply?.before ?? undefined;
  const current = item.saved_reply?.current;
  const changedSince = before && current && (current.updated_at !== before.updated_at || current.deleted_at !== null);
  let content;
  if (!before) {
    content = <><p className="muted-copy">{savedReplyMessages.savedReplyProposalNew}</p><Field to={proposed.title} /><Field to={proposed.body} /></>;
  } else if (item.method === 'DELETE') {
    content = <><Field from={before.title} /><Field from={before.body} /><p className="muted-copy">{savedReplyMessages.savedReplyProposalDelete}</p></>;
  } else if (item.route.endsWith('/{id}/restore')) {
    content = <><Field to={before.title} /><Field to={before.body} /><p className="muted-copy">{savedReplyMessages.savedReplyProposalUndelete}</p></>;
  } else if (item.route.endsWith('/restore')) {
    const restores = item.saved_reply?.restores;
    content = restores
      ? <><p className="muted-copy">{savedReplyMessages.savedReplyProposalVersion(restores.version)}</p>
          <Field from={before.title} to={restores.title} /><Field from={before.body} to={restores.body} /></>
      : <><Field from={before.title} /><Field from={before.body} /></>;
  } else {
    content = <><Field from={before.title} to={proposed.title ?? before.title} /><Field from={before.body} to={proposed.body ?? before.body} /></>;
  }
  return (
    <div role="group" aria-label={savedReplyMessages.savedReplyProposal} className="saved-reply-diff">
      {content}
      {changedSince && <><p className="warning-note">{savedReplyMessages.savedReplyChangedSince}</p><p className="request-body">{current.title}</p><p className="request-body">{current.body}</p></>}
      {before && item.saved_reply && current === null && <p className="warning-note">{savedReplyMessages.savedReplyGoneSince}</p>}
    </div>
  );
}
