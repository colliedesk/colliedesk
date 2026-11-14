import type { AISettings, AISlot, AISlotInput } from '../../api/ai';

// A choice of provider is "server" or a provider id.
export const sourceOf = (slot: AISlot) => (slot.source === 'server' ? 'server' : slot.provider_id ?? '');

// slotChoices is what a feature may run on: the server's provider (when the operator offers it models for this
// feature) and the workspace's own providers, with the one the feature is set to.
export function slotChoices(slot: AISlot, settings: AISettings) {
  const serverModels = settings.server?.models[slot.slot] ?? [];
  const chosen = sourceOf(slot);
  const choices = [
    ...(serverModels.length > 0 && settings.server ? [{ value: 'server', label: settings.server.label, models: serverModels }] : []),
    ...settings.providers.map((provider) => ({ value: provider.id, label: provider.label, models: provider.models })),
  ];
  const choice = choices.find((item) => item.value === chosen);
  // While the operator switched the server's provider off, nobody can pick it; a feature already set to
  // it still shows it, greyed, with why.
  const serverOff = chosen === 'server' && !settings.server && settings.server_off ? settings.server_off : null;
  return { serverModels, chosen, choices, choice, models: choice?.models ?? (slot.model ? [slot.model] : []), serverOff };
}

// slotChange is the request that moves a feature to another provider or model, or turns it on or off; whatever
// it leaves out stays as it was.
export function slotChange(slot: AISlot, chosen: string, next: { source?: string; model?: string; enabled?: boolean }): AISlotInput {
  const source = next.source ?? chosen;
  const input: AISlotInput = source === 'server' ? { source: 'server', enabled: next.enabled ?? slot.enabled }
    : source ? { source: 'provider', provider_id: source, enabled: next.enabled ?? slot.enabled } : { source: 'none', enabled: false };
  const model = next.model ?? (next.source ? undefined : slot.model ?? undefined);
  if (model && input.source === 'provider') input.model = model;
  input.effort = slot.effort;
  return input;
}
