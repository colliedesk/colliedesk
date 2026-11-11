import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { AnsweredWithoutSecret, useCreateMutation } from '../../api/use-create';
import type { AgentToken } from '../../api/schemas';
import { can } from '../../lib/can';
import { messages } from '../../messages';
import { AgentTokensSection, followableRoles } from './agent-tokens-section';
import type { TokenChoice } from './agent-tokens-section';
import { SessionsSection } from './sessions-section';

// AIAgentsSections is the part of Settings → AI & agents for AI agents working through a token: the card
// that creates a token, then the sessions those tokens opened, where one is revoked. They were sections of
// the one Settings page, above its Sessions.
export function AIAgentsSections() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  // Where an AI app signs in with OAuth: APP_ORIGIN's /mcp (on Cloud, one address for every workspace).
  const mcpAddress = `${config.data?.app_origin || window.location.origin}/mcp`;
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: api.sessions });
  const creates = can(me.data, 'team:create_tokens') && !me.data?.token;
  // The roles a token may follow; asked for only where a token can be made.
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => api.roles(), enabled: creates && can(me.data, 'team:read') });
  const refresh = (key: string) => client.invalidateQueries({ queryKey: [key] });
  // A mutation, so that a 401 or 402 signs out or locks the app as any action does. The new token is in no
  // cache: keep hands it to the card, and the mutation's own result is empty. Sessions reloads to list it,
  // also after a failure whose outcome is unknown, so a token the server did create shows up to be revoked.
  // A repeat of a press the server already answered comes back without the token (shown once): the card
  // then points to Sessions, where the token is listed to revoke.
  const createToken = useCreateMutation({
    create: async ({ label, choice, keep }: { label: string; choice: TokenChoice; keep: (created: AgentToken & { token: string }) => void }, key: string) => {
      const created = await api.createAgentToken(label, choice, key);
      if (!created.token) throw new AnsweredWithoutSecret();
      keep({ ...created, token: created.token });
    },
    // Roles name the tokens that follow them (Team warns with those names before a role is taken).
    onSettled: () => { void refresh('sessions'); void refresh('roles'); },
  });
  // A revoked token no longer follows its role: the roles list, which names those tokens, reloads too.
  const revokeSession = useMutation({ mutationFn: api.revokeSession, onSuccess: () => { void refresh('sessions'); void refresh('me'); void refresh('roles'); } });
  const tokens = (sessions.data?.items ?? []).filter((session) => session.token);
  if (!me.data) return null;
  return <>
    {revokeSession.isError && <p role="alert" className="error-copy">{messages.settingsActionFailed}</p>}
    {creates && <AgentTokensSection roles={followableRoles(me.data.agent.roles, roles.data?.items, me.data.permissions)} held={me.data.permissions} mcpAddress={mcpAddress}
      rolePermissions={roles.data ? new Map(roles.data.items.map((role) => [role.id, role.permissions])) : undefined}
      onCreate={(label, choice) => new Promise((keep, reject) => { createToken.mutateAsync({ label, choice, keep }).catch(reject); })} />}
    {(creates || tokens.length > 0) && <SessionsSection kind="tokens" sessions={tokens} timezone={me.data.timezone ?? 'UTC'} onRevokeSession={(id) => revokeSession.mutate(id)} />}
  </>;
}
