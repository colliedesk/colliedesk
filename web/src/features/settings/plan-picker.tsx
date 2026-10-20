import { useState } from 'react';
import type { CheckoutPlan } from '../../api/schemas';
import { billingMessages } from './billing-messages';
import { planCardsFor, priceFor } from './plan-options';
import type { Interval } from './plan-options';

interface PlanPickerProps {
  sellable: readonly CheckoutPlan[];
  // refusals are the prices the workspace does not fit, with the server's reason.
  refusals?: readonly { plan: CheckoutPlan; reason: string }[];
  // busy is true while a checkout is starting or open; the buttons wait so a second click cannot open a second one.
  busy?: boolean;
  onChoose: (plan: CheckoutPlan) => void;
}

// PlanPicker shows each plan the workspace can buy as a card: its price, what it includes and one
// Choose button. Monthly or yearly is chosen once for all cards, and only when a plan offers both.
export function PlanPicker({ sellable, refusals = [], busy = false, onChoose }: PlanPickerProps) {
  const [interval, setInterval] = useState<Interval>('monthly');
  const cards = planCardsFor(sellable, refusals);
  const bothIntervals = cards.some((card) => card.prices.some((price) => price.interval === 'monthly') && card.prices.some((price) => price.interval === 'yearly'));
  return (
    <div className="plan-picker">
      {bothIntervals && <fieldset className="segmented" aria-label={billingMessages.billingInterval}>
        {(['monthly', 'yearly'] as const).map((value) => (
          <label key={value}><input type="radio" name="billing-interval" value={value} checked={interval === value} onChange={() => setInterval(value)} />
            <span>{value === 'monthly' ? billingMessages.monthly : billingMessages.yearlyTwoMonthsFree}</span></label>
        ))}
      </fieldset>}
      <div className="plan-cards">{cards.map((card) => {
        const price = priceFor(card, interval);
        const refusalId = `plan-refusal-${price.plan}`;
        return (
          <article key={card.name} className="plan-card" aria-label={card.name}>
            <h3>{card.name}</h3>
            <div className="plan-price"><p><strong>{price.amount}</strong><span>{billingMessages.perMonth}</span></p>
              <small>{price.billed ?? billingMessages.billedMonthly}</small></div>
            <button type="button" className="primary-action" aria-label={price.label} disabled={busy || price.refusal !== undefined} aria-busy={busy}
              aria-describedby={price.refusal ? refusalId : undefined} onClick={() => onChoose(price.plan)}>{billingMessages.choosePlanName(card.name)}</button>
            {price.refusal && <p id={refusalId} className="muted-copy">{price.refusal}</p>}
            <ul>{card.includes.map((line) => <li key={line}>{line}</li>)}</ul>
          </article>
        );
      })}</div>
    </div>
  );
}
