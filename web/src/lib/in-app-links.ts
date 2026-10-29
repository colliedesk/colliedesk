import { useEffect } from 'react';
import { useRouter } from '@tanstack/react-router';

// useInAppLinks follows every plain link to a page of the app through the router, so it opens without
// reloading even where the link is a bare <a href>. A link the app has no page for (an API download, the
// public help center), a download, a new tab, another site and a click with a modifier key are left to
// the browser; a link whose own handler already followed it is not followed twice.
export function useInAppLinks() {
  const router = useRouter();
  useEffect(() => {
    const follow = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const link = event.target instanceof Element ? event.target.closest('a[href]') : null;
      if (!(link instanceof HTMLAnchorElement) || link.hasAttribute('download')) return;
      if (link.target && link.target !== '_self') return;
      const url = new URL(link.href, window.location.href);
      if (url.origin !== window.location.origin) return;
      const [, , page] = router.getMatchedRoutes(url.pathname);
      if (!page) return;
      event.preventDefault();
      void router.navigate({ href: url.pathname + url.search + url.hash });
    };
    document.addEventListener('click', follow);
    return () => document.removeEventListener('click', follow);
  }, [router]);
}
