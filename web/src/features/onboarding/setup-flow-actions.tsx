import type { ReactNode } from 'react';
import { flowMessages as m } from './setup-flow-messages';

export interface FlowMoves {
  // back is the step before (none on the first); later the address after this one; next opens it.
  back?: string;
  later: string;
  // nextLabel names what the main button opens: "Next: Invite your team", or "Finish setup".
  nextLabel: string;
  next: () => void;
}

// FlowActions are the moves between steps: Back, "I'll do this later" and the one filled button. pinned keeps
// them at the bottom of a narrow screen, with note (a consent line) under the button.
export function FlowActions({ back, later, primary, missing, pinned = false, note }: {
  back?: string; later?: string; primary: ReactNode; missing?: { id: string; text: string }; pinned?: boolean; note?: ReactNode;
}) {
  return <nav className="setup-flow-actions" aria-label={m.flowNav} data-pinned={pinned || undefined}>
    {missing && <p id={missing.id} className="muted-copy setup-flow-missing">{missing.text}</p>}
    <div className="setup-flow-buttons">
      {back && <a className="button setup-flow-back" href={back}>{m.back}</a>}
      {later && <a className="link-action setup-flow-later" href={later}>{m.later}</a>}
      {primary}
      {note && <p className="setup-flow-note">{note}</p>}
    </div>
  </nav>;
}
