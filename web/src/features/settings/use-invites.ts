import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, PlanLimitError } from '../../api/client';
import { useCreateMutation } from '../../api/use-create';
import { messages } from '../../messages';

// useInvites is the invite list with its create and revoke, for Settings and the setup flow alike. A change
// reloads the list and the setup progress, whose invite step it moves. The link of a created invite is in the
// create's answer only: it stays on screen while the component that called this is mounted.
export function useInvites() {
  const client = useQueryClient();
  const invites = useQuery({ queryKey: ['invites'], queryFn: api.invites });
  const refresh = () => Promise.all(['invites', 'setup'].map((key) => client.invalidateQueries({ queryKey: [key] })));
  const create = useCreateMutation({ create: ({ email, role }: { email: string; role: 'admin' | 'agent' }, key: string) => api.createInvite(email, role, key), onSuccess: refresh });
  const revoke = useMutation({ mutationFn: api.revokeInvite, onSuccess: refresh });
  return {
    invites,
    revokeFailed: revoke.isError,
    // What InvitesSection takes, besides who may create invites.
    section: {
      invites: invites.data?.items ?? [],
      inviteError: create.error instanceof PlanLimitError || create.error instanceof InvalidRequestError ? create.error.message : create.isError ? messages.settingsActionFailed : undefined,
      createdInvite: create.data ? { url: create.data.url } : undefined,
      creatingInvite: create.isPending,
      onCreateInvite: (email: string, role: 'admin' | 'agent') => create.mutate({ email, role }),
      onRevokeInvite: (id: string) => revoke.mutate(id),
    },
  };
}
