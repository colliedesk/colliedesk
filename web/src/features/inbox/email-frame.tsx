import { useEffect, useMemo, useRef } from 'react';
import { emailDocument } from './email-html';
import { fitFrame } from './fit-frame';

// No allow-scripts, allow-forms or allow-top-navigation: the mail cannot run code, submit or leave
// the page. allow-same-origin only lets this page measure the content; without allow-scripts nothing
// inside can use that origin, and the policy in the document also forbids scripts. allow-popups (and
// escaping the sandbox) lets a clicked link open in a new tab as an ordinary page.
const sandbox = 'allow-same-origin allow-popups allow-popups-to-escape-sandbox';

// A tracking pixel or spacer that cannot load would show the browser's broken-image icon. An image the mail
// itself declared at most 2 px by 2 px (or 0 in either direction) goes quietly when it fails; a real image keeps its alt
// text. The listener lives in this page, not in the frame, so it needs no script in the mail.
function hideFailedPixels(doc: Document): void {
  doc.addEventListener('error', (event) => {
    const image = event.target as HTMLElement | null;
    if (image?.tagName !== 'IMG') return;
    const declared = (name: string) => (image.hasAttribute(name) ? Number.parseFloat(image.getAttribute(name) ?? '') : Number.NaN);
    const [width, height] = [declared('width'), declared('height')];
    if (width === 0 || height === 0 || (width <= 2 && height <= 2)) image.style.display = 'none';
  }, true);
}

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
    hideFailedPixels(body.ownerDocument);
    observer.current = new ResizeObserver(() => fitFrame(frame));
    observer.current.observe(body);
    observer.current.observe(frame);
  }

  return <div className="email-frame-box"><iframe className="email-frame" title={title} sandbox={sandbox} scrolling="no" srcDoc={srcDoc} onLoad={follow} /></div>;
}
