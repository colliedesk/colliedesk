import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';

// useBillingShown says whether an admin sees Plan and billing. With billing switched off by the operator a
// workspace that has or had a subscription still sees it, to read the plan and cancel it in the portal: a
// subscription can always be ended. One that never had one does not.
export function useBillingShown(isAdmin: boolean): boolean {
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const off = config.data?.billing.switched_off === true;
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin && off });
  return isAdmin && (config.data?.billing.enabled === true || (off && billing.data !== undefined && billing.data.status !== 'none'));
}
