import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import { useBillingShown } from '../billing/billing-shown';
import { ComingCard } from './coming-card';
import { everyPage, SettingsFrame } from './settings-frame';
import { settingsMessages } from './settings-messages';

// Settings → Conversation fields: reserved. The menu holds its place and the page says what is coming.
export function FieldsRoute() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  return <SettingsFrame page="/settings/fields" title={settingsMessages.conversationFields} lead={settingsMessages.fieldsLead} loading={me.isPending} shown={{ ...everyPage, billing: billingShown }}>
    <ComingCard title={settingsMessages.fieldsComingTitle} body={settingsMessages.fieldsComingBody} issue={443} />
  </SettingsFrame>;
}
