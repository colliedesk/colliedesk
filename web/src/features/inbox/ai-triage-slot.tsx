import type { ComponentProps } from 'react';
import { lazyPart, Part, PartUnavailable } from '../../ui/lazy-part';

// The AI triage suggestion loads in its own chunk only where triage is on: in the details panel, and on
// the timeline for what AI applied and for narrow screens, where the details hide behind a toggle.
const aiTriagePart = lazyPart(() => import('./ai-triage-part'));

type CardProps = ComponentProps<typeof import('./ai-triage-part')['AITriageCard']>;

export function AITriageSlot(props: CardProps) {
  return <Part part={aiTriagePart} failed={props.placement === 'timeline' ? null : <section className="details-section"><PartUnavailable /></section>}
    render={(module) => <module.AITriageCard key={props.conversationId} {...props} />} />;
}
