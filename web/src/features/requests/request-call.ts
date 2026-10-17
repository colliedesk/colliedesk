import type { AgentRequest } from '../../api/schemas';

interface ReplyCall { text: string; cc: string[] }

// replyCall reads a reply draft's call: its text and the addresses it copies. Undefined for any other request.
export function replyCall(call: AgentRequest['body']): ReplyCall | undefined {
  if (typeof call !== 'object' || call === null || Array.isArray(call)) return undefined;
  const { body, cc } = call as { body?: unknown; cc?: unknown };
  if (typeof body !== 'string') return undefined;
  return { text: body, cc: Array.isArray(cc) ? cc.filter((item): item is string => typeof item === 'string') : [] };
}
