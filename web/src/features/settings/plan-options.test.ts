import { expect, test } from 'vitest';
import type { CheckoutPlan } from '../../api/schemas';
import { isFoundingOffer, planCardsFor } from './plan-options';

const ladder: CheckoutPlan[] = ['solo_monthly', 'solo_yearly', 'small_monthly', 'small_yearly', 'team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly'];
const foundingOpen: CheckoutPlan[] = [...ladder, 'founding'];
const names = (sellable: CheckoutPlan[], refusals: { plan: CheckoutPlan; reason: string }[] = [], preferred?: 'team25') =>
  planCardsFor(sellable, refusals, preferred).map((card) => card.name);
const foundingRefused = [{ plan: 'founding' as const, reason: 'Founding allows up to 10 agents.' }];

test('while Founding is open the cards are Founding and Team 25, and Solo, Small and Team stay hidden', () => {
  expect(names(foundingOpen)).toEqual(['Founding', 'Team 25']);
});

test('Team 25 carries both its prices and Founding its one', () => {
  const [founding, team25] = planCardsFor(foundingOpen);
  expect(founding.prices.map((price) => price.plan)).toEqual(['founding']);
  expect(team25.prices.map((price) => [price.plan, price.amount, price.per, price.note])).toEqual([
    ['team25_monthly', '$149', 'a month', undefined], ['team25_yearly', '$1,490', 'a year', '2 months free']]);
  expect(founding.prices.map((price) => [price.amount, price.per, price.note])).toEqual([['$99', 'every 3 months', 'Price locked for life']]);
});

test('Team 25 is offered only when the server can sell it', () => {
  expect(names(['team_monthly', 'team_yearly', 'founding'])).toEqual(['Founding']);
  expect(names(['team_monthly', 'founding', 'team25_yearly'])).toEqual(['Founding', 'Team 25']);
});

test('a workspace Founding does not fit sees Founding greyed with its reason and Team 25 choosable', () => {
  const [founding, team25] = planCardsFor(foundingOpen, foundingRefused);
  expect([founding.name, team25.name]).toEqual(['Founding', 'Team 25']);
  expect(founding.prices[0].refusal).toBe('Founding allows up to 10 agents.');
  expect(team25.prices.map((price) => price.refusal)).toEqual([undefined, undefined]);
});

test('when nothing in the Founding offer can be bought the whole ladder is back, so the buyer is never left with nothing', () => {
  expect(names([...ladder.filter((plan) => !plan.startsWith('team25')), 'founding'], foundingRefused)).toEqual(['Solo', 'Small', 'Team', 'Founding']);
});

test('once Founding is closed the whole ladder is offered, smallest first', () => {
  expect(names(ladder)).toEqual(['Solo', 'Small', 'Team', 'Team 25']);
});

test('the plan a visitor came for comes first, whatever mode the cards are in', () => {
  expect(names(foundingOpen, [], 'team25')).toEqual(['Team 25', 'Founding']);
  expect(names(ladder, [], 'team25')).toEqual(['Team 25', 'Solo', 'Small', 'Team']);
});

test('a plan nobody sells is not put first', () => {
  expect(names(['team_monthly', 'team_yearly', 'founding'], [], 'team25')).toEqual(['Founding']);
});

test('each plan says which teams it is for, and the plans of the Founding offer carry the promise of their import', () => {
  const [founding, team25] = planCardsFor(foundingOpen);
  expect([founding.audience, team25.audience]).toEqual(['For teams of 3 to 10 agents', 'For teams of 11 to 25 agents']);
  expect(founding.includes).toEqual(['Up to 10 agents', '20 GiB attachment storage', 'Migration done for you', '30-day money-back guarantee']);
  expect(team25.includes).toEqual(['Up to 25 agents', '250 GiB attachment storage', 'Import done with you']);
});

test('the Founding offer is the two cards, and the ladder is not', () => {
  expect(isFoundingOffer(planCardsFor(foundingOpen))).toBe(true);
  expect(isFoundingOffer(planCardsFor(['team_monthly', 'founding']))).toBe(true);
  expect(isFoundingOffer(planCardsFor(ladder))).toBe(false);
  expect(isFoundingOffer(planCardsFor([...ladder.filter((plan) => !plan.startsWith('team25')), 'founding'], foundingRefused))).toBe(false);
});
