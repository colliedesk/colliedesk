import type { Conversation } from '../../api/schemas';
import { messages } from '../../messages';
import { ArrowUpIcon } from '../../ui/icons';

export function StatusLabel({ status }: { status: Conversation['status'] }) {
  return <span className={`status status-${status}`}>{messages.statuses[status]}</span>;
}

// Normal and low are the quiet default; only priorities that change the order of work get colour.
export function PriorityLabel({ priority }: { priority: Conversation['priority'] }) {
  const loud = priority === 'high' || priority === 'urgent';
  return <span className={`priority priority-${priority}`}>{loud && <ArrowUpIcon />}{messages.priorities[priority]}</span>;
}
