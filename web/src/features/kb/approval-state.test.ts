import { expect, test } from 'vitest';
import { approvalState, requiresApproval } from './approval-state';

const on = { id: 'a', require_approval: 'on' as const };
const inherit = { id: 'b', require_approval: 'inherit' as const };
const off = { id: 'c', require_approval: 'off' as const };
const review = (state: 'open' | 'approved' | 'changes_requested' | 'withdrawn' | 'superseded', current = true) => ({
  id: `r-${state}`, state, current, decision_note: state === 'changes_requested' ? 'Shorter intro' : '',
  reviewers: [{ agent_id: 'x', name: 'Bo', seen_at: null }], decided_by: state === 'open' ? null : { agent_id: 'x', name: 'Bo' },
});

test('a collection set to on, or inherit while the help center requires it, requires approval; off never does', () => {
  expect(requiresApproval([on, off], false, ['c'])).toBe(false);
  expect(requiresApproval([on, off], false, ['c', 'a'])).toBe(true);
  expect(requiresApproval([inherit], true, ['b'])).toBe(true);
  expect(requiresApproval([inherit], false, ['b'])).toBe(false);
  expect(requiresApproval([off], true, ['c'])).toBe(false);
  // A collection the list does not know yet follows the help center.
  expect(requiresApproval([], true, ['z'])).toBe(true);
});

test('publish is blocked with the reason until a current approval, and again by unsaved edits', () => {
  expect(approvalState({ required: false, reviews: [], dirty: false })).toEqual({ required: false });
  expect(approvalState({ required: true, reviews: [], dirty: false }).blocked).toBe('Publishing needs a teammate’s approval: send it for review.');
  expect(approvalState({ required: true, reviews: [review('open')], dirty: false }).blocked).toBe('Waiting for approval from Bo.');
  expect(approvalState({ required: true, reviews: [review('changes_requested')], dirty: false }).blocked).toBe('Bo asked for changes: Shorter intro');
  // An approval of an older text does not count.
  expect(approvalState({ required: true, reviews: [review('approved', false)], dirty: false }).blocked).toBe('Publishing needs a teammate’s approval: send it for review.');
  const approved = approvalState({ required: true, reviews: [review('approved')], dirty: false });
  expect(approved.blocked).toBeUndefined();
  expect(approved.approved?.decided_by?.name).toBe('Bo');
  expect(approvalState({ required: true, reviews: [review('approved')], dirty: true }).blocked).toBe('Your edits need a new approval: save them and send them for review.');
});
