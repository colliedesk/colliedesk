import { useEffect, useRef } from 'react';
import { messages } from '../../messages';

// SeatPausedScreen replaces the view of an agent paused because the workspace moved to a plan with
// fewer agents than it has. The identity bar keeps its sign-out button.
export function SeatPausedScreen({ plan, onSignOut }: { plan: string; onSignOut: () => void }) {
  // A 402 can swap the inbox for this page; focus would stay on a removed node, so the heading takes it.
  const title = useRef<HTMLHeadingElement>(null);
  useEffect(() => title.current?.focus(), []);
  return (
    <main className="settings-page panel locked-screen">
      <header>
        <h1 ref={title} tabIndex={-1}>{messages.seatPausedTitle}</h1>
        <p>{messages.seatPausedExplain(plan)}</p>
      </header>
      <section className="settings-card" aria-labelledby="paused-leave-heading">
        <div className="section-heading"><h2 id="paused-leave-heading">{messages.signOut}</h2></div>
        <div className="settings-body"><div className="button-row"><button type="button" onClick={onSignOut}>{messages.signOut}</button></div></div>
      </section>
    </main>
  );
}
