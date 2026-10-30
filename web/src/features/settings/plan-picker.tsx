import { useState } from 'react';
import type { CheckoutPlan } from '../../api/schemas';
import { billingMessages } from './billing-messages';
import { isFoundingOffer, planCardsFor, priceFor } from './plan-options';
import type { Interval } from './plan-options';
import type { PlanIntent } from './plan-intent';

interface PlanPickerProps {
  sellable: readonly CheckoutPlan[];
  // refusals are the prices the workspace does not fit, with the server's reason.
  refusals?: readonly { plan: CheckoutPlan; reason: string }[];
  // busy is true while a checkout is starting or open; the buttons wait so a second click cannot open a second one.
  busy?: boolean;
  // disabled turns every Choose button off, for the minutes a payment is still being confirmed.
  disabled?: boolean;
  // preferredPlan is the plan the visitor came for on the website: its card comes first and says so.
  preferredPlan?: PlanIntent;
  onChoose: (plan: CheckoutPlan) => void;
}

// PlanPicker shows each plan the workspace can buy as a card: its price, what it includes and one
// Choose button. Monthly or yearly is chosen once for all cards, and only when a plan offers both.
export function PlanPicker({ sellable, refusals = [], busy = false, disabled = false, preferredPlan, onChoose }: PlanPickerProps) {
  const [interval, setInterval] = useState<Interval>('monthly');
  const cards = planCardsFor(sellable, refusals, preferredPlan);
  const foundingOffer = isFoundingOffer(cards);
  const bothIntervals = cards.some((card) => card.prices.some((price) => price.interval === 'monthly') && card.prices.some((price) => price.interval === 'yearly'));
  return (
    <div className="plan-picker">
      {bothIntervals && <div className="plan-interval">
        {foundingOffer && <span>{billingMessages.team25Billed}</span>}
        <fieldset className="segmented" aria-label={billingMessages.billingInterval}>
          {(['monthly', 'yearly'] as const).map((value) => (
            <label key={value}><input type="radio" name="billing-interval" value={value} checked={interval === value} onChange={() => setInterval(value)} />
              <span>{value === 'monthly' ? billingMessages.monthly : billingMessages.yearlyTwoMonthsFree}</span></label>
          ))}
        </fieldset>
      </div>}
      <div className="plan-cards">{cards.map((card) => {
        const price = priceFor(card, interval);
        const refusalId = `plan-refusal-${price.plan}`;
        const picked = card.id === preferredPlan;
        return (
          <article key={card.name} className={picked ? 'plan-card plan-card-picked' : 'plan-card'} aria-label={card.name}>
            <h3>{card.name}{picked && <span className="plan-pick">{billingMessages.planPicked}</span>}</h3>
            <div className="plan-price"><p><strong>{price.amount}</strong><span>{price.per ?? billingMessages.perMonth}</span></p>
              <small>{price.billed ?? billingMessages.billedMonthly}</small></div>
            <button type="button" className="primary-action" aria-label={price.label} disabled={busy || disabled || price.refusal !== undefined} aria-busy={busy}
              aria-describedby={price.refusal ? refusalId : undefined} onClick={() => onChoose(price.plan)}>{billingMessages.choosePlanName(card.name)}</button>
            {price.refusal && <p id={refusalId} className="muted-copy">{price.refusal}</p>}
            <ul>{card.includes.map((line) => <li key={line}>{line}</li>)}</ul>
          </article>
        );
      })}</div>
      {foundingOffer && <p className="muted-copy">{billingMessages.selfHostInstead} <a href={billingMessages.selfHostURL}>{billingMessages.selfHostLink}</a></p>}
    </div>
  );
}
