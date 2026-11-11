import { useState } from 'react';
import type { CheckoutPlan } from '../../api/schemas';
import { CheckIcon, CircleAlertIcon } from '../../ui/icons';
import './purchase.css';
import { billingMessages } from './billing-messages';
import { fitLine, isGreyed } from './plan-fit-line';
import { isFoundingOffer, planCardsFor, priceFor } from './plan-options';
import type { Interval, PlanCard, PlanId } from './plan-options';
import type { PlanIntent } from './plan-intent';
import { recommendPlan } from './plan-recommendation';

interface PlanPickerProps {
  sellable: readonly CheckoutPlan[];
  // refusals are the prices the workspace does not fit, with the server's reason.
  refusals?: readonly { plan: CheckoutPlan; reason: string }[];
  // seats is how many agents and pending invites the workspace has; freshdeskAgents is the agent count of the
  // Freshdesk account it read, which wins when there is one.
  seats: number;
  freshdeskAgents?: number;
  // placesLeft is how many Founding places are free; unknown (not shown) until the server has counted.
  placesLeft?: number;
  // starting is the plan whose checkout is opening: its button says so and everything else waits.
  starting?: CheckoutPlan;
  // preferredPlan is the plan the visitor chose on the website: it leads and says it was their pick.
  preferredPlan?: PlanIntent;
  // compact leaves out what each plan includes and the notes, for the locked screen.
  compact?: boolean;
  // error says why a checkout did not open; onRetry, when given, adds Try again.
  error?: string;
  onRetry?: () => void;
  onChoose: (plan: CheckoutPlan) => void;
}

const quarterHint = (placesLeft: number | undefined) => (placesLeft === undefined ? undefined : billingMessages.placesLeft(placesLeft));

function Card({ card, interval, lead, fit, greyed, starting, anyStarting, placesLeft, compact, onChoose }: {
  card: PlanCard; interval: Interval; lead: 'pick' | 'fits' | undefined; fit: string; greyed: boolean; starting?: CheckoutPlan; anyStarting: boolean; placesLeft?: number; compact: boolean;
  onChoose: (plan: CheckoutPlan) => void;
}) {
  const price = priceFor(card, interval);
  const fitId = `plan-fit-${card.id}`;
  const opening = starting === price.plan;
  const note = [price.note, card.id === 'founding' ? quarterHint(placesLeft) : undefined].filter(Boolean).join(' · ');
  return (
    <article className={lead ? 'plan-card plan-card-picked' : 'plan-card'} aria-label={card.name} data-fit={greyed ? 'no' : undefined}>
      <div className="plan-card-head"><h3>{card.name}</h3>
        {lead && <span className="plan-pick"><CheckIcon />{lead === 'pick' ? billingMessages.planPicked : billingMessages.planFits}</span>}</div>
      <p className="plan-audience">{card.audience}</p>
      <div className="plan-price"><p><strong>{price.amount}</strong><span>{price.per}</span></p><small>{note}</small></div>
      <p className="plan-fit" id={fitId}>{fit}</p>
      <button type="button" className={lead ? 'primary-action' : undefined} aria-label={price.label} aria-busy={opening || undefined} aria-describedby={fit ? fitId : undefined}
        disabled={anyStarting || greyed} onClick={() => onChoose(price.plan)}>
        {opening ? <><span className="spinner" aria-hidden="true" />{billingMessages.openingCheckout}</> : billingMessages.choosePlanName(card.name)}</button>
      {!compact && <ul>{card.includes.map((line) => <li key={line}>{line}</li>)}</ul>}
    </article>
  );
}

// PlanPicker shows each plan the workspace can buy as a card: who it is for, its price, why it fits, one Choose
// button and what it includes. The plan that fits the team (or the one the visitor chose) leads, carries a word
// and the only ink button; the others are plain buttons. Monthly or yearly is chosen once for all cards.
export function PlanPicker({ sellable, refusals = [], seats, freshdeskAgents, placesLeft, starting, preferredPlan, compact = false, error, onRetry, onChoose }: PlanPickerProps) {
  const [interval, setInterval] = useState<Interval>('monthly');
  const unordered = planCardsFor(sellable, refusals);
  const foundingOffer = isFoundingOffer(unordered);
  const recommendation = unordered.length === 0 ? undefined : recommendPlan({ offer: unordered.map((card): PlanId => card.id), intent: preferredPlan, freshdeskAgents, seats });
  // The offer of two puts the plan that fits first; the ladder keeps its order, except that a plan the visitor chose goes first.
  const cards = planCardsFor(sellable, refusals, foundingOffer ? recommendation?.plan : preferredPlan);
  const bothIntervals = cards.some((card) => card.prices.some((price) => price.interval === 'monthly') && card.prices.some((price) => price.interval === 'yearly'));
  const anyStarting = starting !== undefined;
  return (
    <section className="plan-picker" data-compact={compact || undefined} aria-labelledby="plan-heading">
      <div className="plan-picker-head"><h2 id="plan-heading">{billingMessages.upgradeHeading}</h2>
        {bothIntervals && <div className="plan-interval">
          {foundingOffer && <span>{billingMessages.team25Billed}</span>}
          <fieldset className="segmented" aria-label={foundingOffer ? billingMessages.team25Interval : billingMessages.billingInterval}>
            {(['monthly', 'yearly'] as const).map((value) => (
              <label key={value}><input type="radio" name="billing-interval" value={value} checked={interval === value} disabled={anyStarting} onChange={() => setInterval(value)} />
                <span>{value === 'monthly' ? billingMessages.monthly : billingMessages.yearlyTwoMonthsFree}</span></label>
            ))}
          </fieldset></div>}
      </div>
      <div className="plan-cards">{cards.map((card) => {
        const refusal = priceFor(card, interval).refusal;
        const lead = card.id === recommendation?.plan && (recommendation.pick === 'intent' || !isGreyed(card, recommendation, refusal))
          ? (recommendation.pick === 'intent' ? 'pick' : 'fits') : undefined;
        return <Card key={card.id} card={card} interval={interval} lead={lead} fit={fitLine(card, recommendation, refusal)} greyed={isGreyed(card, recommendation, refusal)}
          starting={starting} anyStarting={anyStarting} placesLeft={placesLeft} compact={compact} onChoose={onChoose} />;
      })}</div>
      {error && <p role="alert" className="inline-alert"><CircleAlertIcon /><span>{error}</span>
        {onRetry && <button type="button" onClick={onRetry}>{billingMessages.tryAgain}</button>}</p>}
      {!compact && <div className="plan-notes">
        {cards.some((card) => card.id === 'team25') && <p>{billingMessages.largerThan25} <a href={`mailto:${billingMessages.supportEmail}`}>{billingMessages.supportEmail}</a></p>}
        <p>{billingMessages.payByCard}</p></div>}
    </section>
  );
}
