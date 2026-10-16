import type { CheckoutPlan } from '../../api/schemas';
import { planCardsFor } from './plan-options';

interface PlanPickerProps {
  sellable: readonly CheckoutPlan[];
  // busy is true while a checkout is starting or open; the buttons wait so a second click cannot open a second one.
  busy?: boolean;
  onChoose: (plan: CheckoutPlan) => void;
}

// PlanPicker shows each plan the workspace can buy as a card: what it includes, then one button per price.
export function PlanPicker({ sellable, busy = false, onChoose }: PlanPickerProps) {
  return (
    <div className="plan-cards">{planCardsFor(sellable).map((card) => (
      <article key={card.name} className="plan-card" aria-label={card.name}>
        <h3>{card.name}</h3>
        <ul>{card.includes.map((line) => <li key={line}>{line}</li>)}</ul>
        <div className="plan-prices">{card.prices.map((price) => (
          <button key={price.plan} type="button" aria-label={price.label} disabled={busy} aria-busy={busy} onClick={() => onChoose(price.plan)}>{price.price}</button>
        ))}</div>
      </article>
    ))}</div>
  );
}
