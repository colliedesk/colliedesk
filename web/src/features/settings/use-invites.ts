import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, PlanLimitError } from '../../api/client';
import { useCreateMutation } from '../../api/use-create';
import type { Invite } from '../../api/schemas';
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
  // The server does not email an invite: its link is shown once. A new link is a new invite for the same address and
  // roles; the old pending one is revoked after it, so the address never holds two live links. A refusal of the new
  // invite (a full plan, say) leaves the old one as it was and shows its reason where a refused invite shows it.
  const resend = async (invite: Invite, revokeOld: boolean) => {
    try {
      await create.mutateAsync({ email: invite.email, roleIds: invite.roles.map((role) => role.id) });
    } catch {
      return;
    }
    if (revokeOld) await revoke.mutateAsync(invite.id).catch(() => undefined);
  };
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
      onResendInvite: (invite: Invite, revokeOld: boolean) => { void resend(invite, revokeOld); },
    },
  };
}
