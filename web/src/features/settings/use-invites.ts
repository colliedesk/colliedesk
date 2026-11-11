import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, PlanLimitError } from '../../api/client';
import { useCreateMutation } from '../../api/use-create';
import { messages } from '../../messages';

// useInvites is the invite list with its create and revoke, for Settings and the setup flow alike. A change
// reloads the list and the setup progress, whose invite step it moves. The link of a created invite is in the
// create's answer only: it stays on screen while the component that called this is mounted. enabled is
// false for a person whose roles may not read the team: nothing is asked for.
export function useInvites(enabled = true) {
  const client = useQueryClient();
  const invites = useQuery({ queryKey: ['invites'], queryFn: api.invites, enabled });
  // The roles an invite may give, as Settings lists them.
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => api.roles(), enabled });
  const refresh = () => Promise.all(['invites', 'setup'].map((key) => client.invalidateQueries({ queryKey: [key] })));
  const create = useCreateMutation({ create: ({ email, roleIds }: { email: string; roleIds: string[] }, key: string) => api.createInvite(email, roleIds, key), onSuccess: refresh });
  const revoke = useMutation({ mutationFn: api.revokeInvite, onSuccess: refresh });
  return {
    invites,
    roles,
    revokeFailed: revoke.isError,
    // What InvitesSection takes, besides who may create invites.
    section: {
      invites: invites.data?.items ?? [],
      roles: roles.data?.items,
      inviteError: create.error instanceof PlanLimitError || create.error instanceof InvalidRequestError ? create.error.message : create.isError ? messages.settingsActionFailed : undefined,
      createdInvite: create.data ? { url: create.data.url } : undefined,
      creatingInvite: create.isPending,
      onCreateInvite: (email: string, roleIds: string[]) => create.mutate({ email, roleIds }),
      onRevokeInvite: (id: string) => revoke.mutate(id),
    },
  };
}
