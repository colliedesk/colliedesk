import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, PlanLimitError } from '../../api/client';
import { useCreateMutation } from '../../api/use-create';
import type { Invite } from '../../api/schemas';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';

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
  // roles, and the server keeps both: the old pending link works until it is revoked, expires or is used. So the old
  // one is revoked after the new one, when the person may revoke, and the notice says exactly what is true when that
  // did not happen. A refusal of the new invite (an expired invite on a full plan) shows where a refused invite shows
  // it and leaves the old one as it was. Resolves to whether the new link was made.
  const [notice, setNotice] = useState<{ kind: 'status' | 'alert'; text: string }>();
  const resend = async (invite: Invite, mayRevoke: boolean): Promise<boolean> => {
    setNotice(undefined);
    try {
      await create.mutateAsync({ email: invite.email, roleIds: invite.roles.map((role) => role.id) });
    } catch {
      return false;
    }
    const pending = !invite.used_at && !invite.revoked_at && Date.parse(invite.expires_at) > Date.now();
    if (!pending) setNotice({ kind: 'status', text: settingsMessages.newLinkMade(invite.email) });
    else if (!mayRevoke) setNotice({ kind: 'status', text: settingsMessages.newLinkEarlierWorks });
    else {
      try {
        await api.revokeInvite(invite.id);
        setNotice({ kind: 'status', text: settingsMessages.newLinkMade(invite.email) });
      } catch {
        setNotice({ kind: 'alert', text: settingsMessages.newLinkOldWorks });
      }
    }
    await refresh();
    return true;
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
      inviteNotice: notice,
      onResendInvite: resend,
    },
  };
}
