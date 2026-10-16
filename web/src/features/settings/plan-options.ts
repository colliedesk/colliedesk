import type { CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';

const allPlanOptions: { plan: CheckoutPlan; label: string }[] = [
  { plan: 'team_monthly', label: messages.upgradeTeamMonthly },
  { plan: 'team_yearly', label: messages.upgradeTeamYearly },
  { plan: 'team25_monthly', label: messages.upgradeTeam25Monthly },
  { plan: 'team25_yearly', label: messages.upgradeTeam25Yearly },
  { plan: 'founding', label: messages.upgradeFounding },
];

// The plans a workspace can buy, in the order they are offered: only those the server says it can sell.
export function planOptionsFor(sellable: readonly CheckoutPlan[]): { plan: CheckoutPlan; label: string }[] {
  return allPlanOptions.filter((option) => sellable.includes(option.plan));
}
