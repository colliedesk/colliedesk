import { useCallback, useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ConflictError, InvalidRequestError } from '../../api/client';
import { aiApi } from '../../api/ai';
import type { AIProviderInput, AISettings, AISlotInput, AISlotName } from '../../api/ai';
import { aiMessages as m } from './ai-messages';
import { AIPage } from './ai-page';
import type { FieldError } from './ai-page';
import { useCreateAction } from '../../api/use-create';
import { AIChatSettingsRoute } from './ai-chat-route';
import { useBillingShown } from '../billing/billing-shown';
import { useIntakeSummary } from '../review/use-intake-summary';

const consentKey = 'colliedesk.ai-consent-dismissed';

// The banner's "Not now" is remembered in this browser only: a convenience, never the consent itself.
function readDismissed(workspaceID: string): boolean {
  try {
    return window.localStorage.getItem(`${consentKey}.${workspaceID}`) === '1';
  } catch {
    return false;
  }
}

function writeDismissed(workspaceID: string) {
  try {
    window.localStorage.setItem(`${consentKey}.${workspaceID}`, '1');
  } catch {
    // Without storage the banner comes back next time; nothing else depends on it.
  }
}

// refusal turns a failed action into the message to show and the field it is about.
export function refusal(error: unknown): FieldError {
  if (error instanceof ConflictError) {
    const code = error.detail?.code;
    if (code === 'ai_check_failed') {
      const why = error.detail?.check_error_code;
      return { field: why === 'blocked_address' ? 'base_url' : why === 'model_not_found' ? 'models' : 'api_key', text: error.message };
    }
    if (code === 'ai_provider_name_taken') return { field: 'label', text: error.message };
    if (code === 'ai_checks_rate_limited') return { field: 'form', text: m.checksLimited };
  }
  if (error instanceof InvalidRequestError) return { field: 'form', text: error.message };
  return { field: 'form', text: m.actionFailed };
}

// The banner shows on a server with a provider until a feature was set once or someone chose "Not now".
const neverSet = (settings?: AISettings) => Boolean(settings?.server) && Boolean(settings?.slots.every((slot) => !slot.enabled && !slot.updated_at));

// AIRoute is Settings → Built-in AI and a chat's transcript below it: path is what follows /settings/ai
// ("chats/<id>", or "conversations/<id>/chat": the chat that handed off to a conversation).
export function AIRoute({ path = '' }: { path?: string } = {}) {
  const navigate = useNavigate();
  const go = useCallback((href: string) => void navigate({ href }), [navigate]);
  // tab=chat opens the AI chat tab, where a transcript goes back to.
  const tab = new URLSearchParams(window.location.search).get('tab') === 'chat' ? 'chat' : undefined;
  const chat = /^chats\/([^/]+)$/.exec(path)?.[1];
  if (chat) return <AIChatSettingsRoute id={decodeURIComponent(chat)} navigate={go} />;
  const conversation = /^conversations\/([^/]+)\/chat$/.exec(path)?.[1];
  if (conversation) return <AIChatSettingsRoute conversation={decodeURIComponent(conversation)} navigate={go} />;
  return <AISettingsRoute tab={tab} onOpenChat={(id) => go(`/settings/ai/chats/${encodeURIComponent(id)}`)} />;
}

