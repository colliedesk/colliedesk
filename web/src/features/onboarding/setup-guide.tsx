import type { CSSProperties } from 'react';
import type { OnboardingGoal, SetupProgress } from '../../api/schemas';
import { lazyPart } from '../../ui/lazy-part';
import { visibleSteps } from './setup-steps';

// The checklist loads from its own chunk: the first bundle has no room for it.
const checklistPart = lazyPart(() => import('./setup-checklist'));

interface SetupGuideProps {
  progress: SetupProgress;
  admin: boolean;
  goal?: OnboardingGoal | null;
  onHide: () => void;
}

// SetupGuide shows the setup checklist. Until its chunk arrives, a box of the final height holds the
// place, so the rows below do not jump; if the chunk cannot load the guide is left out (it is a
// reminder, not a way to work).
export function SetupGuide({ progress, admin, goal, onHide }: SetupGuideProps) {
  const part = checklistPart.use();
  if (part.status === 'ready') return <part.module.SetupChecklist progress={progress} admin={admin} goal={goal} onHide={onHide} />;
  if (part.status === 'error') return null;
  const rows = visibleSteps(progress, admin).length + (goal === 'ai_agents' ? 1 : 0);
  return <div className="setup-guide-placeholder" aria-hidden="true" style={{ '--rows': rows } as CSSProperties} />;
}
