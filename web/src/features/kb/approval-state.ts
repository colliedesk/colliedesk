import type { KBApprovalMode } from './kb-api';
import { reviewMessages as r } from './review-messages';

// The server refuses to publish text that needs approval without a current approval (409 approval_required);
// the editor works the same out from what it already has, to turn Publish off and say why before a press.

// requiresApproval: any of the collections the text is in, was in or moves to requires approval: on, or inherit
// while the help center requires it. A collection the list does not hold yet follows the help center.
export function requiresApproval(collections: { id: string; require_approval: KBApprovalMode }[], helpCenter: boolean, ids: string[]) {
  return ids.some((id) => {
    const mode = collections.find((item) => item.id === id)?.require_approval ?? 'inherit';
    return mode === 'on' || (mode === 'inherit' && helpCenter);
  });
}

interface ReviewLike {
  state: 'open' | 'approved' | 'changes_requested' | 'withdrawn' | 'superseded';
  current: boolean;
  decision_note: string;
  reviewers: { name: string }[];
  decided_by: { name: string } | null;
}

export interface Approval<T extends ReviewLike> {
  required: boolean;
  // The review of the text as saved: open, approved or sent back.
  open?: T;
  approved?: T;
  changes?: T;
  // blocked says why Publish is off; unset, it may be pressed.
  blocked?: string;
}

// approvalState reads a text's reviews (newest first, as the server lists them); dirty: edits on screen are not saved.
export function approvalState<T extends ReviewLike>({ required, reviews, dirty }: { required: boolean; reviews: T[]; dirty: boolean }): Approval<T> {
  if (!required) return { required };
  const current = reviews.filter((review) => review.current);
  const open = current.find((review) => review.state === 'open');
  const approved = current.find((review) => review.state === 'approved');
  const changes = current.find((review) => review.state === 'changes_requested');
  const found = { ...(open ? { open } : {}), ...(approved ? { approved } : {}), ...(changes ? { changes } : {}) };
  if (approved) return { required, ...found, ...(dirty ? { blocked: r.editsNeedApproval } : {}) };
  if (open) return { required, ...found, blocked: r.waitingFor(open.reviewers) };
  if (changes) return { required, ...found, blocked: r.changesAsked(changes.decided_by?.name ?? '', changes.decision_note) };
  return { required, blocked: r.needsApproval };
}
