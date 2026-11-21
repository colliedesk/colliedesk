import { useEffect, useRef, useState } from 'react';
import { ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import { formatListTime } from '../../lib/format-date';
import { editable } from './article-draft';
import { kbApi } from './kb-api';
import type { KBArticle } from './kb-api';
import { kbMessages } from './kb-messages';
import { reason } from './kb-tree';
import { clearUnsaved, keepUnsaved, readUnsaved } from './unsaved-store';
import type { UnsavedText } from './unsaved-store';
import { useAutosave } from './use-autosave';

// The autosave's strings live with the editor's chunk, like its other strings.
export const autosaveMessages = {
  savedAt: (time: string) => `Saved ${time}`,
  offline: 'Offline, kept on this device',
  restoreOffer: (time: string) => `Restore unsaved text from ${time}?`,
  restore: 'Restore',
  discard: 'Discard',
};

type Text = { title: string; body: string };
type State = { kind: 'saving' } | { kind: 'saved'; at: string } | { kind: 'offline' } | undefined;

// A keepalive request outlives a closing page, but the browser takes at most 64 KiB of them at once.
const keepaliveBytes = 60_000;
// The editor writes every line ending as \n; a stored \r\n text is the same text.
export const sameBody = (text: string, of: KBArticle) => text === of.body_md || text === of.body_md.replace(/\r\n?/g, '\n');

interface Options {
  // key is where this person's unsaved text of this article is kept on the device (unsavedKey).
  key?: string;
  // article is the article as the server last answered: the form compares against it.
  article?: KBArticle;
  text: Text;
  dirty: boolean;
  timezone: string;
  // current reads the newest answer at once, read the form with typing not yet on the counter.
  current: () => KBArticle | undefined;
  read: () => Text;
  // blocked: a save, a conflict, an upload or a too long text waits for the person.
  blocked: () => boolean;
  onSaved: (updated: KBArticle) => void;
  onConflict: () => void;
  onProblem: (text: string) => void;
  onRestore: (text: Text) => void;
}

// useArticleAutosave saves an article's title and text while the person types, without a version and
// never to the help center: a draft article changes in place, a published one keeps it in its
// unpublished changes. Until the server has the text it is kept on this device and offered back.
export function useArticleAutosave(options: Options) {
  const { key, article, text, dirty, timezone } = options;
  const [state, setState] = useState<State>();
  // pending: an autosave stored text the history has no version of yet; Save adds it.
  const [pending, setPending] = useState(false);
  const inFlight = useRef<Promise<unknown>>(undefined);
  const [offer, setOffer] = useState<UnsavedText | undefined>(() => {
    if (!key || !article) return undefined;
    const kept = readUnsaved(key);
    const shown = editable(article);
    return kept && Date.parse(kept.at) > Date.parse(shown.updated_at) && (kept.title !== shown.title || !sameBody(kept.body, shown)) ? kept : undefined;
  });

  // The device keeps what the server does not have yet; a copy the server already has, or one older
  // than the server's, is dropped. An offered copy waits for the person's answer.
  useEffect(() => {
    if (!key || !article || offer) return;
    if (dirty) keepUnsaved(key, text);
    else clearUnsaved(key);
  }, [key, article, offer, dirty, text.title, text.body]);

  const run = (leaving: boolean) => {
    const of = options.current();
    if (!of || inFlight.current || options.blocked()) return;
    const shown = editable(of);
    const now = options.read();
    const title = now.title.trim();
    const change = {
      ...(title && title !== shown.title ? { title } : {}),
      ...(!sameBody(now.body, shown) ? { body_md: now.body } : {}),
    };
    if (!title || Object.keys(change).length === 0) return;
    const body = JSON.stringify(change);
    setState({ kind: 'saving' });
    inFlight.current = kbApi.updateArticle(shown.id, { updated_at: shown.updated_at, autosave: true, ...change },
      leaving && new TextEncoder().encode(body).length < keepaliveBytes ? { keepalive: true } : undefined)
      .then((updated) => {
        options.onSaved(updated);
        setPending(true);
        if (key) clearUnsaved(key, now);
        setState({ kind: 'saved', at: updated.updated_at });
      }, (error: unknown) => {
        if (error instanceof ChangedElsewhereError) {
          setState(undefined);
          options.onConflict();
        } else if (error instanceof InvalidRequestError) {
          setState(undefined);
          options.onProblem(reason(error, kbMessages.articleActionFailed));
        } else {
          setState({ kind: 'offline' });
        }
      })
      .finally(() => { inFlight.current = undefined; });
  };
  useAutosave(`${text.title}\u0000${text.body}`, run);

  // A manual save waits for an autosave in flight, so both never send the same updated_at. With none in
  // flight there is nothing to wait for, and the save goes on in the same tick.
  const settle = () => inFlight.current?.then(() => undefined, () => undefined);
  // saved: a manual save added the version.
  const saved = (at: string) => {
    setPending(false);
    setState({ kind: 'saved', at });
  };
  const word = state?.kind === 'saving' ? kbMessages.saving : state?.kind === 'offline' ? autosaveMessages.offline
    : dirty ? kbMessages.unsaved : state?.kind === 'saved' ? autosaveMessages.savedAt(formatListTime(state.at, timezone)) : '';
  const restoreBar = offer && key ? (
    <div role="group" aria-label={autosaveMessages.restoreOffer(formatListTime(offer.at, timezone))} className="warning-note kb-restore">
      <p>{autosaveMessages.restoreOffer(formatListTime(offer.at, timezone))}</p>
      <div className="button-row">
        <button type="button" onClick={() => { setOffer(undefined); options.onRestore(offer); }}>{autosaveMessages.restore}</button>
        <button type="button" onClick={() => { clearUnsaved(key); setOffer(undefined); }}>{autosaveMessages.discard}</button>
      </div>
    </div>
  ) : undefined;
  return { word, pending, settle, saved, restoreBar };
}
