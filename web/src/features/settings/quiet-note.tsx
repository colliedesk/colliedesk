import { HelpTip } from '../../ui/help-tip';
import type { HelpTipProps } from '../../ui/help-tip';

// QuietNote is the one short line a row keeps ("Quiet by default" in docs/internals/design.md) with the tip that holds
// the rest. The line goes in `note` so the quiet-copy gate counts it at the call site; a procedure also passes `link`
// (Learn more) to its page in the docs. The paragraph keeps the muted-copy class, since the section styles address it.
export function QuietNote({ note, label, tip, link, id, className = '' }: { note: string; label: string; tip: string; link?: HelpTipProps['link']; id?: string; className?: string }) {
  return <p className={`muted-copy heading-with-tip ${className}`.trim()}><span>{note}</span><HelpTip label={label} text={tip} link={link} id={id} /></p>;
}
