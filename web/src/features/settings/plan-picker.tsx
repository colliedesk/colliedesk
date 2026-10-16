import { useState } from 'react';
import type { CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';
import { planCardsFor, priceFor } from './plan-options';
import type { Interval } from './plan-options';

interface PlanPickerProps {
  sellable: readonly CheckoutPlan[];
  // busy is true while a checkout is starting or open; the buttons wait so a second click cannot open a second one.
  busy?: boolean;
  onChoose: (plan: CheckoutPlan) => void;
}

// PlanPicker shows each plan the workspace can buy as a card: its price, what it includes and one
// Choose button. Monthly or yearly is chosen once for all cards, and only when a plan offers both.
export function PlanPicker({ sellable, busy = false, onChoose }: PlanPickerProps) {
  const [interval, setInterval] = useState<Interval>('monthly');
  const cards = planCardsFor(sellable);
  const bothIntervals = cards.some((card) => card.prices.some((price) => price.interval === 'monthly') && card.prices.some((price) => price.interval === 'yearly'));
  return (
    <div className="plan-picker">
      {bothIntervals && <fieldset className="segmented" aria-label={messages.billingInterval}>
        {(['monthly', 'yearly'] as const).map((value) => (
          <label key={value}><input type="radio" name="billing-interval" value={value} checked={interval === value} onChange={() => setInterval(value)} />
            <span>{value === 'monthly' ? messages.monthly : messages.yearlyTwoMonthsFree}</span></label>
        ))}
      </fieldset>}
      <div className="plan-cards">{cards.map((card) => {
        const price = priceFor(card, interval);
        return (
          <article key={card.name} className="plan-card" aria-label={card.name}>
            <h3>{card.name}</h3>
            <p className="plan-price"><strong>{price.amount}</strong><span>{price.per}</span></p>
            <ul>{card.includes.map((line) => <li key={line}>{line}</li>)}</ul>
            <button type="button" className="primary-action" aria-label={price.label} disabled={busy} aria-busy={busy}
              onClick={() => onChoose(price.plan)}>{messages.choosePlanName(card.name)}</button>
          </article>
        );
      })}</div>
    </div>
  );
}
