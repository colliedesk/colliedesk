import type { MouseEvent, ReactNode } from 'react';

interface RouterLinkProps {
  href: string;
  // Follows the link inside the app; without it the browser loads the address.
  onFollow?: () => void;
  className?: string;
  label?: string;
  children: ReactNode;
}

// RouterLink is a real link (a new tab or a copied address still works) that a plain click follows
// through the router, so the page does not reload.
export function RouterLink({ href, onFollow, className, label, children }: RouterLinkProps) {
  const follow = (event: MouseEvent) => {
    if (!onFollow || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
    event.preventDefault();
    onFollow();
  };
  return <a className={className} href={href} aria-label={label} onClick={follow}>{children}</a>;
}
