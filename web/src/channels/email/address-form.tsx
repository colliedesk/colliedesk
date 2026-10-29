import { useId, useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { ConflictError } from '../../api/client';
import { settingsMessages } from '../../features/settings/settings-messages';
import { saveState, useSaveFeedback } from '../../features/settings/save-feedback';
import { conflictLink, emailMessages } from './email-shared';

export interface AddressInput { name?: string; address: string }

// AddressForm asks the support address, and a name when named (a second mailbox is told apart by it). It
// saves a new channel or the address of an existing one; save says which. A refusal is about the field it
// names: that field takes the focus, and the words stand right after it.
export function AddressForm({ canManage, named = false, initial = '', save, onSaved }: { canManage: boolean; named?: boolean; initial?: string;
  save: (input: AddressInput) => Promise<unknown>; onSaved?: () => void }) {
  const [name, setName] = useState('');
  const [address, setAddress] = useState(initial);
  const mutation = useMutation({ mutationFn: () => save({ ...(named && name.trim() ? { name: name.trim() } : {}), address: address.trim() }), onSuccess: onSaved });
  const state = saveState(mutation);
  const errorID = useId();
  const nameHint = useId();
  const about = mutation.error instanceof ConflictError && mutation.error.detail?.code === 'channel_name_taken' ? 'name' : 'address';
  const nameField = useRef<HTMLInputElement>(null);
  const addressField = useRef<HTMLInputElement>(null);
  const feedback = useSaveFeedback(state, errorID, about === 'name' ? nameField : addressField, about === 'name' ? name : address);
  // A teammate reads what is set.
  if (!canManage) return <p><strong>{settingsMessages.supportAddress}: </strong><span>{initial || settingsMessages.emailNotConnected}</span></p>;
  const invalid = (field: 'name' | 'address') => (feedback.invalid && about === field) || undefined;
  const refusal = (field: 'name' | 'address') => about === field && <>
    {feedback.status}
    {feedback.invalid && state.conflict && <p>{conflictLink(state.conflict)}</p>}
  </>;
  return <form className="settings-body" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) mutation.mutate(); }}>
    {named && <>
      <label className="field"><span>{emailMessages.name}</span>
        <input ref={nameField} value={name} maxLength={80} placeholder={address} aria-invalid={invalid('name')} aria-describedby={invalid('name') ? `${errorID} ${nameHint}` : nameHint}
          onChange={(event) => { feedback.touch(); setName(event.target.value); }} /></label>
      <small id={nameHint}>{emailMessages.nameHint}</small>
      {refusal('name')}
    </>}
    <label className="field"><span>{settingsMessages.supportAddress}</span>
      <input ref={addressField} type="email" required value={address} placeholder="support@yourcompany.com" aria-invalid={invalid('address')} aria-describedby={invalid('address') ? errorID : undefined}
        onChange={(event) => { feedback.touch(); setAddress(event.target.value); }} /></label>
    {refusal('address')}
    <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(settingsMessages.saveSupportAddress)}</button></span>
  </form>;
}
