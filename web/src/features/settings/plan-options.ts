import type { CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';

export type Interval = 'monthly' | 'yearly';
// amount is the price per month, so plans compare at a glance; billed says what a yearly plan charges.
// refusal is the server's reason this workspace cannot buy the price now; the button is then disabled.
export interface PlanPrice { plan: CheckoutPlan; interval: Interval; amount: string; billed?: string; label: string; refusal?: string }
export interface PlanCard { name: string; includes: string[]; prices: PlanPrice[] }

// What each plan includes; the numbers match docs/user/billing.md and the server's plan limits.
// charge is what one payment costs; it names the button for screen readers.
const allPlans: { name: string; includes: string[]; prices: (Omit<PlanPrice, 'label'> & { charge: string })[] }[] = [
  { name: messages.plans.solo, includes: [messages.planAgents(1), messages.planStorage('10 GiB')], prices: [
    { plan: 'solo_monthly', interval: 'monthly', amount: '$19', charge: '$19 per month' },
    { plan: 'solo_yearly', interval: 'yearly', amount: '$15.83', billed: messages.billedYearly('$190'), charge: '$190 per year' }] },
  { name: messages.plans.small, includes: [messages.planAgents(3), messages.planStorage('30 GiB')], prices: [
    { plan: 'small_monthly', interval: 'monthly', amount: '$39', charge: '$39 per month' },
    { plan: 'small_yearly', interval: 'yearly', amount: '$32.50', billed: messages.billedYearly('$390'), charge: '$390 per year' }] },
  { name: messages.plans.team, includes: [messages.planAgents(10), messages.planStorage('100 GiB')], prices: [
    { plan: 'team_monthly', interval: 'monthly', amount: '$69', charge: '$69 per month' },
    { plan: 'team_yearly', interval: 'yearly', amount: '$57.50', billed: messages.billedYearly('$690'), charge: '$690 per year' }] },
  { name: messages.plans.team25, includes: [messages.planAgents(25), messages.planStorage('250 GiB')], prices: [
    { plan: 'team25_monthly', interval: 'monthly', amount: '$149', charge: '$149 per month' },
    { plan: 'team25_yearly', interval: 'yearly', amount: '$124.17', billed: messages.billedYearly('$1,490'), charge: '$1,490 per year' }] },
  { name: messages.plans.founding, includes: [messages.planAgents(10), messages.planStorage('20 GiB'), messages.foundingLimited], prices: [
    { plan: 'founding', interval: 'yearly', amount: '$33', billed: messages.billedFounding, charge: '$99 per quarter for life' }] },
];

// The plans a workspace can buy, in the order they are offered: only the prices the server says it
// can sell, and only plans with at least one of them.
export function planCardsFor(sellable: readonly CheckoutPlan[], refusals: readonly { plan: CheckoutPlan; reason: string }[] = []): PlanCard[] {
  return allPlans
    .map((card) => ({ ...card, prices: card.prices.filter((price) => sellable.includes(price.plan))
      .map(({ charge, ...price }) => ({ ...price, label: messages.choosePlanPrice(card.name, charge),
        refusal: refusals.find((refusal) => refusal.plan === price.plan)?.reason })) }))
    .filter((card) => card.prices.length > 0);
}

// priceFor is the card's price for the chosen interval, or its only price (Founding has one price, billed every 3 months).
export function priceFor(card: PlanCard, interval: Interval): PlanPrice {
  return card.prices.find((price) => price.interval === interval) ?? card.prices[0];
}
