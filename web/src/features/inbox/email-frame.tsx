import { useEffect, useMemo, useRef } from 'react';
import { emailDocument } from './email-html';
import { fitFrame } from './fit-frame';

// No allow-scripts, allow-forms or allow-top-navigation: the mail cannot run code, submit or leave
// the page. allow-same-origin only lets this page measure the content; without allow-scripts nothing
// inside can use that origin, and the policy in the document also forbids scripts. allow-popups (and
// escaping the sandbox) lets a clicked link open in a new tab as an ordinary page.
const sandbox = 'allow-same-origin allow-popups allow-popups-to-escape-sandbox';

// EmailFrame shows untrusted email HTML inline, as tall as its content.
export function EmailFrame({ title, html }: { title: string; html: string }) {
  const observer = useRef<ResizeObserver | null>(null);
  const srcDoc = useMemo(() => emailDocument(html, window.location.origin), [html]);
  useEffect(() => () => observer.current?.disconnect(), []);

  function follow(event: { currentTarget: HTMLIFrameElement }) {
    const frame = event.currentTarget;
    const body = frame.contentDocument?.body;
    if (!body) return;
    fitFrame(frame);
    observer.current?.disconnect();
    if (typeof ResizeObserver === 'undefined') return;
    // Images finishing their load grow the content; a resize of the column changes the width to fit.
    observer.current = new ResizeObserver(() => fitFrame(frame));
    observer.current.observe(body);
    observer.current.observe(frame);
  }

  return <iframe className="email-frame" title={title} sandbox={sandbox} scrolling="no" srcDoc={srcDoc} onLoad={follow} />;
}
