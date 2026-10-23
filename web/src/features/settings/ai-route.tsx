import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ConflictError, InvalidRequestError } from '../../api/client';
import { aiApi } from '../../api/ai';
import type { AIProviderInput, AISettings, AISlotInput, AISlotName } from '../../api/ai';
import { aiMessages as m } from './ai-messages';
import { AIPage } from './ai-page';
import type { FieldError } from './ai-page';

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

export function AIRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const settings = useQuery({ queryKey: ['ai-settings'], queryFn: aiApi.settings, enabled: Boolean(me.data) });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
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
  return <AIPage isAdmin={isAdmin} workspaceName={me.data.workspace.name} timezone={me.data.timezone ?? 'UTC'}
    shown={{ general: isAdmin, team: isAdmin, email: isAdmin, webForm: isAdmin, savedReplies: true, exportData: isAdmin,
      billing: isAdmin && config.data?.billing.enabled === true, importData: isAdmin, ai: true }}
    settings={settings.data} loadFailed={settings.isError} showConsent={!dismissed && neverSet(settings.data)}
    busySlot={busySlot} busyProvider={busyProvider} error={error}
    onSetSlot={(name, input) => { consent.reset(); check.reset(); slot.mutate({ name, input }); }}
    onConsent={() => { slot.reset(); check.reset(); consent.mutate(); }}
    onDismissConsent={() => { writeDismissed(workspaceID); setDismissed(true); }}
    onCheck={(id) => { slot.reset(); consent.reset(); check.mutate(id); }}
    onAdd={(input: AIProviderInput) => run(() => aiApi.add(input))}
    onSetKey={(id, key) => run(() => aiApi.setKey(id, key))}
    onRemove={(id) => run(() => aiApi.remove(id))}
    onSetAutomatic={(perDay, perMinute) => run(() => aiApi.setAutomatic(perDay, perMinute))}
    usage={usage.data} usageFailed={usage.isError} onUsageMonth={setUsageMonth} />;
}
