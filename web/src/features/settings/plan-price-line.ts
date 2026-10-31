import type { CheckoutPlan } from '../../api/schemas';
import { planCardsFor } from './plan-options';
import type { PlanId } from './plan-options';

const lowerFirst = (text: string) => text.charAt(0).toLowerCase() + text.slice(1);

export interface PurchasedPrice {
  name: string;
  // amount is what one payment charges.
  amount: string;
  // text reads as a sentence part ("$99 every 3 months, price locked for life"); detail is the same as a
  // line of a receipt ("$99 every 3 months · price locked for life"), which follows the plan's name.
  text: string;
  detail: string;
}

// purchasedPrice describes what a checkout plan costs, from the same prices the picker shows.
export function purchasedPrice(plan: CheckoutPlan): PurchasedPrice | undefined {
  const card = planCardsFor([plan])[0];
  const price = card?.prices[0];
  if (!card || !price) return undefined;
  const text = `${price.amount} ${price.per}${price.note ? `, ${lowerFirst(price.note)}` : ''}`;
  return { name: card.name, amount: price.amount, text, detail: `${price.amount} ${price.per}${price.note ? ` · ${lowerFirst(price.note)}` : ''}` };
}

// priceOfPlan is the price of a paid plan when it is known. Founding has one price. Team 25 has two and the
// subscription does not say which was bought, so it is known only for the checkout this browser just made.
export function priceOfPlan(plan: PlanId, remembered?: CheckoutPlan): PurchasedPrice | undefined {
  if (remembered?.startsWith(plan)) return purchasedPrice(remembered);
  return plan === 'founding' ? purchasedPrice('founding') : undefined;
}
