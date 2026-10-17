import type { AgentRequest } from '../../api/schemas';

interface ReplyCall { text: string; cc: string[] }

// replyCall reads a reply draft's call: its text and the addresses it copies. Undefined for any other request.
export function replyCall(call: AgentRequest['body']): ReplyCall | undefined {
  if (typeof call !== 'object' || call === null || Array.isArray(call)) return undefined;
  const { body, cc } = call as { body?: unknown; cc?: unknown };
  if (typeof body !== 'string') return undefined;
  return { text: body, cc: Array.isArray(cc) ? cc.filter((item): item is string => typeof item === 'string') : [] };
}

// canApproveRequest says whether the signed-in person may approve item: an owner request only by the
// person the token belongs to, an admin request only by an admin, any other by everyone.
export function canApproveRequest(item: AgentRequest, person: { id?: string; role?: string }): boolean {
  if (item.approvable_by === 'owner') return item.requested_by.agent_id === person.id;
  return person.role === 'admin' || item.approvable_by === 'agent';
}
