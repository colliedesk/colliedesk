import type { MouseEvent, ReactNode } from 'react';

interface RouterLinkProps {
  href: string;
  // Follows the link inside the app; without it the browser loads the address.
  onFollow?: () => void;
  className?: string;
  label?: string;
  // current marks the link to the page or filter on screen.
  current?: boolean;
  children: ReactNode;
}

// RouterLink is a real link (a new tab or a copied address still works) that a plain click follows
// through the router, so the page does not reload.
export function RouterLink({ href, onFollow, className, label, current, children }: RouterLinkProps) {
  const follow = (event: MouseEvent) => {
    if (!onFollow || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
    event.preventDefault();
    onFollow();
  };
  return <a className={className} href={href} aria-label={label} aria-current={current ? 'page' : undefined} onClick={follow}>{children}</a>;
}
