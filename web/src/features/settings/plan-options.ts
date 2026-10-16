import type { CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';

export interface PlanPrice { plan: CheckoutPlan; price: string; label: string }
export interface PlanCard { name: string; includes: string[]; prices: PlanPrice[] }

// What each plan includes; the numbers match docs/user/billing.md and the server's plan limits.
const allPlans: { name: string; includes: string[]; prices: { plan: CheckoutPlan; price: string }[] }[] = [
  { name: messages.plans.team, includes: [messages.planAgents(10), messages.planStorage('100 GiB')],
    prices: [{ plan: 'team_monthly', price: messages.priceTeamMonthly }, { plan: 'team_yearly', price: messages.priceTeamYearly }] },
  { name: messages.plans.team25, includes: [messages.planAgents(25), messages.planStorage('250 GiB')],
    prices: [{ plan: 'team25_monthly', price: messages.priceTeam25Monthly }, { plan: 'team25_yearly', price: messages.priceTeam25Yearly }] },
  { name: messages.plans.founding, includes: [messages.planAgents(10), messages.planStorage('20 GiB'), messages.foundingLimited],
    prices: [{ plan: 'founding', price: messages.priceFounding }] },
];

// The plans a workspace can buy, in the order they are offered: only the prices the server says it
// can sell, and only plans with at least one of them. A price's label names the plan for screen readers.
export function planCardsFor(sellable: readonly CheckoutPlan[]): PlanCard[] {
  return allPlans
    .map((card) => ({ ...card, prices: card.prices.filter((price) => sellable.includes(price.plan)).map((price) => ({ ...price, label: `${card.name}, ${price.price}` })) }))
    .filter((card) => card.prices.length > 0);
}
