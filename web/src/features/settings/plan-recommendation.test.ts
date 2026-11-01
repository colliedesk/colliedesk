import { expect, test } from 'vitest';
import type { PlanId } from './plan-options';
import { fitsPlan, recommendPlan } from './plan-recommendation';

const foundingOffer: PlanId[] = ['founding', 'team25'];
const ladder: PlanId[] = ['solo', 'small', 'team', 'team25'];

test('with no intent and no Freshdesk count, the smallest plan that holds the workspace seats leads', () => {
  expect(recommendPlan({ offer: foundingOffer, seats: 3 })).toEqual({ plan: 'founding', pick: 'size', size: { count: 3, source: 'seats' } });
  expect(recommendPlan({ offer: foundingOffer, seats: 10 }).plan).toBe('founding');
  expect(recommendPlan({ offer: foundingOffer, seats: 11 }).plan).toBe('team25');
  expect(recommendPlan({ offer: foundingOffer, seats: 25 }).plan).toBe('team25');
});

test('a team larger than every offered plan still leads with the largest one', () => {
  expect(recommendPlan({ offer: foundingOffer, seats: 40 })).toEqual({ plan: 'team25', pick: 'size', size: { count: 40, source: 'seats' } });
});

test('the agent count of a Freshdesk import beats the workspace seats', () => {
  expect(recommendPlan({ offer: foundingOffer, freshdeskAgents: 14, seats: 3 })).toEqual({ plan: 'team25', pick: 'size', size: { count: 14, source: 'freshdesk' } });
  expect(recommendPlan({ offer: foundingOffer, freshdeskAgents: 3, seats: 14 })).toEqual({ plan: 'founding', pick: 'size', size: { count: 3, source: 'freshdesk' } });
});

test('an import with no active agents is no count: the workspace seats decide', () => {
  expect(recommendPlan({ offer: foundingOffer, freshdeskAgents: 0, seats: 12 })).toEqual({ plan: 'team25', pick: 'size', size: { count: 12, source: 'seats' } });
});

test('the plan a visitor chose on the website leads whatever the size, and says it was their pick', () => {
  expect(recommendPlan({ offer: foundingOffer, intent: 'team25', seats: 3 })).toEqual({ plan: 'team25', pick: 'intent', size: { count: 3, source: 'seats' } });
  expect(recommendPlan({ offer: foundingOffer, intent: 'team25', freshdeskAgents: 14, seats: 3 })).toEqual({ plan: 'team25', pick: 'intent', size: { count: 14, source: 'freshdesk' } });
});

test('a plan fits when the team is no larger than its agents', () => {
  expect(fitsPlan('founding', 10)).toBe(true);
  expect(fitsPlan('founding', 11)).toBe(false);
  expect(fitsPlan('team25', 25)).toBe(true);
  expect(fitsPlan('team25', 26)).toBe(false);
});

test('the ladder leads with the smallest plan that holds the team, and Team 25 is the last resort', () => {
  expect(recommendPlan({ offer: ladder, seats: 1 }).plan).toBe('solo');
  expect(recommendPlan({ offer: ladder, seats: 2 }).plan).toBe('small');
  expect(recommendPlan({ offer: ladder, seats: 3 }).plan).toBe('small');
  expect(recommendPlan({ offer: ladder, seats: 4 }).plan).toBe('team');
  expect(recommendPlan({ offer: ladder, seats: 11 }).plan).toBe('team25');
  expect(recommendPlan({ offer: ladder, seats: 90 }).plan).toBe('team25');
});

test('a plan the server does not sell is never recommended, not even from the website intent', () => {
  expect(recommendPlan({ offer: ['founding'], intent: 'team25', seats: 3 }).plan).toBe('founding');
  expect(recommendPlan({ offer: ['founding'], seats: 30 }).plan).toBe('founding');
});
