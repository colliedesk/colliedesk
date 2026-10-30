import type { OnboardingGoal, SetupProgress } from '../../api/schemas';
import { lazyPart } from '../../ui/lazy-part';

// The checklist loads from its own chunk: the first bundle has no room for it.
const checklistPart = lazyPart(() => import('./setup-checklist'));

// Starts the download of that chunk; the inbox calls it when it asks for the setup data, so the two travel together.
export const preloadSetupGuide = checklistPart.preload;

interface SetupGuideProps {
  progress: SetupProgress;
  admin: boolean;
  goal?: OnboardingGoal | null;
  defaultOpen?: boolean;
  onHide: () => void;
}

// SetupGuide shows the setup checklist once its chunk arrives. It floats in a corner, so nothing needs
// to hold its place meanwhile; if the chunk cannot load the guide is left out (it is a reminder, not a
// way to work).
export function SetupGuide({ progress, admin, goal, defaultOpen, onHide }: SetupGuideProps) {
  const part = checklistPart.use();
  if (part.status !== 'ready') return null;
  return <part.module.SetupChecklist progress={progress} admin={admin} goal={goal} defaultOpen={defaultOpen} onHide={onHide} />;
}
