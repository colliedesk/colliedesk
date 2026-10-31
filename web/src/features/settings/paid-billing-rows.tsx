import type { Billing, CheckoutPlan } from '../../api/schemas';
import { formatDay, formatMonthDay } from '../../lib/format-date';
import { messages } from '../../messages';
import { ExternalLinkIcon } from '../../ui/icons';
import './purchase.css';
import { billingMessages } from './billing-messages';
import { priceOfPlan } from './plan-price-line';
import { gibOf } from './usage-format';

interface MigrationState { requestedAt: string | null; done: boolean }

interface PaidBillingRowsProps {
  billing: Billing;
  timezone: string;
  // migration is set only for a paid Founding or Team 25 workspace whose import state is known.
  migration?: MigrationState;
  // remembered is the checkout this browser just made, which names the price of a plan with two (Team 25).
  remembered?: CheckoutPlan;
  // dialogOpen is true while a dialog sits over the page: its button is then the only ink button.
  dialogOpen?: boolean;
  onRequestMigration: () => void;
  onManage: () => void;
}

function Meter({ label, used, limit }: { label: string; used: number; limit: number }) {
  const percent = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;
  return <div className="usage-bar" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={limit} aria-valuenow={used} data-level={percent >= 80 ? 'high' : 'ok'}><span style={{ width: `${percent}%` }} /></div>;
}

function Row({ label, children, action }: { label: string; children: React.ReactNode; action?: React.ReactNode }) {
  return <div className="billing-row"><dt>{label}</dt><dd className="billing-value">{children}</dd><dd className="billing-action">{action}</dd></div>;
}

function MigrationRow({ migration, timezone, primary, onRequest }: { migration: MigrationState; timezone: string; primary: boolean; onRequest: () => void }) {
  if (migration.done) return <Row label={billingMessages.rowMigration}><span>{billingMessages.migrationFinished}</span></Row>;
  if (migration.requestedAt) {
    return <Row label={billingMessages.rowMigration}><span>{billingMessages.migrationRequested(formatMonthDay(migration.requestedAt, timezone))}</span><small>{billingMessages.migrationAnswer}</small></Row>;
  }
  return <Row label={billingMessages.rowMigration} action={<button type="button" className={primary ? 'primary-action' : undefined} onClick={onRequest}>{billingMessages.sendFreshdeskAddress}</button>}>
    <span>{billingMessages.migrationNot}</span></Row>;
}

// PaidBillingRows is Plan and billing for a workspace that has a paid plan: one row for each thing an admin
// comes to read (plan, next charge, seats, storage, migration, invoices), each with at most one small
// button, and Cancel plan as a quiet link under them. A complimentary plan has no charge, portal or cancel.
export function PaidBillingRows({ billing, timezone, migration, remembered, dialogOpen = false, onRequestMigration, onManage }: PaidBillingRowsProps) {
  const complimentary = billing.complimentary;
  const planName = messages.plans[billing.plan];
  const price = complimentary ? undefined : priceOfPlan(billing.plan as 'founding' | 'team25', remembered);
  const periodEnd = billing.current_period_end ? formatDay(billing.current_period_end, timezone) : undefined;
  const cancelled = billing.cancel_at_period_end || billing.status === 'canceled';
  const storage = billing.limits.storage_bytes;
  const seats = billing.usage.agents + billing.usage.pending_invites;
  return (
    <>
      <dl className="billing-rows" aria-label={billingMessages.rowsLabel}>
        <Row label={billingMessages.rowPlan}>
          {complimentary
            ? <span><strong>{billingMessages.complimentaryPlan(planName, billing.complimentary_until ? formatDay(billing.complimentary_until, timezone) : null)}</strong></span>
            : <span><strong>{planName}</strong>{price && ` · ${price.detail}`}</span>}
        </Row>
        {!complimentary && periodEnd && <Row label={billingMessages.rowNextCharge}><span>{cancelled ? billingMessages.rowNoCharge(periodEnd) : price ? billingMessages.chargeOn(price.amount, periodEnd) : periodEnd}</span></Row>}
        <Row label={billingMessages.rowAgents} action={<a className="button" href="/settings#invites-heading">{billingMessages.invitePeople}</a>}>
          <span>{billingMessages.seatsOf(billing.usage.agents, billing.limits.agents, billing.usage.pending_invites)}</span>
          <Meter label={billingMessages.rowAgents} used={seats} limit={billing.limits.agents} />
        </Row>
        {storage !== null && <Row label={billingMessages.rowStorage}>
          <span>{billingMessages.usageOf(gibOf(billing.usage.storage_bytes), gibOf(storage))}</span>
          <Meter label={billingMessages.rowStorage} used={Number(gibOf(billing.usage.storage_bytes))} limit={Number(gibOf(storage))} />
        </Row>}
        {!complimentary && migration && <MigrationRow migration={migration} timezone={timezone} primary={!dialogOpen} onRequest={onRequestMigration} />}
        {!complimentary && <Row label={billingMessages.rowPayment} action={<button type="button" onClick={onManage}>{billingMessages.openPortal}<ExternalLinkIcon /></button>}>
          <span className="muted">{billingMessages.paymentHelp}</span></Row>}
      </dl>
      {!complimentary && periodEnd && (cancelled
        ? <p className="cancel-line"><span>{billingMessages.cancelsOn(periodEnd)}</span></p>
        : <p className="cancel-line"><button type="button" className="link-button" onClick={onManage}>{billingMessages.cancelPlan}</button><span>{billingMessages.accessUntil(periodEnd)}</span></p>)}
    </>
  );
}
