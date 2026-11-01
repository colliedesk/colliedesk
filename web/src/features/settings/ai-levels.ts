import type { AILevel, AISlotName } from '../../api/ai';

// What an AI feature does by itself (docs/user/ai.md "What AI does by itself, per channel"), in the
// words Settings → Built-in AI and a channel's AI tab share.

export const levelsOf = (slot: AISlotName): AILevel[] => (slot === 'draft' ? ['off', 'suggest'] : ['off', 'suggest', 'auto']);

export const levelName: Record<AILevel, string> = { off: 'Off', suggest: 'Suggest', auto: 'Auto' };

export const levelMeaning: Record<AISlotName, Partial<Record<AILevel, string>>> = {
  triage: { off: 'Only when someone presses Suggest', suggest: 'Suggests for each new conversation', auto: 'Applies the picked parts by itself, with Undo' },
  draft: { off: 'Only Draft with AI', suggest: 'Writes a draft when a customer writes; AI never sends' },
  intake: { off: 'The channel’s rules decide', suggest: 'The rules decide; AI suggests in Review', auto: 'AI decides what is clear; otherwise the rules' },
  // The help center's chat is on or off; it has no level of its own.
  chat: {},
};

export const featureName: Record<AISlotName, string> = { triage: 'Triage', draft: 'Reply drafts', chat: 'AI chat', intake: 'Intake' };

// scopeName names a scope of a channel's type; '' is the whole channel.
export const scopeName = (scope: string) => (scope === '' ? 'Whole channel' : scope === 'direct' || scope === 'user' ? 'One-to-one' : scope === 'group' ? 'Groups'
  : scope.charAt(0).toUpperCase() + scope.replaceAll('_', ' ').slice(1));
