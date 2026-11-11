import type { CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';
import { billingMessages } from './billing-messages';

export type Interval = 'monthly' | 'yearly';
// amount is what one payment charges and per says for how long ("$99" every 3 months); note is the line under it.
// refusal is the server's reason this workspace cannot buy the price now; the button is then disabled.
export interface PlanPrice { plan: CheckoutPlan; interval: Interval; amount: string; per: string; note?: string; label: string; refusal?: string }
// id is the plan's name in the server's plans, which a plan intent also names.
export type PlanId = 'solo' | 'small' | 'team' | 'team25' | 'founding';
// agents is how many agents the plan holds; audience says which teams it is for.
export interface PlanCard { id: PlanId; name: string; agents: number; audience: string; includes: string[]; prices: PlanPrice[] }

// What each plan includes; the numbers match docs/user/billing.md and the server's plan limits.
// charge is what one payment costs; it names the button for screen readers.
const allPlans: { id: PlanId; name: string; agents: number; audience: string; includes: string[]; prices: (Omit<PlanPrice, 'label'> & { charge: string })[] }[] = [
  { id: 'solo', name: messages.plans.solo, agents: 1, audience: billingMessages.audienceSolo, includes: [billingMessages.planAgents(1), billingMessages.planStorage('10 GiB')], prices: [
    { plan: 'solo_monthly', interval: 'monthly', amount: '$19', per: billingMessages.perMonth, charge: '$19 per month' },
    { plan: 'solo_yearly', interval: 'yearly', amount: '$190', per: billingMessages.perYear, note: billingMessages.twoMonthsFree, charge: '$190 per year' }] },
  { id: 'small', name: messages.plans.small, agents: 3, audience: billingMessages.audienceSmall, includes: [billingMessages.planAgents(3), billingMessages.planStorage('30 GiB')], prices: [
    { plan: 'small_monthly', interval: 'monthly', amount: '$39', per: billingMessages.perMonth, charge: '$39 per month' },
    { plan: 'small_yearly', interval: 'yearly', amount: '$390', per: billingMessages.perYear, note: billingMessages.twoMonthsFree, charge: '$390 per year' }] },
  { id: 'team', name: messages.plans.team, agents: 10, audience: billingMessages.audienceTeam, includes: [billingMessages.planAgents(10), billingMessages.planStorage('100 GiB')], prices: [
    { plan: 'team_monthly', interval: 'monthly', amount: '$69', per: billingMessages.perMonth, charge: '$69 per month' },
    { plan: 'team_yearly', interval: 'yearly', amount: '$690', per: billingMessages.perYear, note: billingMessages.twoMonthsFree, charge: '$690 per year' }] },
  { id: 'team25', name: messages.plans.team25, agents: 25, audience: billingMessages.audienceTeam25, includes: [billingMessages.planAgents(25), billingMessages.planStorage('250 GiB'), billingMessages.team25Import], prices: [
    { plan: 'team25_monthly', interval: 'monthly', amount: '$149', per: billingMessages.perMonth, charge: '$149 per month' },
    { plan: 'team25_yearly', interval: 'yearly', amount: '$1,490', per: billingMessages.perYear, note: billingMessages.twoMonthsFree, charge: '$1,490 per year' }] },
  { id: 'founding', name: messages.plans.founding, agents: 10, audience: billingMessages.audienceFounding, includes: [billingMessages.planAgents(10), billingMessages.planStorage('20 GiB'), billingMessages.foundingMigration, billingMessages.moneyBack], prices: [
    { plan: 'founding', interval: 'yearly', amount: '$99', per: billingMessages.perQuarter, note: billingMessages.priceLocked, charge: '$99 per quarter for life' }] },
];

// While a Founding slot is free the website sells two plans, Founding and Team 25 (for 11 to 25 agents),
// and the app offers the same two, in that order.
const foundingOfferIds: readonly PlanId[] = ['founding', 'team25'];
const buyable = (card: PlanCard) => card.prices.some((price) => price.refusal === undefined);

// The plans a workspace can buy, in the order they are offered: only the prices the server says it
// can sell, and only plans with at least one of them. While Founding is open (the server offers it
// only until its slots are taken) the offer is Founding, greyed with its reason when this workspace
// does not fit it, and Team 25 when the server sells it; Solo, Small and Team wait. When this workspace
// can buy neither, it gets the whole ladder, which also comes back on its own once Founding closes.
// The plan that leads (the one a visitor came for, or the one that fits the team) comes first.
export function planCardsFor(sellable: readonly CheckoutPlan[], refusals: readonly { plan: CheckoutPlan; reason: string }[] = [], leading?: PlanId): PlanCard[] {
  const cards = allPlans
    .map((card) => ({ ...card, prices: card.prices.filter((price) => sellable.includes(price.plan))
      .map(({ charge, ...price }) => ({ ...price, label: billingMessages.choosePlanPrice(card.name, charge),
        refusal: refusals.find((refusal) => refusal.plan === price.plan)?.reason })) }))
    .filter((card) => card.prices.length > 0);
  const offer = cards.some((card) => card.id === 'founding') ? foundingOfferIds.flatMap((id) => cards.filter((card) => card.id === id)) : [];
  const shown = offer.some(buyable) ? offer : cards;
  const asked = shown.find((card) => card.id === leading);
  return asked ? [asked, ...shown.filter((card) => card !== asked)] : shown;
}

// isFoundingOffer is true when the cards are the Founding offer (Founding, with or without Team 25) and not the ladder.
export function isFoundingOffer(cards: readonly PlanCard[]): boolean {
  return cards.some((card) => card.id === 'founding') && cards.every((card) => foundingOfferIds.includes(card.id));
}

// priceFor is the card's price for the chosen interval, or its only price (Founding has one price, billed every 3 months).
export function priceFor(card: PlanCard, interval: Interval): PlanPrice {
  return card.prices.find((price) => price.interval === interval) ?? card.prices[0];
}

// agentsOf is how many agents a plan holds; 0 for a name that is no plan.
export const agentsOf = (plan: PlanId): number => allPlans.find((entry) => entry.id === plan)?.agents ?? 0;
