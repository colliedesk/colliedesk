import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { RequestBody } from '../../api/client';
import { useCreateMutation } from '../../api/use-create';
import { can } from '../../lib/can';
import { messages } from '../../messages';
import { useBillingShown } from '../billing/billing-shown';
import { macroMessages } from './macro-messages';
import { MacrosSection } from './macros-section';
import { everyPage, SettingsFrame } from './settings-frame';

// Settings → Macros: a reply template plus the ticket actions it applies.
export function MacrosRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const reads = can(me.data, 'macros:read');
  const macros = useQuery({ queryKey: ['macros'], queryFn: api.macros, enabled: reads });
  const trash = useQuery({ queryKey: ['macros', 'trash'], queryFn: api.trashedMacros, enabled: reads });
  const people = useQuery({ queryKey: ['agents'], queryFn: api.agents, enabled: reads && can(me.data, 'team:read') });
  const tags = useQuery({ queryKey: ['tags'], queryFn: api.tags, enabled: reads && can(me.data, 'tags:read') });
  const refresh = () => Promise.all([
    client.invalidateQueries({ queryKey: ['macros'] }),
  ]);
  const createMacro = useCreateMutation({
    create: (value: RequestBody<'createMacro'>, key: string) => api.createMacro(value, key),
    onSuccess: () => { void refresh(); },
  });
  const updateMacro = useMutation({ mutationFn: ({ id, ...value }: { id: string } & RequestBody<'updateMacro'>) => api.updateMacro(id, value), onSuccess: () => { void refresh(); } });
  const deleteMacro = useMutation({ mutationFn: ({ id, updatedAt }: { id: string; updatedAt: string }) => api.deleteMacro(id, updatedAt), onSuccess: () => { void refresh(); } });
  const restoreMacro = useCreateMutation({ create: (id: string, key: string) => api.restoreMacro(id, key), onSuccess: () => { void refresh(); } });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  return (
    <SettingsFrame page="/settings/macros" title={messages.macros} lead={macroMessages.lead} loading={me.isPending || (reads && macros.isPending)} shown={{ ...everyPage, billing: billingShown }}>
      {reads && <MacrosSection macros={macros.data?.items ?? []} trash={trash.data?.items ?? []}
        people={people.data?.items ?? []} tags={tags.data?.items ?? []}
        onCreate={(value) => createMacro.mutateAsync(value)}
        onUpdate={(id, value) => updateMacro.mutateAsync({ id, ...value })}
        onDelete={(id, updatedAt) => deleteMacro.mutateAsync({ id, updatedAt })}
        onRestore={(id) => restoreMacro.mutateAsync(id)}
        onReload={() => { void refresh(); }} />}
    </SettingsFrame>
  );
}
