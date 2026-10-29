import { useEffect, useState } from 'react';

// A small router for the console's dozen screens: the address under the base the server names
// (<meta name="colliedesk-operator-base">: /operator on the app's host, empty on OPERATOR_ORIGIN).
export function basePath(): string {
  const meta = document.querySelector('meta[name="colliedesk-operator-base"]')?.getAttribute('content') ?? '/operator';
  return meta.replace(/\/$/, '');
}

export function currentPath(): string {
  const base = basePath();
  const path = window.location.pathname;
  const inside = base && path.startsWith(base) ? path.slice(base.length) : path;
  return inside === '' ? '/' : inside;
}

export function href(path: string): string {
  return `${basePath()}${path}`;
}

const listeners = new Set<() => void>();

export function navigate(path: string, replace = false) {
  const target = href(path);
  if (replace) window.history.replaceState(null, '', target);
  else window.history.pushState(null, '', target);
  listeners.forEach((listener) => listener());
}

export function usePath(): string {
  const [path, setPath] = useState(currentPath);
  useEffect(() => {
    const update = () => setPath(currentPath());
    listeners.add(update);
    window.addEventListener('popstate', update);
    return () => {
      listeners.delete(update);
      window.removeEventListener('popstate', update);
    };
  }, []);
  return path;
}
