import { useEffect } from 'react';
import { useRouter } from '@tanstack/react-router';

// useInAppLinks follows every plain link to a page of the app through the router, so it opens without
// reloading even where the link is a bare <a href>, and loads that page ahead when the pointer or the focus
// reaches the link, as a router Link does. A link the app has no page for (an API download, the public help
// center), a download, a new tab and another site are left to the browser, and so is a click with a modifier
// key; a link whose own handler already followed it is not followed twice.
export function useInAppLinks() {
  const router = useRouter();
  useEffect(() => {
    // pageAddress is the address inside the app the event's link opens, or undefined when the browser handles it.
    const pageAddress = (event: Event) => {
      const link = event.target instanceof Element ? event.target.closest('a[href]') : null;
      if (!(link instanceof HTMLAnchorElement) || link.hasAttribute('download')) return undefined;
      if (link.target && link.target !== '_self') return undefined;
      const url = new URL(link.href, window.location.href);
      if (url.origin !== window.location.origin) return undefined;
      const [, , page] = router.getMatchedRoutes(url.pathname);
      return page ? url.pathname + url.search + url.hash : undefined;
    };
    const follow = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const href = pageAddress(event);
      if (!href) return;
      event.preventDefault();
      void router.navigate({ href });
    };
    const preload = (event: Event) => {
      const href = pageAddress(event);
      // A page that cannot load ahead loads on the click and says so there.
      // href decides the address; to only satisfies the type (buildLocation reads href first).
      if (href) void router.preloadRoute({ to: href.split(/[?#]/, 1)[0], href }).catch(() => undefined);
    };
    document.addEventListener('click', follow);
    document.addEventListener('pointerover', preload);
    document.addEventListener('focusin', preload);
    return () => {
      document.removeEventListener('click', follow);
      document.removeEventListener('pointerover', preload);
      document.removeEventListener('focusin', preload);
    };
  }, [router]);
}
