import type { CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';

export type Interval = 'monthly' | 'yearly';
export interface PlanPrice { plan: CheckoutPlan; interval: Interval; amount: string; per: string; label: string }
export interface PlanCard { name: string; includes: string[]; prices: PlanPrice[] }

// What each plan includes; the numbers match docs/user/billing.md and the server's plan limits.
const allPlans: { name: string; includes: string[]; prices: Omit<PlanPrice, 'label'>[] }[] = [
  { name: messages.plans.team, includes: [messages.planAgents(10), messages.planStorage('100 GiB')], prices: [
    { plan: 'team_monthly', interval: 'monthly', amount: '$49', per: messages.perMonth },
    { plan: 'team_yearly', interval: 'yearly', amount: '$490', per: messages.perYear }] },
  { name: messages.plans.team25, includes: [messages.planAgents(25), messages.planStorage('250 GiB')], prices: [
    { plan: 'team25_monthly', interval: 'monthly', amount: '$99', per: messages.perMonth },
    { plan: 'team25_yearly', interval: 'yearly', amount: '$990', per: messages.perYear }] },
  { name: messages.plans.founding, includes: [messages.planAgents(10), messages.planStorage('20 GiB'), messages.foundingLimited], prices: [
    { plan: 'founding', interval: 'yearly', amount: '$100', per: messages.perYearForLife }] },
];

// The plans a workspace can buy, in the order they are offered: only the prices the server says it
// can sell, and only plans with at least one of them. A price's label names the plan for screen readers.
export function planCardsFor(sellable: readonly CheckoutPlan[]): PlanCard[] {
  return allPlans
    .map((card) => ({ ...card, prices: card.prices.filter((price) => sellable.includes(price.plan))
      .map((price) => ({ ...price, label: messages.choosePlanPrice(card.name, `${price.amount} ${price.per}`) })) }))
    .filter((card) => card.prices.length > 0);
}

// priceFor is the card's price for the chosen interval, or its only price (Founding is yearly only).
export function priceFor(card: PlanCard, interval: Interval): PlanPrice {
  return card.prices.find((price) => price.interval === interval) ?? card.prices[0];
}
