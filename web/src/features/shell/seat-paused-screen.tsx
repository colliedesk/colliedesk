import { useMutation } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { api } from '../../api/client';
import { messages } from '../../messages';

// SeatPausedScreen replaces the view of an agent paused because the workspace moved to a plan with
// fewer agents than it has. A paused admin may be the one who holds the billing account, so an admin
// can still open the billing portal to change the plan.
export function SeatPausedScreen({ plan, isAdmin, onSignOut }: { plan: string; isAdmin: boolean; onSignOut: () => void }) {
  const portal = useMutation({ mutationFn: () => api.openBillingPortal(), onSuccess: (link) => window.location.assign(link.url) });
  // A 402 can swap the inbox for this page; focus would stay on a removed node, so the heading takes it.
  const title = useRef<HTMLHeadingElement>(null);
  useEffect(() => title.current?.focus(), []);
  return (
    <main className="settings-page panel locked-screen">
      <header>
        <h1 ref={title} tabIndex={-1}>{messages.seatPausedTitle}</h1>
        <p>{messages.seatPausedExplain(plan)}</p>
      </header>
      {portal.isError && <p role="alert" className="error-copy">{messages.billingActionFailed}</p>}
      {isAdmin && <div className="button-row"><button type="button" className="primary-action" onClick={() => portal.mutate()}>{messages.manageBilling}</button></div>}
      <section className="settings-card" aria-labelledby="paused-leave-heading">
        <div className="section-heading"><h2 id="paused-leave-heading">{messages.signOut}</h2></div>
        <div className="settings-body"><div className="button-row"><button type="button" onClick={onSignOut}>{messages.signOut}</button></div></div>
      </section>
    </main>
  );
}
