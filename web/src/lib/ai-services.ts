// The AI services a person picks from, in the workspace's Add provider and in the operator console's server
// provider: Anthropic, two OpenAI-compatible services whose address is filled in, and any other
// OpenAI-compatible server, whose address is typed. No messages here, so both bundles can import it.
export type AIService = 'anthropic' | 'openai' | 'openrouter' | 'self_hosted';
export const aiServices: AIService[] = ['anthropic', 'openai', 'openrouter', 'self_hosted'];
export const aiServiceURL: Partial<Record<AIService, string>> = { openai: 'https://api.openai.com/v1', openrouter: 'https://openrouter.ai/api/v1' };

export function aiServiceKind(service: AIService): 'anthropic' | 'openai_compatible' {
  return service === 'anthropic' ? 'anthropic' : 'openai_compatible';
}

// aiServiceOf names the service a saved kind and address are: an address that is not one of the services'
// own (a trailing slash aside) is a self-hosted server.
export function aiServiceOf(kind: string, baseURL: string): AIService {
  const base = baseURL.trim().replace(/\/+$/, '');
  if (kind === 'anthropic' || (kind === '' && base === '')) return 'anthropic';
  for (const service of aiServices) if (aiServiceURL[service] === base) return service;
  return 'self_hosted';
}

// aiModelMatches finds a model whatever the punctuation, spacing, case or word order typed:
// "gpt5 mini", "GPT-5-mini" and "mini gpt5" all find gpt-5-mini; "sonnet 4.6" finds claude-sonnet-4-6.
export function aiModelMatches(model: string, query: string): boolean {
  const compact = (text: string) => text.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '');
  const name = compact(model);
  const words = query.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  return name.includes(compact(query)) || words.every((word) => name.includes(word));
}
