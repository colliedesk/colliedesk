import { useId } from 'react';
import { useQuery } from '@tanstack/react-query';
import { kbApi } from './kb-api';
import type { KBCollection } from './kb-api';
import { reviewMessages as r } from './review-messages';

type Mode = KBCollection['require_approval'];

// CollectionApprovalField overrides, for one collection, whether publishing needs a teammate's approval.
// Inherit names what the help center says now, so nobody has to open Settings to know.
export function CollectionApprovalField({ value, onChange }: { value: Mode; onChange: (mode: Mode) => void }) {
  const id = useId();
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter, retry: false, staleTime: 60_000 });
  return <label className="field" htmlFor={id}><span>{r.collectionApproval}</span>
    <select id={id} value={value} onChange={(event) => onChange(event.target.value as Mode)}>
      <option value="inherit">{r.approvalModes.inherit(center.data?.require_approval ?? false)}</option>
      <option value="on">{r.approvalModes.on}</option>
      <option value="off">{r.approvalModes.off}</option>
    </select></label>;
}
