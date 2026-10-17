import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';

// The same poll interval as inbox activity: an open tab learns of a new request within 20 seconds.
const pollMilliseconds = 20_000;

export const pendingRequestsKey = ['agent-requests', 'pending'] as const;

// usePendingRequests is the workspace's waiting agent requests, shared by the rail badge, the inbox
// (reply drafts) and the Requests page.
export function usePendingRequests(enabled = true) {
  return useQuery({ queryKey: pendingRequestsKey, queryFn: () => api.agentRequests('pending'), enabled,
    refetchInterval: pollMilliseconds, refetchIntervalInBackground: true });
}
