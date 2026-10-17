import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { ApproveOutcome } from './request-card';
import { RequestsPage } from './requests-page';
import type { RequestFilter } from './requests-page';
import { pendingRequestsKey } from './use-pending-requests';

export function RequestsRoute() {
  const client = useQueryClient();
  const [filter, setFilter] = useState<RequestFilter>('pending');
  const [approved, setApproved] = useState<ApproveOutcome[]>([]);
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const list = useQuery({ queryKey: filter === 'pending' ? pendingRequestsKey : ['agent-requests', 'all'],
    queryFn: () => api.agentRequests(filter === 'pending' ? 'pending' : undefined), refetchInterval: 20_000 });
  // An approved call may have changed conversations, messages or settings anywhere in the app.
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['agent-requests'] });
    void client.invalidateQueries({ queryKey: ['conversations'] });
    void client.invalidateQueries({ queryKey: ['messages'] });
  };
  return <RequestsPage items={list.data?.items} approved={approved} filter={filter} onFilter={setFilter} timezone={me.data?.timezone ?? 'UTC'}
    loading={list.isPending} failed={list.isError}
    onApprove={async (id) => {
      const outcome = await api.approveAgentRequest(id);
      setApproved((current) => [outcome, ...current]);
      refresh();
      return outcome;
    }}
    onReject={async (id, reason) => { await api.rejectAgentRequest(id, reason); refresh(); }} />;
}