function AISettingsRoute({ tab, onOpenChat }: { tab?: 'chat'; onOpenChat: (id: string) => void }) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const settings = useQuery({ queryKey: ['ai-settings'], queryFn: aiApi.settings, enabled: Boolean(me.data) });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
  const billingShown = useBillingShown(isAdmin);
  // Intake has a row once the workspace has a chat channel that collects messages (the rail's summary).
  const chatChannels = useIntakeSummary().data?.available ?? false;
  const workspaceID = me.data?.workspace.id ?? '';
  const [dismissedNow, setDismissed] = useState(false);
  const dismissed = dismissedNow || (workspaceID !== '' && readDismissed(workspaceID));
  const refresh = () => client.invalidateQueries({ queryKey: ['ai-settings'] });
  const [busySlot, setBusySlot] = useState<AISlotName>();
  const slot = useMutation({
    mutationFn: ({ name, input }: { name: AISlotName; input: AISlotInput }) => aiApi.setSlot(name, input),
    onMutate: ({ name }) => setBusySlot(name), onSettled: () => { setBusySlot(undefined); void refresh(); },
  });
  const consent = useMutation({
    mutationFn: async () => {
      for (const name of ['triage', 'draft'] as const) {
        if ((settings.data?.server?.models[name] ?? []).length > 0) await aiApi.setSlot(name, { source: 'server', enabled: true });
      }
    },
    onMutate: () => setBusySlot('triage'), onSettled: () => { setBusySlot(undefined); void refresh(); },
  });
  const [busyProvider, setBusyProvider] = useState<string>();
  const check = useMutation({
    mutationFn: aiApi.check, onMutate: (id) => setBusyProvider(id), onSettled: () => { setBusyProvider(undefined); void refresh(); },
  });
  // Dialog actions answer the dialog, which shows a refusal under its field.
  const addProvider = useCreateAction((input: AIProviderInput, key: string) => aiApi.add(input, key));
  const run = async (action: () => Promise<unknown>): Promise<FieldError | undefined> => {
    try {
      await action();
      await refresh();
      return undefined;
    } catch (error) {
      return refusal(error);
    }
  };
  const [usageMonth, setUsageMonth] = useState<string>();
  const usage = useQuery({ queryKey: ['ai-usage', usageMonth], queryFn: () => aiApi.usage(usageMonth ?? ''), enabled: isAdmin && Boolean(usageMonth) });
  const failed = [slot, consent, check].find((mutation) => mutation.isError)?.error;
  if (!me.data) return <main className="settings-page" aria-busy="true" />;
  const error = failed ? refusal(failed).text : undefined;
  return <AIPage chatChannels={chatChannels} isAdmin={isAdmin} workspaceName={me.data.workspace.name} timezone={me.data.timezone ?? 'UTC'}
    shown={{ general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin,
      billing: billingShown, importData: isAdmin, ai: true }}
    settings={settings.data} loadFailed={settings.isError} showConsent={!dismissed && neverSet(settings.data)}
    busySlot={busySlot} busyProvider={busyProvider} error={error}
    onSetSlot={(name, input) => { consent.reset(); check.reset(); slot.mutate({ name, input }); }}
    onConsent={() => { slot.reset(); check.reset(); consent.mutate(); }}
    onDismissConsent={() => { writeDismissed(workspaceID); setDismissed(true); }}
    onCheck={(id) => { slot.reset(); consent.reset(); check.mutate(id); }}
    onAdd={(input: AIProviderInput) => run(() => addProvider.run(input))}
    onListModels={async (input) => {
      try { return (await aiApi.models(input)).models; } catch (error) {
        // A provider that keeps no list (or will not show it to this key) is answered by typing the names.
        const why = error instanceof ConflictError && error.detail?.code === 'ai_check_failed' ? error.detail.check_error_code : undefined;
        return why && ['model_not_found', 'bad_request', 'invalid_response', 'permission'].includes(String(why))
          ? { field: 'models', text: (error as ConflictError).message } : refusal(error);
      }
    }}
    onSetKey={(id, key) => run(() => aiApi.setKey(id, key))}
    onRemove={(id) => run(() => aiApi.remove(id))}
    onSetAutomatic={(perDay, perMinute, draftPerDay) => run(() => aiApi.setAutomatic(perDay, perMinute, draftPerDay))}
    usage={usage.data} usageFailed={usage.isError} onUsageMonth={setUsageMonth} initialTab={tab} onOpenChat={onOpenChat}
    onSetChatCap={async (cap) => {
      const chat = settings.data?.slots.find((item) => item.slot === 'chat');
      if (!chat) throw new Error('no chat feature');
      // The limit is set with the feature as it is: where it runs, the model, effort and on or off.
      await aiApi.setSlot('chat', { source: chat.source, ...(chat.provider_id ? { provider_id: chat.provider_id } : {}), ...(chat.model ? { model: chat.model } : {}),
        effort: chat.effort, enabled: chat.enabled, monthly_cap_micro_usd: cap });
      await refresh();
    }} />;
}
