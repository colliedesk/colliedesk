import { useEffect, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import type { Me, SetupProgress } from '../../api/schemas';
import { lazyPart, PartUnavailable } from '../../ui/lazy-part';
import { quickStartCount, readSkipped, storeSkipped } from './setup-steps';
import { frameMessages } from '../shell/frame-messages';

// The checklist loads from its own chunk: the first bundle has no room for it. It starts loading as soon as there is
// a step left, so the press that opens it finds it there.
const checklistPart = lazyPart(() => import('./setup-checklist'));
// The app asks for the chunk together with the setup data, not after it.
export const preloadQuickStart = checklistPart.preload;

// A ring that fills with the share of steps done.
const Ring = ({ share }: { share: number }) => <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" className="quick-start-ring"
  style={{ '--share': share } as CSSProperties}>
  <circle cx="12" cy="12" r="8" fill="none" stroke="var(--line-control)" strokeWidth="2" />
  <circle cx="12" cy="12" r="8" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" pathLength="100"
    strokeDasharray={`${Math.max(share * 100, 1)} 100`} transform="rotate(-90 12 12)" />
</svg>;

// QuickStart is the icon beside the workspace name while setup has a step left: its name says how many, an amber dot
// says a Set up step is among them, and a press opens the checklist under it. It is gone once every step is done or
// skipped, and while the person hid the setup guide (Settings → Preferences).
export function QuickStart({ progress, me }: { progress?: SetupProgress; me: Me }) {
  const [skipped, setSkipped] = useState(() => readSkipped(me.agent.id));
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  // Where the checklist opens: under the icon, or beside it on the folded rail. Read when it opens.
  const [at, setAt] = useState<{ left: number; top: number }>();
  const open = at !== undefined;
  const setOpen = (next: boolean) => {
    const box = button.current?.getBoundingClientRect();
    const beside = box !== undefined && box.right <= 64 && window.innerWidth > 704;
    setAt(!next || !box ? undefined : beside ? { left: box.right + 8, top: box.top } : { left: 8, top: box.bottom + 8 });
  };
  const count = progress && !me.setup_guide_hidden ? quickStartCount(progress, me, skipped) : undefined;
  const shown = count !== undefined && count.left > 0;
  const part = checklistPart.use(shown);
  useEffect(() => {
    if (!open) return;
    const closeOutside = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setAt(undefined); };
    document.addEventListener('pointerdown', closeOutside);
    return () => document.removeEventListener('pointerdown', closeOutside);
  }, [open]);
  if (!shown || !progress) return null;
  const skip = (next: boolean) => { setSkipped(next); storeSkipped(me.agent.id, next); };
  return (
    <div className="quick-start" ref={root}
      onKeyDown={(event) => { if (event.key === 'Escape' && open) { event.stopPropagation(); setOpen(false); button.current?.focus(); } }}>
      <button type="button" ref={button} className="quick-start-button" aria-label={frameMessages.quickStartLeft(count.left)} aria-pressed={open}
        data-tip={frameMessages.quickStart} onClick={() => setOpen(!open)}>
        <Ring share={count.total === 0 ? 0 : count.done / count.total} />
        {count.setUpLeft && <span className="quick-start-dot" aria-hidden="true" />}
      </button>
      {/* A chosen step opens its page: the checklist steps aside for it. */}
      {open && <div className="quick-start-popover" role="dialog" aria-label={frameMessages.getReady(me.workspace.name)} style={at}
        onClick={(event) => { if ((event.target as Element).closest('a')) setOpen(false); }}>
        {part.status === 'ready' ? <part.module.SetupChecklist progress={progress} me={me} goal={me.onboarding_goal} workspaceName={me.workspace.name}
          skipped={skipped} onSkip={skip} /> : part.status === 'error' ? <PartUnavailable /> : null}
      </div>}
    </div>
  );
}
