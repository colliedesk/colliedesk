import { useEffect, useState } from 'react';
import type { CheckoutPlan } from '../../api/schemas';
import './purchase.css';
import { billingMessages } from './billing-messages';
import { purchasedPrice } from './plan-price-line';

// After this long without a confirmation the page stops promising and says what to do.
export const stillWaitingAfterMs = 60_000;

// PaymentConfirming replaces the plan picker for the minutes after a checkout until the payment provider
// confirms the plan. It says how long that takes, keeps the order in view and, if it takes longer than a minute,
// says so and offers Check again. It never says the payment arrived: only the plan changing says that.
export function PaymentConfirming({ plan, onCheckAgain }: { plan?: CheckoutPlan; onCheckAgain: () => void }) {
  const [longWait, setLongWait] = useState(false);
  useEffect(() => {
    const timer = setTimeout(() => setLongWait(true), stillWaitingAfterMs);
    return () => clearTimeout(timer);
  }, []);
  const order = plan ? purchasedPrice(plan) : undefined;
  return (
    <section className="payment-confirming" aria-labelledby="confirming-heading" aria-live="polite">
      <header><span className="spinner" aria-hidden="true" /><h2 id="confirming-heading">{billingMessages.confirmingHeading}</h2>
        <p>{longWait ? <>{billingMessages.confirmingStillWaiting} <a href={`mailto:${billingMessages.supportEmail}`}>{billingMessages.supportEmail}</a></> : billingMessages.confirmingWait}</p></header>
      {order && <dl className="order-summary"><dt>{billingMessages.orderPlan}</dt><dd>{order.name}</dd><dt>{billingMessages.orderPrice}</dt><dd>{order.text}</dd></dl>}
      {longWait && <div className="button-row"><button type="button" onClick={onCheckAgain}>{billingMessages.checkAgain}</button></div>}
    </section>
  );
}
