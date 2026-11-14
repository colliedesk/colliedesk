import type { AISlot, AISlotInput } from '../../api/ai';

// slotInput is the slot as it is, to change one thing of it: Settings → Built-in AI and the setup flow turn a
// feature on or off with it, so its source, provider, model and effort stay as they were.
export function slotInput(slot: AISlot): AISlotInput {
  const input: AISlotInput = slot.source === 'server' ? { source: 'server', enabled: slot.enabled }
    : slot.provider_id ? { source: 'provider', provider_id: slot.provider_id, enabled: slot.enabled } : { source: 'none', enabled: false };
  // The server's provider runs the model its operator picks; only a workspace provider takes one.
  if (slot.model && input.source === 'provider') input.model = slot.model;
  input.effort = slot.effort;
  return input;
}
