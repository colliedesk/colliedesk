import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import { useScrollToHashWhenReady } from '../../lib/scroll-to-hash';
import { rememberSignOut } from '../../lib/sign-out-intent';
import { messages } from '../../messages';
import { useBillingShown } from '../billing/billing-shown';
import { saveState } from './save-feedback';
import { SettingsPage } from './settings-page';
import { MentionEmailSetting } from './mention-email-setting';
import { storeSkipped } from '../onboarding/setup-steps';

// Settings → Account: the profile, this browser's preferences, the devices signed in and the AI apps
// connected with OAuth. It asks for who is signed in and the sessions only.
export function AccountRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  // Where an AI app signs in with OAuth: APP_ORIGIN's /mcp (on Cloud, one address for every workspace).
  const mcpAddress = `${config.data?.app_origin || window.location.origin}/mcp`;
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: api.sessions });
  const refresh = (key: string) => client.invalidateQueries({ queryKey: [key] });
  // Revoking this device's own session is the person's Sign out: reloading me leads to the sign-in page, which
  // ends the identity provider's session too.
  const revokeSession = useMutation({ mutationFn: api.revokeSession, onSuccess: (_, id) => {
    if (sessions.data?.items.some((session) => session.current && session.id === id)) rememberSignOut();
    void refresh('sessions'); void refresh('me'); void refresh('roles');
  } });
  const agentTimezone = useMutation({ mutationFn: api.updateAgentTimezone, onSuccess: () => refresh('me') });
  // Your name also shows in the team list and the assignee lists.
  const agentDisplayName = useMutation({ mutationFn: api.updateAgentDisplayName, onSuccess: () => Promise.all([refresh('me'), refresh('agents')]) });
  // Showing the setup guide again shows all of Quick start, Try more too if this browser skipped it.
  const setupGuide = useMutation({ mutationFn: api.setSetupGuideHidden, onSuccess: (answer) => {
    if (!answer.hidden && me.data) storeSkipped(me.data.agent.id, false);
    return Promise.all([refresh('me'), refresh('setup')]);
  } });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  const loading = me.isPending || sessions.isPending;
  // A link to a section (a pasted address, an old anchor) arrives before the sections do.
  useScrollToHashWhenReady(!loading);
  return <SettingsPage page="account" loading={loading} mentionEmail={can(me.data, 'messages:read') ? <MentionEmailSetting /> : undefined} sessions={sessions.data?.items ?? []} timezone={me.data?.timezone ?? 'UTC'} showBilling={billingShown}
    personalTimezoneSave={saveState(agentTimezone)} email={me.data?.agent.email} displayName={me.data?.agent.display_name} displayNameSave={saveState(agentDisplayName)}
    onSetDisplayName={(name) => agentDisplayName.mutateAsync(name)}
    setupGuideHidden={me.data?.setup_guide_hidden} onSetSetupGuideHidden={(hidden) => setupGuide.mutateAsync(hidden)}
    workspaceTimezone={me.data?.workspace.timezone} timezoneOverride={me.data?.agent.timezone_override}
    error={revokeSession.isError ? messages.settingsActionFailed : undefined} mcpAddress={mcpAddress}
    onRevokeSession={(id) => revokeSession.mutate(id)} onSetAgentTimezone={(value) => agentTimezone.mutateAsync(value)} />;
}
