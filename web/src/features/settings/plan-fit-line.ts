import { billingMessages } from './billing-messages';
import type { PlanCard } from './plan-options';
import { fitsPlan } from './plan-recommendation';
import type { Recommendation } from './plan-recommendation';

// fitLine is the sentence above a plan's button: why it fits this team, or why it does not. The reason of the
// plan that leads always shows. A plan the team is too big for says so in numbers; a plan the server refuses
// for another reason (the storage a workspace holds) shows the server's words.
export function fitLine(card: PlanCard, recommendation: Recommendation | undefined, refusal: string | undefined): string {
  if (!recommendation) return refusal ?? '';
  const { size, plan, pick } = recommendation;
  const team = size.source === 'freshdesk' ? billingMessages.fitFreshdesk(size.count) : billingMessages.fitSeats(size.count);
  if (!fitsPlan(card.id, size.count)) return billingMessages.fitTooSmall(card.agents, size.count);
  if (refusal) return refusal;
  if (card.id === plan) {
    if (pick === 'intent') return billingMessages.fitChose(card.name);
    return card.id === 'founding' ? `${team} ${billingMessages.fitCovers(card.name, card.agents)}` : team;
  }
  return pick === 'intent' ? `${team} ${billingMessages.fitAlso(card.name)}` : '';
}

// isGreyed is true for a plan this team cannot buy: too small, or refused by the server.
export function isGreyed(card: PlanCard, recommendation: Recommendation | undefined, refusal: string | undefined): boolean {
  return refusal !== undefined || (recommendation !== undefined && !fitsPlan(card.id, recommendation.size.count));
}
