import { PlanLimitError } from '../../api/client';

// The next step after an AI draft or suggestion fails for a setting. Shared by both lazy parts.

const text = { openSettings: 'Open AI settings', askAdmin: 'Ask an admin to check Settings → Built-in AI.' };

// Failures a setting causes: the provider's key or credit, the included AI, the feature turned off.
const settingsCodes = ['auth', 'permission', 'quota_exhausted', 'model_not_found', 'ai_allowance_reached', 'feature_off', 'ai_feature_off'];

// isSettingsFailure says whether a setting causes the failure, so trying again fails the same way.
export function isSettingsFailure(code: string | null): boolean {
  return Boolean(code && settingsCodes.includes(code));
}

// refusalCode is the error code of a refused request.
export function refusalCode(error: unknown): string | null {
  const detail = (error as { detail?: { code?: unknown } } | null)?.detail;
  if (typeof detail?.code === 'string') return detail.code;
  // A 402 carries no detail: the included AI is used up.
  return error instanceof PlanLimitError ? 'ai_allowance_reached' : null;
}

// SettingsWay is the next step for a failure a setting causes: Settings → Built-in AI for an admin, ask an admin otherwise.
export function SettingsWay({ code, canManage }: { code: string | null; canManage: boolean }) {
  if (!isSettingsFailure(code)) return null;
  return canManage ? <a href="/settings/ai" target="_blank" rel="noopener">{text.openSettings}</a> : <span>{text.askAdmin}</span>;
}
