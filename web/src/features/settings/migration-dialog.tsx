import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { InvalidRequestError } from '../../api/client';
import type { z } from 'zod';
import type { migrationRequestSchema } from '../../api/schemas';
import { messages } from '../../messages';
import { CheckCircleIcon, CircleAlertIcon } from '../../ui/icons';
import { Dialog, Field } from '../../ui/dialog';
import './purchase.css';
import { billingMessages } from './billing-messages';

export type MigrationPlan = 'founding' | 'team25';
type MigrationRequest = z.infer<typeof migrationRequestSchema>;
type State = 'editing' | 'sending' | 'failed' | 'sent';

interface MigrationDialogProps {
  // welcome is set right after the payment: the dialog then welcomes the buyer and shows the receipt.
  welcome?: { plan: MigrationPlan; summary: string; nextCharge: string };
  // plan names the plan when there is no welcome (the form opened from Plan and billing).
  plan?: MigrationPlan;
  onSend: (input: { freshdesk_address: string; times: string }) => Promise<MigrationRequest>;
  onDismiss: () => void;
}

const fieldMessage = (error: unknown, field: string): string | undefined => {
  const fields = error instanceof InvalidRequestError ? (error.detail?.fields as Record<string, unknown> | undefined) : undefined;
  return typeof fields?.[field] === 'string' ? fields[field] : undefined;
};

// MigrationDialog is the one job after paying for Founding or Team 25: send us the Freshdesk address (and
// times that suit) so we can move the data. A failed send keeps what was typed; a sent one says when to expect an answer.
export function MigrationDialog({ welcome, plan: asked, onSend, onDismiss }: MigrationDialogProps) {
  const plan = welcome?.plan ?? asked ?? 'founding';
  const [address, setAddress] = useState('');
  const [times, setTimes] = useState('');
  const [state, setState] = useState<State>('editing');
  const [addressError, setAddressError] = useState<string>();
  const [timesError, setTimesError] = useState<string>();
  const [sent, setSent] = useState<MigrationRequest>();
  const sending = state === 'sending';
  // Focus starts on the step's heading, not on a field: a phone keeps its keyboard down until the buyer taps, and
  // Escape and Tab already work from the first key.
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);

  async function submit(_event?: FormEvent) {
    if (sending) return;
    setAddressError(undefined);
    setTimesError(undefined);
    if (address.trim() === '') { setAddressError(billingMessages.freshdeskAddressMissing); setState('editing'); return; }
    setState('sending');
    try {
      setSent(await onSend({ freshdesk_address: address.trim(), times: times.trim() }));
      setState('sent');
    } catch (error) {
      const refused = fieldMessage(error, 'freshdesk_address');
      const refusedTimes = fieldMessage(error, 'times');
      setAddressError(refused);
      setTimesError(refusedTimes);
      setState(refused || refusedTimes ? 'editing' : 'failed');
    }
  }

  const footer = state === 'sent'
    ? <button key="done" type="button" className="primary-action" autoFocus onClick={onDismiss}>{billingMessages.migrationDone}</button>
    : <>
      <button type="submit" className="primary-action" aria-busy={sending || undefined} disabled={sending}>
        {sending ? <><span className="spinner" aria-hidden="true" />{billingMessages.migrationSending}</> : state === 'failed' ? billingMessages.migrationRetry : billingMessages.migrationSend}</button>
      <a className="link-action" href="/settings/import-freshdesk" onClick={onDismiss}>{billingMessages.migrationSelfServe}</a>
      <button type="button" className="link-button" onClick={onDismiss}>{billingMessages.migrationLater}</button>
    </>;
  return (
    <Dialog className="welcome-dialog" title={welcome ? billingMessages.welcomeTitle(messages.plans[welcome.plan]) : billingMessages.migrationTitle} closeLabel={billingMessages.migrationClose}
      onClose={onDismiss} onSubmit={() => void submit()} footer={footer}>
      {welcome && <div className="receipt"><p>{welcome.summary}</p><p>{welcome.nextCharge}</p></div>}
      <section className="migration-step" aria-labelledby="migration-heading">
        {state === 'sent' && sent ? <>
          <h3 id="migration-heading" className="sr-only">{billingMessages.migrationTitle}</h3>
          <div className="sent-note" role="status"><CheckCircleIcon /><p>{billingMessages.migrationSent}</p></div>
          <dl className="order-summary"><dt>{billingMessages.migrationSentFreshdesk}</dt><dd>{sent.freshdesk_address}</dd>
            {sent.times && <><dt>{billingMessages.migrationSentTimes}</dt><dd>{sent.times}</dd></>}</dl></> : <>
          <h3 id="migration-heading" ref={heading} tabIndex={-1}>{billingMessages.migrationHeading}</h3>
          <p>{plan === 'founding' ? billingMessages.migrationFounding : billingMessages.migrationTeam25}</p>
          <Field id="fd-address" label={billingMessages.freshdeskAddress} error={addressError}>
            {(props) => <input {...props} name="address" autoComplete="url" placeholder={billingMessages.freshdeskPlaceholder} value={address} readOnly={sending} onChange={(event) => setAddress(event.target.value)} />}</Field>
          <Field id="fd-times" label={billingMessages.migrationTimes} error={timesError}>
            {(props) => <input {...props} name="times" placeholder={billingMessages.migrationTimesPlaceholder} value={times} readOnly={sending} onChange={(event) => setTimes(event.target.value)} />}</Field>
          {state === 'failed' && <p role="alert" className="form-alert"><CircleAlertIcon /><span>{billingMessages.migrationFailed} <a href={`mailto:${billingMessages.supportEmail}`}>{billingMessages.supportEmail}</a>.</span></p>}</>}
      </section>
    </Dialog>
  );
}
