import { useQuery } from '@tanstack/react-query';
import { Navigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import { usePhone } from '../../lib/use-phone';
import { messages } from '../../messages';
import { useBillingShown } from '../billing/billing-shown';
import { everyPage, SettingsFrame } from './settings-frame';
import { settingsMessages } from './settings-messages';

// The Settings chunk: one page per item of the settings menu (#528), each asking only for what it shows.
// AI & agents, Data, a role's page, Channels, Response targets and Plan and billing are chunks of their own.
export { AccountRoute } from './account-route';
export { GeneralRoute } from './general-route';
export { MembersRoute } from './members-route';
export { FieldsRoute } from './fields-route';
export type { MembersTab } from './settings-anchors';
export { SavedRepliesRoute } from './saved-replies-route';

// SettingsIndexRoute is the menu itself as a page: /settings on a phone, where the menu and a page take
// turns on the screen as the inbox list and a conversation do. A wider screen opens Account instead (app.tsx),
// also when the phone is turned on its side while the menu is open.
export function SettingsIndexRoute() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  if (!usePhone()) return <Navigate to="/settings/account" replace />;
  return <SettingsFrame page="/settings" title={messages.settings} lead={settingsMessages.pageLeads.index} loading={me.isPending} shown={{ ...everyPage, billing: billingShown }} />;
}
