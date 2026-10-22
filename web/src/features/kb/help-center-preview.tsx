import { useEffect, useId, useRef, useState } from 'react';
import { themeMessages } from './help-center-theme-messages';
import { ThemeInvalidError, themeApi } from './theme-api';
import type { PreviewPage, ThemeInput, ThemePreview } from './theme-api';

// The policy the server writes into every preview document. The frame holds the document only when it
// carries exactly this one, so a preview never runs under whatever the app's own page would allow. The
// frame is same-origin and sends the cookie, so the policy names paths rather than the origin: images
// only from the workspace's image and media routes, style only from the help center's hashed sheets
// and the inline brand block.
export const previewPolicy = (origin: string) => `default-src 'none'; img-src ${origin}/api/v1/kb/images/ ${origin}/api/v1/kb/media/; `
  + `style-src ${origin}/help/_/ 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'`;
const policyMeta = (origin: string) => `<meta http-equiv="Content-Security-Policy" content="${previewPolicy(origin)}">`;

// No allow-scripts, allow-forms, allow-popups or allow-top-navigation: nothing in the page runs, submits
// or opens. allow-same-origin lets its images load from the app with the session, and lets this page stop
// a link from replacing the preview; without scripts nothing inside can use that origin.
const sandbox = 'allow-same-origin';

// The preview waits this long after the last change before it asks the server.
export const previewDelay = 300;

// useThemePreview renders page with theme on the server: 300 ms after the last change, one request at a
// time, and an answer for a theme that is no longer the latest is dropped (the latest goes out next).
// A failure keeps the last page shown; a refusal also gives its messages by field (refused), so a field
// can say why it is not in the preview yet. Nothing is asked while enabled is false (the saved theme is
// still loading) or after the caller goes away.
export function useThemePreview(page: PreviewPage, theme: ThemeInput, enabled = true) {
  const key = JSON.stringify({ page, theme });
  const [shown, setShown] = useState<{ key?: string; preview?: ThemePreview; error?: string; refused?: Record<string, string> }>({});
  const queue = useRef({ latest: key, due: undefined as string | undefined, busy: false, timer: 0, alive: true });
  const run = () => {
    const state = queue.current;
    if (!state.alive || state.busy || state.due === undefined) return;
    const body = state.due;
    state.due = undefined;
    state.busy = true;
    const sent = JSON.parse(body) as { page: PreviewPage; theme: ThemeInput };
    themeApi.preview(sent.page, sent.theme).then(
      (preview) => { if (state.alive && body === state.latest) setShown({ key: body, preview }); },
      (error: unknown) => {
        if (!state.alive || body !== state.latest) return;
        const refused = error instanceof ThemeInvalidError ? error.fields : undefined;
        setShown((current) => ({ ...current, key: body, error: themeMessages.previewFailed, refused }));
      },
    ).finally(() => {
      state.busy = false;
      run();
    });
  };
  useEffect(() => {
    const state = queue.current;
    state.latest = key;
    window.clearTimeout(state.timer);
    if (!enabled) return;
    state.timer = window.setTimeout(() => {
      state.due = state.latest;
      run();
    }, previewDelay);
  }, [key, enabled]);
  useEffect(() => {
    const state = queue.current;
    state.alive = true;
    return () => {
      state.alive = false;
      window.clearTimeout(state.timer);
    };
  }, []);
  const current = shown.key === key;
  return { preview: shown.preview, error: current ? shown.error : undefined, refused: current ? shown.refused : undefined, pending: !current };
}

// A click on a link inside the preview would load that address into the frame; the preview is a picture
// of the page, so it stays where it is. The document's own style takes pointers off its links from the
// first paint; this listener, which lives in this page so the frame needs no script, also holds a link
// opened from the keyboard.
function holdLinks(event: { currentTarget: HTMLIFrameElement }) {
  event.currentTarget.contentDocument?.addEventListener('click', (click) => {
    if ((click.target as Element | null)?.closest?.('a, button')) click.preventDefault();
  }, true);
}

type Width = 'desktop' | 'phone';

// HelpCenterPreview shows a preview document in a sandboxed frame, at desktop or phone width, with the
// choice of the page it shows.
export function HelpCenterPreview({ html, pending, error, page, onPage }: {
  html?: string; pending: boolean; error?: string; page: PreviewPage; onPage: (page: PreviewPage) => void;
}) {
  const id = useId();
  const [width, setWidth] = useState<Width>('desktop');
  const safe = html !== undefined && html.includes(policyMeta(window.location.origin));
  const radios = <T extends string>(name: string, legend: string, value: T, options: readonly (readonly [T, string])[], change: (next: T) => void) => (
    <fieldset className="segmented">
      <legend className="sr-only">{legend}</legend>
      {options.map(([option, label]) => (
        <label key={option}><input type="radio" name={`${id}-${name}`} checked={value === option} onChange={() => change(option)} />{label}</label>
      ))}
    </fieldset>
  );
  return (
    <section className="theme-preview" aria-labelledby={`${id}-heading`}>
      <div className="theme-preview-bar">
        <h3 id={`${id}-heading`}>{themeMessages.preview}</h3>
        {radios('page', themeMessages.previewPage, page, [['home', themeMessages.previewHome], ['article', themeMessages.previewArticle]] as const, onPage)}
        {radios('width', themeMessages.previewWidth, width, [['desktop', themeMessages.desktop], ['phone', themeMessages.phoneWidth]] as const, setWidth)}
        <span role="status" className="muted-copy theme-preview-state">{pending && html !== undefined ? themeMessages.previewUpdating : ''}</span>
      </div>
      {error && <p role="alert" className="error-copy">{error}</p>}
      {html === undefined
        ? !error && <p className="muted-copy">{themeMessages.previewLoading}</p>
        : safe
          ? <div className="theme-preview-stage" data-width={width}>
              <iframe title={themeMessages.previewTitle} sandbox={sandbox} srcDoc={html} onLoad={holdLinks} />
            </div>
          : <p role="alert" className="error-copy">{themeMessages.previewFailed}</p>}
    </section>
  );
}
