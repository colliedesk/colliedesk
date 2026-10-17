import type { SavedReplyChange } from '../api/schemas';
import { messages } from '../messages';

// savedReplyChangeText says who made a change to a saved reply and how: "Edited by Ada via Claude
// triage", "Version 2 restored by Ada", or for an approved agent request "Edited by Ada via Claude
// triage (approved request)".
export function savedReplyChangeText(change: Pick<SavedReplyChange, 'kind' | 'restored_from_version' | 'changed_by' | 'token' | 'agent_request_id'>): string {
  const what = change.kind === 'restore' && change.restored_from_version !== null
    ? messages.savedReplyRestoredVersion(change.restored_from_version)
    : messages.savedReplyChangeVerb[change.kind];
  // A baseline names no person: who wrote the text before history started is not known.
  if (change.kind === 'baseline') return what;
  let text = messages.savedReplyChangeBy(what, change.changed_by?.name ?? messages.someone);
  if (change.token) text = messages.savedReplyChangeVia(text, change.token.label);
  if (change.agent_request_id) text = `${text} (${messages.savedReplyApprovedRequest})`;
  return text;
}
