import type { CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';

// The plans a workspace can buy, in the order they are offered.
export const planOptions: { plan: CheckoutPlan; label: string }[] = [
  { plan: 'team_monthly', label: messages.upgradeTeamMonthly },
  { plan: 'team_yearly', label: messages.upgradeTeamYearly },
  { plan: 'founding', label: messages.upgradeFounding },
];
