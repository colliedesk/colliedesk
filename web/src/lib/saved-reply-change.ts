import type { SavedReplyChange } from '../api/schemas';
import { messages } from '../messages';

// savedReplyChangeText says who made a change to a saved reply and how: "Edited by Ada via Claude
// triage", "Version 2 restored by Ada", or for an approved agent request "Edited by Ada via Claude
// triage (approved request)". A knowledge base article's history reads the same, and adds imports.
export function savedReplyChangeText(change: Pick<SavedReplyChange, 'restored_from_version' | 'changed_by' | 'token' | 'agent_request_id'> & { kind: SavedReplyChange['kind'] | 'import' | 'media_replace' | 'ai_exclude' | 'ai_include' }): string {
  const what = change.kind === 'restore' && change.restored_from_version !== null
    ? messages.savedReplyRestoredVersion(change.restored_from_version)
    : messages.savedReplyChangeVerb[change.kind];
  // A baseline names no person: who wrote the text before history started is not known; an import names the
  // help desk's person in the article, not here.
  if (change.kind === 'baseline' || (change.kind === 'import' && !change.changed_by)) return what;
  let text = messages.savedReplyChangeBy(what, change.changed_by?.name ?? messages.someone);
  if (change.token) text = messages.savedReplyChangeVia(text, change.token.label);
  if (change.agent_request_id) text = `${text} (${messages.savedReplyApprovedRequest})`;
  return text;
}
