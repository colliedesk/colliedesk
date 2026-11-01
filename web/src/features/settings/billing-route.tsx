import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { api } from '../../api/client';
import { importsApi } from '../../api/imports';
import { useCreateMutation } from '../../api/use-create';
import { messages } from '../../messages';
import { billingMessages } from './billing-messages';
import { BillingPage } from './billing-page';
import { clearCheckoutIntent, readCheckoutIntent } from './checkout-intent';
import { readPendingCheckout } from './checkout-pending';
import { freshdeskAgentCount } from './freshdesk-agents';
import { readPlanIntent } from './plan-intent';
import { useCheckout } from './use-checkout';
import { hasSeenWelcome, markWelcomeSeen } from './welcome-seen';

interface BillingRouteProps {
  // checkoutReturned is true on the page the checkout sends the browser back to.
  checkoutReturned?: boolean;
}

// While the payment provider confirms a new subscription the plan is still free; poll briefly.
const confirmPollMs = 2000;

export function BillingRoute({ checkoutReturned = false }: BillingRouteProps) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
  const enabled = config.data?.billing.enabled === true;
  // Switched off by the operator: nothing is sold, but a workspace that has or had a subscription still sees
  // it and the portal that cancels it.
  const switchedOff = !enabled && config.data?.billing.switched_off === true;
  const billing = useQuery({
    queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin && (enabled || switchedOff),
    refetchInterval: (query) => (checkoutReturned && (query.state.data?.plan === 'trial' || query.state.data?.plan === 'none') ? confirmPollMs : false),
  });
  const unpaid = billing.data?.plan === 'trial' || billing.data?.plan === 'none';
  // The migration row of a paid Founding or Team 25 workspace says Done once a Freshdesk import has finished.
  const migrationPlan = billing.data?.plan === 'founding' || billing.data?.plan === 'team25';
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setupProgress, enabled: isAdmin && migrationPlan });
  const importDone = setup.isSuccess ? setup.data.steps.some((step) => step.key === 'import' && step.state === 'done') : setup.isError ? false : undefined;
  // What the plan picker needs to tell which plan fits: the agents of the Freshdesk account that was read, and the Founding places left.
  const imports = useQuery({ queryKey: ['imports'], queryFn: importsApi.list, enabled: isAdmin && unpaid && enabled });
  const founding = useQuery({ queryKey: ['founding'], queryFn: api.foundingSlots, enabled: isAdmin && unpaid && enabled, staleTime: 60_000 });
  const placesLeft = founding.data?.open && founding.data.taken !== null ? Math.max(0, founding.data.slots - founding.data.taken) : undefined;
  const { start, retry, starting, failed, openPortal, portalFailed } = useCheckout();
  // The same address and times, sent again after a failure, go under the same key, so the request is made once.
  const requestMigration = useCreateMutation({ create: api.requestMigration, onSuccess: () => client.invalidateQueries({ queryKey: ['billing'] }) });
  const [welcomeClosed, setWelcomeClosed] = useState(false);
  // A remembered plan is acted on once, as soon as it is known whether this admin can buy it; every
  // other outcome drops it, so it can never fire later or loop.
  const intentHandled = useRef(false);
  const cannotBuy = (config.data && !enabled) || (me.data && !isAdmin) || billing.isError;
  useEffect(() => {
    if (intentHandled.current || !(cannotBuy || billing.data)) return;
    const wanted = readCheckoutIntent();
    if (!wanted) return;
    intentHandled.current = true;
    clearCheckoutIntent();
    const offered = billing.data?.checkout_plans.find((plan) => plan === wanted);
    if (offered && unpaid && !checkoutReturned) start(offered);
  }, [billing.data, cannotBuy, checkoutReturned, start, unpaid]);
  const notEnabled = <main className="settings-page"><p className="muted-copy">{billingMessages.billingNotEnabled}</p></main>;
  if (config.data && !enabled && !switchedOff) return notEnabled;
  if (me.data && !isAdmin) return <main className="settings-page"><p className="muted-copy">{billingMessages.billingAdminsOnly(me.data.plan?.admins ?? [])}</p></main>;
  if (!billing.data) return <main className="settings-page">{billing.isError && <p role="alert" className="error-copy">{messages.billingActionFailed}</p>}</main>;
  if (switchedOff && billing.data.status === 'none') return notEnabled;
  const workspaceId = me.data?.workspace.id ?? '';
  const closeWelcome = () => { markWelcomeSeen(workspaceId); setWelcomeClosed(true); };
  return <BillingPage billing={billing.data} switchedOff={switchedOff} importDone={importDone} timezone={me.data?.timezone ?? 'UTC'} checkoutReturned={checkoutReturned}
    pendingPlan={checkoutReturned ? readPendingCheckout() : undefined} freshdeskAgents={imports.data ? freshdeskAgentCount(imports.data.items) : undefined} placesLeft={placesLeft}
    welcome={checkoutReturned && !welcomeClosed && !hasSeenWelcome(workspaceId)} onWelcomeDone={closeWelcome} preferredPlan={readPlanIntent()}
    checkoutFailed={failed} portalFailed={portalFailed} starting={starting} onCheckout={start} onRetryCheckout={retry} onCheckAgain={() => void billing.refetch()}
    onManage={() => openPortal()} onRequestMigration={(input) => requestMigration.mutateAsync(input)} />;
}
