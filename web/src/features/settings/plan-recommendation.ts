import type { PlanIntent } from './plan-intent';
import { agentsOf } from './plan-options';
import type { PlanId } from './plan-options';

// TeamSize is how many people the plan has to hold, and where that number came from: the agents of the
// Freshdesk account the workspace read, or the workspace's own seats (agents plus pending invites).
export interface TeamSize { count: number; source: 'freshdesk' | 'seats' }

export interface Recommendation {
  plan: PlanId;
  // pick says why the plan leads: the visitor chose it on the website, or it is the smallest one the team fits.
  pick: 'intent' | 'size';
  size: TeamSize;
}

// fitsPlan is true when a team of this size is within the plan's agents.
export function fitsPlan(plan: PlanId, count: number): boolean {
  return count <= agentsOf(plan);
}

// teamSize prefers the Freshdesk account's agent count (the team that is moving) to the workspace seats.
function teamSize(freshdeskAgents: number | undefined, seats: number): TeamSize {
  return freshdeskAgents !== undefined && freshdeskAgents > 0 ? { count: freshdeskAgents, source: 'freshdesk' } : { count: seats, source: 'seats' };
}

// recommendPlan chooses the plan to lead with among the plans on offer: the one the visitor chose on the
// website, else the smallest plan that holds the team (the first on offer when two hold the same), else the
// largest. It is the one place that decides "the smallest plan that fits", for the plan picker and for any
// other screen that has to name a plan for a team size.
export function recommendPlan({ offer, intent, freshdeskAgents, seats }: { offer: readonly PlanId[]; intent?: PlanIntent; freshdeskAgents?: number; seats: number }): Recommendation {
  const size = teamSize(freshdeskAgents, seats);
  if (intent && offer.includes(intent)) return { plan: intent, pick: 'intent', size };
  const bySize = [...offer].sort((a, b) => agentsOf(a) - agentsOf(b));
  const plan = bySize.find((candidate) => fitsPlan(candidate, size.count)) ?? bySize[bySize.length - 1];
  return { plan, pick: 'size', size };
}
