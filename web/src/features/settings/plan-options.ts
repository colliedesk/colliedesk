import type { CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';

export type Interval = 'monthly' | 'yearly';
// amount is the price per month, so plans compare at a glance; billed says what a yearly plan charges.
export interface PlanPrice { plan: CheckoutPlan; interval: Interval; amount: string; billed?: string; label: string }
export interface PlanCard { name: string; includes: string[]; prices: PlanPrice[] }

// What each plan includes; the numbers match docs/user/billing.md and the server's plan limits.
// charge is what one payment costs; it names the button for screen readers.
const allPlans: { name: string; includes: string[]; prices: (Omit<PlanPrice, 'label'> & { charge: string })[] }[] = [
  { name: messages.plans.team, includes: [messages.planAgents(10), messages.planStorage('100 GiB')], prices: [
    { plan: 'team_monthly', interval: 'monthly', amount: '$49', charge: '$49 per month' },
    { plan: 'team_yearly', interval: 'yearly', amount: '$40.83', billed: messages.billedYearly('$490'), charge: '$490 per year' }] },
  { name: messages.plans.team25, includes: [messages.planAgents(25), messages.planStorage('250 GiB')], prices: [
    { plan: 'team25_monthly', interval: 'monthly', amount: '$99', charge: '$99 per month' },
    { plan: 'team25_yearly', interval: 'yearly', amount: '$82.50', billed: messages.billedYearly('$990'), charge: '$990 per year' }] },
  { name: messages.plans.founding, includes: [messages.planAgents(10), messages.planStorage('20 GiB'), messages.foundingLimited], prices: [
    { plan: 'founding', interval: 'yearly', amount: '$8.33', billed: messages.billedFounding, charge: '$100 per year for life' }] },
];

// The plans a workspace can buy, in the order they are offered: only the prices the server says it
// can sell, and only plans with at least one of them.
export function planCardsFor(sellable: readonly CheckoutPlan[]): PlanCard[] {
  return allPlans
    .map((card) => ({ ...card, prices: card.prices.filter((price) => sellable.includes(price.plan))
      .map(({ charge, ...price }) => ({ ...price, label: messages.choosePlanPrice(card.name, charge) })) }))
    .filter((card) => card.prices.length > 0);
}

// priceFor is the card's price for the chosen interval, or its only price (Founding is yearly only).
export function priceFor(card: PlanCard, interval: Interval): PlanPrice {
  return card.prices.find((price) => price.interval === interval) ?? card.prices[0];
}
